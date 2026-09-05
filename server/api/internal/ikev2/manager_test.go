package ikev2

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderSessionConfigPutsEverySettingOnItsOwnLine(t *testing.T) {
	// swanctl reads a value to the end of the line, so two settings sharing
	// a line silently invalidate the config. Guard the property directly.
	out := renderSessionConfig(sessionParams{
		Name: "sess_node_abc", ServerID: "vpn.example.com", ServerCert: "server-cert.pem",
		Username: "sess_node_abc", Password: "s3cret", VirtualIP: "10.88.0.5",
		DNSServers: []string{"10.66.0.1"},
	})

	for i, line := range strings.Split(out, "\n") {
		if strings.Count(line, "=") > 1 {
			t.Errorf("line %d packs more than one setting, which swanctl mis-parses: %q", i+1, line)
		}
	}
}

func TestRenderSessionConfigScopesCredentialToItsOwnConnection(t *testing.T) {
	out := renderSessionConfig(sessionParams{
		Name: "sess_a", ServerID: "vpn.example.com", ServerCert: "server-cert.pem",
		Username: "sess_a", Password: "pw", VirtualIP: "10.88.0.9",
	})

	for _, want := range []string{
		"eap_id = sess_a",      // only this session's identity may use it
		"pools = pool-sess_a",  // its own single-address pool
		"addrs = 10.88.0.9/32", // pinned, so the promised virtualIp is real
		"id = sess_a",
		"secret = pw",
		"mobike = yes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q:\n%s", want, out)
		}
	}
}

func TestProvisionRejectsInvalidSessionNames(t *testing.T) {
	m := NewManager(Config{ConfDir: t.TempDir()})
	for _, bad := range []string{"", "../escape", "has space", "semi;colon", strings.Repeat("x", 100)} {
		if _, err := m.Provision(bad, "10.88.0.2"); err == nil {
			t.Errorf("Provision(%q) should have been rejected", bad)
		}
	}
}

func TestReadyFalseWithoutServerKeypair(t *testing.T) {
	m := NewManager(Config{
		ConfDir: t.TempDir(), CertDir: t.TempDir(), KeyDir: t.TempDir(),
		ServerCert: "server-cert.pem", ServerKey: "server-key.pem",
	})
	if m.Ready() {
		t.Fatal("Ready() = true with no certificate or key on disk")
	}
}

// parseSAs fixture provenance, so a future reader knows what is verified
// and what isn't: the raw grammar (nested `section { key=value }` and
// `key=[list]`) is what `swanctl --list-conns --raw` actually emits on
// strongSwan 5.9.13, and every key name below was read out of the shipped
// libstrongswan-vici.so rather than from documentation or memory.
//
// The byte counters specifically could not be captured from a live SA
// while developing this: establishing a CHILD_SA needs kernel IPsec, which
// the development sandbox lacks (IKE_SA establishment and EAP auth *were*
// exercised against a real daemon). On a host with a working datapath,
// TestGeneratedConfigIsAcceptedByRealSwanctl plus a real client is the way
// to confirm the counters end to end.
const rawSAFixture = `list-sa event {sess_node_abc {uniqueid=3 version=2 state=ESTABLISHED ` +
	`local-host=203.0.113.10 local-port=4500 local-id=vpn.example.com ` +
	`remote-host=198.51.100.77 remote-port=4500 remote-id=sess_node_abc ` +
	`remote-eap-id=sess_node_abc remote-vips=[10.88.0.5] established=42 ` +
	`child-sas {net-1 {uniqueid=3 state=INSTALLED mode=TUNNEL ` +
	`bytes-in=1048576 packets-in=900 bytes-out=524288 packets-out=450}}}}
list-sas reply {}`

func TestParseSAsExtractsCountersAndAssignedAddress(t *testing.T) {
	stats := parseSAs(rawSAFixture)
	if stats == nil {
		t.Fatal("parseSAs returned nil for an established SA")
	}
	if !stats.Established {
		t.Error("Established = false, want true for state=ESTABLISHED")
	}
	if stats.BytesIn != 1048576 || stats.BytesOut != 524288 {
		t.Errorf("bytes = (%d, %d), want (1048576, 524288)", stats.BytesIn, stats.BytesOut)
	}
	if stats.AssignedIP != "10.88.0.5" {
		t.Errorf("AssignedIP = %q, want 10.88.0.5 (from remote-vips)", stats.AssignedIP)
	}
	if stats.RemoteAddress != "198.51.100.77" {
		t.Errorf("RemoteAddress = %q, want 198.51.100.77", stats.RemoteAddress)
	}
}

func TestParseSAsSumsMultipleChildSAs(t *testing.T) {
	// A rekeying tunnel briefly has two CHILD_SAs; their counters must add
	// up rather than the second one overwriting the first.
	raw := `list-sa event {sess_x {state=ESTABLISHED child-sas ` +
		`{net-1 {bytes-in=100 bytes-out=200} net-2 {bytes-in=50 bytes-out=25}}}}`
	stats := parseSAs(raw)
	if stats == nil {
		t.Fatal("parseSAs returned nil")
	}
	if stats.BytesIn != 150 || stats.BytesOut != 225 {
		t.Errorf("bytes = (%d, %d), want (150, 225) summed across child SAs", stats.BytesIn, stats.BytesOut)
	}
}

func TestParseSAsReturnsNilWhenNoSessionConnected(t *testing.T) {
	// What swanctl prints when the filter matches no SA.
	if stats := parseSAs("list-sas reply {}\n"); stats != nil {
		t.Fatalf("parseSAs = %+v for an empty reply, want nil", stats)
	}
}

// TestGeneratedConfigIsAcceptedByRealSwanctl is the test that matters most:
// it feeds our generated config to an actual strongSwan daemon and asserts
// the connection, pool and credential all load. Without it, a formatting
// mistake (see the end-of-line value rule above) would only surface in
// production.
func TestGeneratedConfigIsAcceptedByRealSwanctl(t *testing.T) {
	swanctl, err := exec.LookPath("swanctl")
	if err != nil {
		t.Skip("swanctl not installed; skipping real strongSwan acceptance test")
	}
	confDir := "/etc/swanctl/conf.d"
	if _, err := os.Stat(confDir); err != nil {
		t.Skip("no /etc/swanctl/conf.d; strongSwan not set up on this machine")
	}
	if out, err := exec.Command(swanctl, "--stats").CombinedOutput(); err != nil {
		t.Skipf("strongSwan daemon not reachable (%s); skipping", strings.TrimSpace(string(out)))
	}

	name := "sess_gotest_acceptance"
	path := filepath.Join(confDir, "mobilvpn-"+name+".conf")
	content := renderSessionConfig(sessionParams{
		Name: name, ServerID: "vpn.example.com", ServerCert: "server-cert.pem",
		Username: name, Password: "test-password", VirtualIP: "10.88.250.9",
		DNSServers: []string{"10.66.0.1"},
	})
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writing session config: %v", err)
	}
	t.Cleanup(func() {
		os.Remove(path)
		exec.Command(swanctl, "--load-all", "--noprompt").Run()
	})

	out, err := exec.Command(swanctl, "--load-all", "--noprompt").CombinedOutput()
	if err != nil {
		t.Fatalf("swanctl --load-all failed: %v\n%s", err, out)
	}
	text := string(out)
	// Scope the rejection check to config-loading lines. A bare
	// "failed to load" substring also matches strongSwan's unrelated
	// "plugin 'x': failed to load" warnings, which are normal on hosts
	// missing optional plugins and would make this test lie.
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "loading ") && strings.Contains(line, "failed") {
			t.Fatalf("strongSwan rejected the generated config: %s", line)
		}
		if strings.Contains(line, "config discarded") {
			t.Fatalf("strongSwan discarded the generated config: %s", line)
		}
	}
	if !strings.Contains(text, "loaded connection '"+name+"'") {
		t.Fatalf("connection %q was not loaded; swanctl said:\n%s", name, text)
	}
	if !strings.Contains(text, "loaded pool 'pool-"+name+"'") {
		t.Fatalf("pool for %q was not loaded; swanctl said:\n%s", name, text)
	}
}
