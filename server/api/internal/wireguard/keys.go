package wireguard

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// GenerateKeypair produces a WireGuard-format (base64 of 32 raw bytes)
// Curve25519 keypair using only the Go standard library (crypto/ecdh, added
// in Go 1.20, implements X25519). This avoids a third-party crypto
// dependency purely to shell out to `wg genkey`, and lets the server
// generate client keys even on a host where wireguard-tools happens not to
// be installed yet.
//
// The private scalar is clamped per RFC 7748 before use, matching the byte
// pattern `wg genkey` itself produces. This is a convention, not a
// correctness requirement: X25519 clamps internally on every scalar
// multiplication regardless of whether the stored bytes look pre-clamped,
// so the resulting keys are fully interoperable with any WireGuard peer
// either way.
func GenerateKeypair() (privateKeyB64, publicKeyB64 string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("wireguard: reading randomness: %w", err)
	}
	raw[0] &= 248
	raw[31] &= 127
	raw[31] |= 64

	priv, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", "", fmt.Errorf("wireguard: deriving key pair: %w", err)
	}
	return base64.StdEncoding.EncodeToString(priv.Bytes()),
		base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()),
		nil
}

// PublicFromPrivate re-derives the public key for a stored private key, used
// when loading a persisted server identity from disk at startup.
func PublicFromPrivate(privateKeyB64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(privateKeyB64)
	if err != nil {
		return "", fmt.Errorf("wireguard: private key is not valid base64: %w", err)
	}
	priv, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", fmt.Errorf("wireguard: invalid private key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()), nil
}

// ValidatePublicKey checks that s decodes to exactly 32 bytes, which is all
// that's required for a syntactically valid WireGuard public key.
func ValidatePublicKey(s string) error {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("wireguard: public key is not valid base64: %w", err)
	}
	if len(raw) != 32 {
		return fmt.Errorf("wireguard: public key must decode to 32 bytes, got %d", len(raw))
	}
	return nil
}
