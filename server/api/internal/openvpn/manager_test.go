package openvpn

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPKIReadyFalseWithoutBootstrap(t *testing.T) {
	p := NewPKI(t.TempDir())
	if p.Ready() {
		t.Fatalf("Ready() = true for a directory with no pki/ca.crt")
	}
	if _, err := p.IssueClient("sess-abc"); !errors.Is(err, ErrPKINotInitialized) {
		t.Fatalf("IssueClient on uninitialized PKI = %v, want ErrPKINotInitialized", err)
	}
}

func TestIssueClientRejectsInvalidNames(t *testing.T) {
	p := NewPKI(t.TempDir())
	for _, bad := range []string{"", "../etc/passwd", "sess with spaces", "sess;rm -rf", strings.Repeat("a", 100)} {
		if _, err := p.IssueClient(bad); err == nil {
			t.Fatalf("IssueClient(%q) should have been rejected", bad)
		}
	}
}

func TestWriteAndRemoveCCD(t *testing.T) {
	dir := t.TempDir()
	if err := writeCCD(dir, "sess-1", "10.77.0.5", "255.255.0.0"); err != nil {
		t.Fatalf("writeCCD: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "sess-1"))
	if err != nil {
		t.Fatalf("reading CCD file: %v", err)
	}
	want := "ifconfig-push 10.77.0.5 255.255.0.0\n"
	if string(data) != want {
		t.Fatalf("CCD content = %q, want %q", data, want)
	}

	removeCCD(dir, "sess-1")
	if _, err := os.Stat(filepath.Join(dir, "sess-1")); !os.IsNotExist(err) {
		t.Fatalf("CCD file still present after removeCCD")
	}

	// Removing an already-absent CCD entry must not panic (disconnect is
	// idempotent even if the peer was never actually pinned).
	removeCCD(dir, "never-existed")
}

func TestRenderProfileEmbedsAllMaterialAndCorrectProto(t *testing.T) {
	cred := &ClientCredential{
		CertPEM: "CERT-DATA",
		KeyPEM:  "KEY-DATA",
		CAPEM:   "CA-DATA",
		TAKey:   "TA-DATA",
	}

	udp := renderProfile("vpn.example.com", false, 1194, 443, cred)
	for _, want := range []string{"proto udp", "remote vpn.example.com 1194", "CERT-DATA", "KEY-DATA", "CA-DATA", "TA-DATA"} {
		if !strings.Contains(udp, want) {
			t.Errorf("udp profile missing %q:\n%s", want, udp)
		}
	}

	tcp := renderProfile("vpn.example.com", true, 1194, 443, cred)
	if !strings.Contains(tcp, "proto tcp-client") || !strings.Contains(tcp, "remote vpn.example.com 443") {
		t.Errorf("tcp profile has wrong proto/port:\n%s", tcp)
	}
}
