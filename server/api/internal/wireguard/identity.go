package wireguard

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnsureServerIdentity loads the server's own WireGuard keypair from
// <keyDir>/server_private.key, generating and persisting a fresh one on
// first run. The private key file is written with 0600 permissions before
// any key material touches it (os.WriteFile applies the mode atomically at
// creation, so there is no window where the key is world-readable).
func EnsureServerIdentity(keyDir string) (privateKey, publicKey string, err error) {
	privPath := filepath.Join(keyDir, "server_private.key")

	if data, readErr := os.ReadFile(privPath); readErr == nil {
		priv := trimKey(data)
		pub, derivErr := PublicFromPrivate(priv)
		if derivErr != nil {
			return "", "", fmt.Errorf("wireguard: existing key at %s is invalid: %w", privPath, derivErr)
		}
		return priv, pub, nil
	} else if !os.IsNotExist(readErr) {
		return "", "", fmt.Errorf("wireguard: reading %s: %w", privPath, readErr)
	}

	priv, pub, err := GenerateKeypair()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return "", "", fmt.Errorf("wireguard: creating %s: %w", keyDir, err)
	}
	if err := os.WriteFile(privPath, []byte(priv+"\n"), 0600); err != nil {
		return "", "", fmt.Errorf("wireguard: writing %s: %w", privPath, err)
	}
	pubPath := filepath.Join(keyDir, "server_public.key")
	if err := os.WriteFile(pubPath, []byte(pub+"\n"), 0644); err != nil {
		return "", "", fmt.Errorf("wireguard: writing %s: %w", pubPath, err)
	}
	return priv, pub, nil
}

func trimKey(data []byte) string {
	s := string(data)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
