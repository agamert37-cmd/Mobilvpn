package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	app, wg, ovpn, _ := newTestAppFull(t, mutateCfg)
	return app, wg, ovpn
}

// newTestAppFull also exposes the IKEv2 fake, for the tests that need it.
func newTestAppFull(t *testing.T, mutateCfg func(*config.Config)) (*App, *fakeWG, *fakeOVPN, *fakeIKEv2) {
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
	ikev2Pool, err := ipam.NewPool("ikev2", cfg.IKEv2.SubnetCIDR, subnetGateway(cfg.IKEv2.SubnetCIDR))
	if err != nil {
		t.Fatalf("ikev2 ipam.NewPool: %v", err)
	}
	registry, err := fleet.Load(cfg.Fleet.NodesFile, cfg.NodeID, fleet.Node{Country: "Test"}, cfg.Fleet.SharedSecret, time.Hour, time.Second)
	if err != nil {
		t.Fatalf("fleet.Load: %v", err)
	}

	fw := newFakeWG()
	fo := newFakeOVPN()
	fi := newFakeIKEv2()
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
		ikev2:          fi,
		ikev2Pool:      ikev2Pool,
		registry:       registry,
		proxy:          fleet.NewProxyClient(2*time.Second, cfg.Fleet.SharedSecret),
		connectLimiter: ratelimit.New(cfg.RateLimit.RequestsPerMinutePerIP, cfg.RateLimit.Burst),
		mutateLimiter:  ratelimit.New(cfg.RateLimit.RequestsPerMinutePerIP, cfg.RateLimit.Burst),
		startedAt:      time.Now(),
	}
	registry.SetSelfLoadFunc(app.computeSelfLoad)
	return app, fw, fo, fi
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

func TestHandleConnectIPv4OnlyDoesNotAdvertiseV6Route(t *testing.T) {
	// The default (v4-only) node must not tell a client to route ::/0 into
	// a tunnel that can't carry it — that black-holes the client's IPv6.
	app, fw, _ := newTestApp(t, nil)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	_, body := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{
		ServerID: "test_node", Protocol: "WIREGUARD",
	})
	var out ConnectResponseDto
	mustDecode(t, body, &out)

	if out.AllowedIps != "0.0.0.0/0" {
		t.Fatalf("AllowedIps = %q on a v4-only node, want exactly \"0.0.0.0/0\"", out.AllowedIps)
	}
	if out.VirtualIPv6 != "" {
		t.Fatalf("VirtualIPv6 = %q on a v4-only node, want empty", out.VirtualIPv6)
	}
	fw.mu.Lock()
	defer fw.mu.Unlock()
	for _, allowed := range fw.peers {
		if strings.Contains(allowed, "/128") {
			t.Fatalf("peer allowed-ips = %q on a v4-only node, should carry no IPv6", allowed)
		}
	}
}

func TestHandleConnectDualStackAssignsPairedV6Address(t *testing.T) {
	app, fw, _ := newTestApp(t, func(cfg *config.Config) {
		cfg.WireGuard.IPv6Enabled = true
		cfg.WireGuard.SubnetCIDRv6 = "fd00:66::/64"
	})
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	_, body := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{
		ServerID: "test_node", Protocol: "WIREGUARD",
	})
	var out ConnectResponseDto
	mustDecode(t, body, &out)

	if !strings.Contains(out.AllowedIps, "::/0") {
		t.Errorf("AllowedIps = %q on a dual-stack node, want it to include ::/0", out.AllowedIps)
	}
	derived, err := ipam.DeriveIPv6(net.ParseIP(out.VirtualIP), "fd00:66::/64")
	if err != nil {
		t.Fatalf("deriving the expected v6 address: %v", err)
	}
	if out.VirtualIPv6 != derived.String() {
		t.Errorf("VirtualIPv6 = %q, want %q (paired with %s)", out.VirtualIPv6, derived, out.VirtualIP)
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()
	for _, allowed := range fw.peers {
		if !strings.Contains(allowed, "/32") || !strings.Contains(allowed, "/128") {
			t.Fatalf("peer allowed-ips = %q, want both the v4 /32 and the v6 /128 pinned", allowed)
		}
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

func TestHandleConnectIKEv2DisabledReportsUnavailable(t *testing.T) {
	// IKEv2 is opt-in (it needs scripts/35-ikev2-setup.sh); a node that
	// hasn't enabled it must say so rather than pretend to provision.
	app, _, _, _ := newTestAppFull(t, func(cfg *config.Config) { cfg.IKEv2.Enabled = false })
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, body := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "IKEV2"})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s, want 503 when IKEv2 is disabled", resp.StatusCode, body)
	}
	var out ConnectResponseDto
	mustDecode(t, body, &out)
	if out.Success || out.Status != "UNAVAILABLE" {
		t.Fatalf("unexpected payload: %+v", out)
	}
}

func TestHandleConnectIKEv2ProvisionsScopedCredential(t *testing.T) {
	app, _, _, fi := newTestAppFull(t, func(cfg *config.Config) { cfg.IKEv2.Enabled = true })
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, body := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "IKEV2"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out ConnectResponseDto
	mustDecode(t, body, &out)

	// PROVISIONED, not ESTABLISHED: the credential exists but the client
	// still has to dial in, exactly like the OpenVPN path.
	if !out.Success || out.Status != "PROVISIONED" {
		t.Fatalf("unexpected ikev2 connect response: %+v", out)
	}
	if out.IKEv2Username == "" || out.IKEv2Password == "" || out.IKEv2ServerID == "" {
		t.Fatalf("missing IKEv2 credentials in response: %+v", out)
	}
	if out.IKEv2Username != out.SessionID {
		t.Errorf("IKEv2Username = %q, want it scoped to the session id %q", out.IKEv2Username, out.SessionID)
	}
	fi.mu.Lock()
	pinned := fi.provisioned[out.SessionID]
	fi.mu.Unlock()
	if pinned != out.VirtualIP {
		t.Errorf("strongSwan was pinned to %q but the API promised %q", pinned, out.VirtualIP)
	}
}

func TestHandleDisconnectReleasesIKEv2Session(t *testing.T) {
	app, _, _, fi := newTestAppFull(t, func(cfg *config.Config) { cfg.IKEv2.Enabled = true })
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	_, connectBody := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "IKEV2"})
	var connected ConnectResponseDto
	mustDecode(t, connectBody, &connected)
	if app.ikev2Pool.InUse() != 1 {
		t.Fatalf("ikev2Pool.InUse() = %d after connect, want 1", app.ikev2Pool.InUse())
	}

	doJSON(t, srv, http.MethodPost, "/api/v1/disconnect", DisconnectRequestDto{SessionID: connected.SessionID})

	fi.mu.Lock()
	_, stillProvisioned := fi.provisioned[connected.SessionID]
	calls := len(fi.deprovisionCalls)
	fi.mu.Unlock()
	if stillProvisioned || calls != 1 {
		t.Errorf("expected exactly one Deprovision and no leftover config (calls=%d, present=%v)", calls, stillProvisioned)
	}
	if app.ikev2Pool.InUse() != 0 {
		t.Errorf("ikev2Pool.InUse() = %d after disconnect, want 0", app.ikev2Pool.InUse())
	}
}

func TestHandleTelemetryIKEv2ReportsClientPerspective(t *testing.T) {
	app, _, _, fi := newTestAppFull(t, func(cfg *config.Config) { cfg.IKEv2.Enabled = true })
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	_, connectBody := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{ServerID: "test_node", Protocol: "IKEV2"})
	var connected ConnectResponseDto
	mustDecode(t, connectBody, &connected)

	// strongSwan counts from the server's side: bytes-in is what the client
	// uploaded, bytes-out what it downloaded.
	const bytesIn, bytesOut = 2_000, 9_000
	fi.setStat(connected.SessionID, bytesIn, bytesOut)

	// The first poll only establishes the sampling baseline, so it reports a
	// zero delta by design.
	doJSON(t, srv, http.MethodGet, "/api/v1/telemetry?sessionId="+connected.SessionID, nil)

	// Second poll: the counters have moved, and the response must carry the
	// difference (what the client will add to its own running total), with
	// download and upload the right way round.
	const moreIn, moreOut = 2_500, 14_000
	fi.setStat(connected.SessionID, moreIn, moreOut)

	_, body := doJSON(t, srv, http.MethodGet, "/api/v1/telemetry?sessionId="+connected.SessionID, nil)
	var telemetry ServerTelemetryDto
	mustDecode(t, body, &telemetry)

	if want := int64(moreOut - bytesOut); telemetry.TotalDownloadedBytes != want {
		t.Errorf("TotalDownloadedBytes = %d, want %d (delta of the server's bytes-out, not the cumulative %d)",
			telemetry.TotalDownloadedBytes, want, moreOut)
	}
	if want := int64(moreIn - bytesIn); telemetry.TotalUploadedBytes != want {
		t.Errorf("TotalUploadedBytes = %d, want %d (delta of the server's bytes-in, not the cumulative %d)",
			telemetry.TotalUploadedBytes, want, moreIn)
	}
	if telemetry.TrafficSamples == nil {
		t.Error("TrafficSamples is nil; it marshals to JSON null, which the client's non-null List<Float> rejects")
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
	// setStat takes the counters as WireGuard reports them, i.e. from the
	// server's side: rx = what the server received (the client's upload),
	// tx = what the server sent (the client's download).
	const serverReceived, serverSent = 1_000_000, 5_000_000
	fw.setStat(peerKey, serverReceived, serverSent)
	_, secondBody := doJSON(t, srv, http.MethodGet, "/api/v1/telemetry?sessionId="+connected.SessionID, nil)
	var second ServerTelemetryDto
	mustDecode(t, secondBody, &second)
	if second.DownloadSpeedMbps <= 0 || second.UploadSpeedMbps <= 0 {
		t.Fatalf("expected positive throughput after byte counters increased: %+v", second)
	}
	// The app shows these to a person looking at their own phone, so the
	// server's TX is their download. Getting this backwards swaps the two
	// numbers in the UI.
	if second.TotalDownloadedBytes != serverSent {
		t.Errorf("TotalDownloadedBytes = %d, want %d (the server's TX is the client's download)",
			second.TotalDownloadedBytes, serverSent)
	}
	if second.TotalUploadedBytes != serverReceived {
		t.Errorf("TotalUploadedBytes = %d, want %d (the server's RX is the client's upload)",
			second.TotalUploadedBytes, serverReceived)
	}
	if second.DownloadSpeedMbps <= second.UploadSpeedMbps {
		t.Errorf("download (%v) should exceed upload (%v) given 5MB down vs 1MB up",
			second.DownloadSpeedMbps, second.UploadSpeedMbps)
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
