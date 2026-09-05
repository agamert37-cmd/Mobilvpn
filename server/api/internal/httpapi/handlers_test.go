package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"vpnapi/internal/config"
	"vpnapi/internal/fleet"
	"vpnapi/internal/ipam"
	"vpnapi/internal/ratelimit"
	"vpnapi/internal/session"
	"vpnapi/internal/wireguard"
)

// newTestApp builds a fully wired App using fake WireGuard/OpenVPN
// backends, so these tests exercise real routing, validation, IPAM,
// session lifecycle, and DTO-shape logic without needing root, a kernel
// WireGuard interface, or an easy-rsa PKI on disk.
func newTestApp(t *testing.T, mutateCfg func(*config.Config)) (*App, *fakeWG, *fakeOVPN) {
	t.Helper()
	cfg := config.Default()
	cfg.NodeID = "test_node"
	cfg.NodeRegion = "Test Region"
	cfg.PublicEndpointHost = "vpn.test.example"
	cfg.StateFile = ""
	cfg.Fleet.NodesFile = filepath.Join(t.TempDir(), "missing-nodes.json")
	cfg.RateLimit.RequestsPerMinutePerIP = 6000
	cfg.RateLimit.Burst = 1000
	cfg.RateLimit.MaxConcurrentSessions = 500
	if mutateCfg != nil {
		mutateCfg(&cfg)
	}

	wgPool, err := ipam.NewPool("wg", cfg.WireGuard.SubnetCIDR, cfg.WireGuard.ServerVirtualIP)
	if err != nil {
		t.Fatalf("wg ipam.NewPool: %v", err)
	}
	ovpnPool, err := ipam.NewPool("ovpn", cfg.OpenVPN.SubnetCIDR, subnetGateway(cfg.OpenVPN.SubnetCIDR))
	if err != nil {
		t.Fatalf("ovpn ipam.NewPool: %v", err)
	}
	registry, err := fleet.Load(cfg.Fleet.NodesFile, cfg.NodeID, fleet.Node{Country: "Test"}, cfg.Fleet.SharedSecret, time.Hour, time.Second)
	if err != nil {
		t.Fatalf("fleet.Load: %v", err)
	}

	fw := newFakeWG()
	fo := newFakeOVPN()
	app := &App{
		cfg:            cfg,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessions:       session.NewStore(cfg.StateFile),
		policy:         NewPolicy(cfg.DNS.BlocklistEnabledDefault),
		wg:             fw,
		wgPub:          "server-pub-key",
		wgPool:         wgPool,
		ovpn:           fo,
		ovpnPool:       ovpnPool,
		registry:       registry,
		proxy:          fleet.NewProxyClient(2*time.Second, cfg.Fleet.SharedSecret),
		connectLimiter: ratelimit.New(cfg.RateLimit.RequestsPerMinutePerIP, cfg.RateLimit.Burst),
		mutateLimiter:  ratelimit.New(cfg.RateLimit.RequestsPerMinutePerIP, cfg.RateLimit.Burst),
		startedAt:      time.Now(),
	}
	registry.SetSelfLoadFunc(app.computeSelfLoad)
	return app, fw, fo
}

func doJSON(t *testing.T, srv *httptest.Server, method, path string, body interface{}) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	return resp, respBody
}

func TestHandleHealth(t *testing.T) {
	app, _, _ := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, body := doJSON(t, srv, http.MethodGet, "/api/v1/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var health BackendHealthDto
	mustDecode(t, body, &health)
	if health.Status != "OPERATIONAL" || health.ActiveNodes != 1 || health.ClusterRegion != "Test Region" {
		t.Fatalf("unexpected health payload: %+v", health)
	}
}

func TestHandleServersSelfOnly(t *testing.T) {
	app, _, _ := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, body := doJSON(t, srv, http.MethodGet, "/api/v1/servers", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var servers []ServerNodeDto
	mustDecode(t, body, &servers)
	if len(servers) != 1 || servers[0].ID != "test_node" {
		t.Fatalf("servers = %+v, want exactly one self node", servers)
	}
}

func TestHandleConnectWireGuardEphemeral(t *testing.T) {
	app, fw, _ := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, body := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{
		ServerID: "test_node", Protocol: "WIREGUARD",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out ConnectResponseDto
	mustDecode(t, body, &out)

	if !out.Success || out.Status != "ESTABLISHED" {
		t.Fatalf("unexpected connect response: %+v", out)
	}
	if out.ClientPrivateKey == "" {
		t.Fatalf("ephemeral connect should return a generated client private key")
	}
	if out.ServerPublicKey != "server-pub-key" {
		t.Fatalf("ServerPublicKey = %q, want the configured server pub key", out.ServerPublicKey)
	}
	if out.VirtualIP == "" || out.SessionID == "" {
		t.Fatalf("missing virtualIp/sessionId: %+v", out)
	}
	if fw.peerCount() != 1 {
		t.Fatalf("expected exactly one peer added to the (fake) WireGuard interface, got %d", fw.peerCount())
	}
	if _, ok := app.sessions.Get(out.SessionID); !ok {
		t.Fatalf("session %q was not stored", out.SessionID)
	}
}

func TestHandleConnectWireGuardBringYourOwnKey(t *testing.T) {
	app, fw, _ := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	_, clientPub, err := wireguard.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}

	resp, body := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{
		ServerID: "test_node", Protocol: "WIREGUARD", ClientPublicKey: clientPub,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out ConnectResponseDto
	mustDecode(t, body, &out)
	if out.ClientPrivateKey != "" {
		t.Fatalf("bring-your-own-key connect must never return a private key, got one")
	}
	fw.mu.Lock()
	_, peered := fw.peers[clientPub]
	fw.mu.Unlock()
	if !peered {
		t.Fatalf("the exact client-supplied public key should have been added as the wg peer")
	}
}

func TestHandleConnectRejectsMalformedPublicKey(t *testing.T) {
	app, _, _ := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, _ := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{
		ServerID: "test_node", Protocol: "WIREGUARD", ClientPublicKey: "not-a-valid-key",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a malformed public key", resp.StatusCode)
	}
}

func TestHandleConnectUnknownServerID(t *testing.T) {
	app, _, _ := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, _ := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{
		ServerID: "does_not_exist", Protocol: "WIREGUARD",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown serverId", resp.StatusCode)
	}
}

func TestHandleConnectWireGuardUnavailable(t *testing.T) {
	app, fw, _ := newTestApp(t, nil)
	fw.available = true
	fw.unavailMsg = "wg binary missing"
	fw.available = false
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, body := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{
		ServerID: "test_node", Protocol: "WIREGUARD",
	})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out ConnectResponseDto
	mustDecode(t, body, &out)
	if out.Success || out.Status != "UNAVAILABLE" {
		t.Fatalf("unexpected payload: %+v", out)
	}
}

func TestHandleConnectReleasesIPOnAddPeerFailure(t *testing.T) {
	app, fw, _ := newTestApp(t, func(cfg *config.Config) {
		cfg.WireGuard.SubnetCIDR = "10.66.0.0/30" // capacity 1, to prove release-on-failure frees it back up
	})
	fw.addPeerErr = errFakeAddPeer
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, _ := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "WIREGUARD"})
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when AddPeer fails", resp.StatusCode)
	}
	if app.wgPool.InUse() != 0 {
		t.Fatalf("wgPool.InUse() = %d after a failed AddPeer, want 0 (IP must be released)", app.wgPool.InUse())
	}

	// Prove the pool is actually usable again, not just numerically zeroed.
	fw.addPeerErr = nil
	resp2, body2 := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "WIREGUARD"})
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("retry after failure: status = %d, body = %s", resp2.StatusCode, body2)
	}
}

func TestHandleConnectCapacityFull(t *testing.T) {
	app, _, _ := newTestApp(t, func(cfg *config.Config) {
		cfg.RateLimit.MaxConcurrentSessions = 0
	})
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, _ := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "WIREGUARD"})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 at zero capacity", resp.StatusCode)
	}
}

func TestHandleConnectIKEv2Unsupported(t *testing.T) {
	app, _, _ := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, body := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "IKEV2"})
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, body = %s, want 501 for IKEv2", resp.StatusCode, body)
	}
	var out ConnectResponseDto
	mustDecode(t, body, &out)
	if out.Success {
		t.Fatalf("IKEv2 connect must report success=false")
	}
}

func TestHandleConnectUnknownProtocol(t *testing.T) {
	app, _, _ := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, _ := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "CARRIER_PIGEON"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unrecognized protocol", resp.StatusCode)
	}
}

func TestHandleConnectOpenVPNTCPProvisionsProfile(t *testing.T) {
	app, _, fo := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, body := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "OPENVPN_TCP"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out ConnectResponseDto
	mustDecode(t, body, &out)
	if !out.Success || out.Status != "PROVISIONED" || out.OvpnProfile == "" {
		t.Fatalf("unexpected openvpn connect response: %+v", out)
	}
	if out.AssignedPort != app.cfg.OpenVPN.TCPPort {
		t.Fatalf("AssignedPort = %d, want the configured TCP port %d", out.AssignedPort, app.cfg.OpenVPN.TCPPort)
	}
	fo.mu.Lock()
	_, ok := fo.provisioned[out.SessionID]
	fo.mu.Unlock()
	if !ok {
		t.Fatalf("session %q was not provisioned in the (fake) OpenVPN PKI", out.SessionID)
	}
}

func TestHandleDisconnectUnknownSessionIsIdempotent(t *testing.T) {
	app, _, _ := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, body := doJSON(t, srv, http.MethodPost, "/api/v1/disconnect", DisconnectRequestDto{SessionID: "sess_test_node_doesnotexist"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 (disconnect is idempotent)", resp.StatusCode, body)
	}
	var out DisconnectResponseDto
	mustDecode(t, body, &out)
	if !out.Success {
		t.Fatalf("disconnecting an unknown session should still report success")
	}
}

func TestHandleDisconnectReleasesResources(t *testing.T) {
	app, fw, _ := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	_, connectBody := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "WIREGUARD"})
	var connected ConnectResponseDto
	mustDecode(t, connectBody, &connected)

	if app.wgPool.InUse() != 1 {
		t.Fatalf("wgPool.InUse() = %d after connect, want 1", app.wgPool.InUse())
	}

	resp, disconnectBody := doJSON(t, srv, http.MethodPost, "/api/v1/disconnect", DisconnectRequestDto{SessionID: connected.SessionID})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, disconnectBody)
	}
	if fw.peerCount() != 0 {
		t.Fatalf("peer was not removed from the (fake) WireGuard interface on disconnect")
	}
	if app.wgPool.InUse() != 0 {
		t.Fatalf("wgPool.InUse() = %d after disconnect, want 0 (IP must be released)", app.wgPool.InUse())
	}
	if _, ok := app.sessions.Get(connected.SessionID); ok {
		t.Fatalf("session still present in the store after disconnect")
	}
}

func TestHandleTelemetryUnknownSession(t *testing.T) {
	app, _, _ := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, _ := doJSON(t, srv, http.MethodGet, "/api/v1/telemetry?sessionId=sess_test_node_ghost", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an unknown session", resp.StatusCode)
	}
}

func TestHandleTelemetryComputesThroughputAcrossPolls(t *testing.T) {
	app, fw, _ := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	_, connectBody := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "WIREGUARD"})
	var connected ConnectResponseDto
	mustDecode(t, connectBody, &connected)

	// Find the peer key the handler actually used (bootstrap/ephemeral mode
	// generates it server-side, so we recover it from the fake's peer map).
	fw.mu.Lock()
	var peerKey string
	for k := range fw.peers {
		peerKey = k
	}
	fw.mu.Unlock()

	fw.setStat(peerKey, 0, 0)
	_, firstBody := doJSON(t, srv, http.MethodGet, "/api/v1/telemetry?sessionId="+connected.SessionID, nil)
	var first ServerTelemetryDto
	mustDecode(t, firstBody, &first)
	if first.DownloadSpeedMbps != 0 {
		t.Fatalf("first sample should be a zero baseline, got %v", first.DownloadSpeedMbps)
	}

	time.Sleep(20 * time.Millisecond)
	fw.setStat(peerKey, 5_000_000, 1_000_000)
	_, secondBody := doJSON(t, srv, http.MethodGet, "/api/v1/telemetry?sessionId="+connected.SessionID, nil)
	var second ServerTelemetryDto
	mustDecode(t, secondBody, &second)
	if second.DownloadSpeedMbps <= 0 || second.UploadSpeedMbps <= 0 {
		t.Fatalf("expected positive throughput after byte counters increased: %+v", second)
	}
	if second.TotalDownloadedBytes != 5_000_000 {
		t.Fatalf("TotalDownloadedBytes = %d, want the cumulative counter 5000000", second.TotalDownloadedBytes)
	}
}

func TestHandleSettingsRoundTripAffectsSplitTunneling(t *testing.T) {
	app, _, _ := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, body := doJSON(t, srv, http.MethodPost, "/api/v1/settings", UpdateSettingsRequestDto{
		KillSwitch: true, ThreatProtection: false, SplitTunneling: true, AutoConnect: true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var echoed map[string]bool
	mustDecode(t, body, &echoed)
	if !echoed["splitTunneling"] || !echoed["autoConnect"] || echoed["threatProtection"] {
		t.Fatalf("settings echo = %+v", echoed)
	}

	// A subsequent connect should now receive the split-tunnel AllowedIps
	// instead of a full-tunnel default route.
	_, connectBody := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "WIREGUARD"})
	var connected ConnectResponseDto
	mustDecode(t, connectBody, &connected)
	if connected.AllowedIps != app.cfg.WireGuard.SubnetCIDR {
		t.Fatalf("AllowedIps = %q after enabling splitTunneling, want the tunnel subnet %q", connected.AllowedIps, app.cfg.WireGuard.SubnetCIDR)
	}
}

func TestConnectRateLimitReturns429(t *testing.T) {
	app, _, _ := newTestApp(t, func(cfg *config.Config) {
		cfg.RateLimit.RequestsPerMinutePerIP = 60
		cfg.RateLimit.Burst = 1
	})
	app.connectLimiter = ratelimit.New(60, 1)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp1, _ := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "WIREGUARD"})
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", resp1.StatusCode)
	}
	resp2, _ := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "WIREGUARD"})
	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second immediate request status = %d, want 429 (burst=1)", resp2.StatusCode)
	}
}

func mustDecode(t *testing.T, body []byte, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decoding JSON %s: %v", body, err)
	}
}

var errFakeAddPeer = &fakeError{"simulated AddPeer failure"}

type fakeError struct{ msg string }

func (e *fakeError) Error() string { return e.msg }
