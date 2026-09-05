package wireguard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureServerIdentityGeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()

	priv1, pub1, err := EnsureServerIdentity(dir)
	if err != nil {
		t.Fatalf("EnsureServerIdentity (first call): %v", err)
	}
	if err := ValidatePublicKey(pub1); err != nil {
		t.Fatalf("generated public key invalid: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "server_private.key"))
	if err != nil {
		t.Fatalf("private key file missing: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("private key file mode = %o, want 0600", perm)
	}

	// Second call must load the same identity rather than regenerating.
	priv2, pub2, err := EnsureServerIdentity(dir)
	if err != nil {
		t.Fatalf("EnsureServerIdentity (second call): %v", err)
	}
	if priv1 != priv2 || pub1 != pub2 {
		t.Fatalf("identity changed across calls: (%s,%s) != (%s,%s)", priv1, pub1, priv2, pub2)
	}
}
