package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"vpnapi/internal/config"
	"vpnapi/internal/fleet"
)

// TestFleetProxyingRoutesToSiblingNode verifies the multi-node story end to
// end: the Android app only ever configures ONE backend URL, but that
// node can transparently forward connect/disconnect/telemetry to a sibling
// node when the user picks a server that isn't the one they're talking to
// — with no shared database between the two, just the session ID's
// embedded node prefix (see ids.go).
func TestFleetProxyingRoutesToSiblingNode(t *testing.T) {
	nodeB, nodeBWG, _ := newTestApp(t, func(cfg *config.Config) { cfg.NodeID = "node_b" })
	nodeBSrv := httptest.NewServer(nodeB.Routes())
	defer nodeBSrv.Close()

	nodeA, nodeAWG, _ := newTestApp(t, func(cfg *config.Config) {
		cfg.NodeID = "node_a"
		nodes := []fleet.Node{
			{ID: "node_a"},
			{ID: "node_b", InternalAPIURL: nodeBSrv.URL},
		}
		data, err := json.Marshal(nodes)
		if err != nil {
			t.Fatalf("marshal nodes.json fixture: %v", err)
		}
		if err := os.WriteFile(cfg.Fleet.NodesFile, data, 0644); err != nil {
			t.Fatalf("writing nodes.json fixture: %v", err)
		}
	})
	nodeASrv := httptest.NewServer(nodeA.Routes())
	defer nodeASrv.Close()

	// The client's backendUrl is node_a's the whole time; it asks for
	// node_b by serverId, exactly as picking a different entry from
	// GET /api/v1/servers would produce.
	resp, body := doJSON(t, nodeASrv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{
		ServerID: "node_b", Protocol: "WIREGUARD",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("connect via proxy: status = %d, body = %s", resp.StatusCode, body)
	}
	var connected ConnectResponseDto
	mustDecode(t, body, &connected)

	if !connected.Success || connected.Status != "ESTABLISHED" {
		t.Fatalf("unexpected proxied connect response: %+v", connected)
	}
	if !strings.HasPrefix(connected.SessionID, "sess_node_b_") {
		t.Fatalf("sessionId = %q, want it minted by node_b (sess_node_b_*)", connected.SessionID)
	}
	if nodeAWG.peerCount() != 0 {
		t.Fatalf("node_a must not provision anything locally for a connect it only proxied")
	}
	if nodeBWG.peerCount() != 1 {
		t.Fatalf("node_b should have provisioned exactly one peer, got %d", nodeBWG.peerCount())
	}

	// Telemetry for that session, still asked of node_a, must be re-proxied
	// to node_b (the only place that actually knows about it).
	tResp, tBody := doJSON(t, nodeASrv, http.MethodGet, "/api/v1/telemetry?sessionId="+connected.SessionID, nil)
	if tResp.StatusCode != http.StatusOK {
		t.Fatalf("proxied telemetry: status = %d, body = %s", tResp.StatusCode, tBody)
	}
	var telemetry ServerTelemetryDto
	mustDecode(t, tBody, &telemetry)
	if telemetry.VirtualIP != connected.VirtualIP {
		t.Fatalf("proxied telemetry VirtualIP = %q, want %q", telemetry.VirtualIP, connected.VirtualIP)
	}

	// Disconnect, also asked of node_a, must reach node_b and actually
	// release node_b's peer/IP — not silently no-op locally on node_a.
	dResp, dBody := doJSON(t, nodeASrv, http.MethodPost, "/api/v1/disconnect", DisconnectRequestDto{SessionID: connected.SessionID})
	if dResp.StatusCode != http.StatusOK {
		t.Fatalf("proxied disconnect: status = %d, body = %s", dResp.StatusCode, dBody)
	}
	if nodeBWG.peerCount() != 0 {
		t.Fatalf("node_b's peer should have been removed by the proxied disconnect")
	}
	if nodeB.wgPool.InUse() != 0 {
		t.Fatalf("node_b's virtual IP should have been released by the proxied disconnect")
	}
}

func TestConnectToNodeWithNoInternalURLIsRejected(t *testing.T) {
	app, _, _ := newTestApp(t, func(cfg *config.Config) {
		nodes := []fleet.Node{
			{ID: "test_node"},
			{ID: "display_only_node"}, // advertised, but no automatic routing configured
		}
		data, _ := json.Marshal(nodes)
		if err := os.WriteFile(cfg.Fleet.NodesFile, data, 0644); err != nil {
			t.Fatalf("writing nodes.json fixture: %v", err)
		}
	})
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, body := doJSON(t, srv, http.MethodPost, "/api/v1/connect", ConnectRequestDto{
		ServerID: "display_only_node", Protocol: "WIREGUARD",
	})
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s, want 502 for a node with no internalApiUrl", resp.StatusCode, body)
	}
}
