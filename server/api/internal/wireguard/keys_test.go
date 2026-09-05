package wireguard

import (
	"bytes"
	"os/exec"
	"testing"
)

func TestGenerateKeypairIsWellFormed(t *testing.T) {
	priv, pub, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	if err := ValidatePublicKey(pub); err != nil {
		t.Fatalf("generated public key failed validation: %v", err)
	}
	rederivedPub, err := PublicFromPrivate(priv)
	if err != nil {
		t.Fatalf("PublicFromPrivate: %v", err)
	}
	if rederivedPub != pub {
		t.Fatalf("re-derived public key %q != original %q", rederivedPub, pub)
	}
}

func TestGenerateKeypairDistinctEachCall(t *testing.T) {
	_, pub1, _ := GenerateKeypair()
	_, pub2, _ := GenerateKeypair()
	if pub1 == pub2 {
		t.Fatalf("two consecutive GenerateKeypair calls produced the same public key")
	}
}

// TestInteropWithRealWgTool cross-validates our pure-Go X25519 public key
// derivation against the actual `wg` binary (part of wireguard-tools), when
// it's installed on the machine running `go test`. This is the test that
// actually proves our keys are usable by real WireGuard, not just
// internally self-consistent.
func TestInteropWithRealWgTool(t *testing.T) {
	wgPath, err := exec.LookPath("wg")
	if err != nil {
		t.Skip("wg binary not installed; skipping real wireguard-tools interop check")
	}

	priv, pub, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}

	cmd := exec.Command(wgPath, "pubkey")
	cmd.Stdin = bytes.NewBufferString(priv + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running `wg pubkey`: %v", err)
	}
	wgComputedPub := string(bytes.TrimSpace(out))

	if wgComputedPub != pub {
		t.Fatalf("wg pubkey computed %q, our code computed %q for the same private key", wgComputedPub, pub)
	}
}
