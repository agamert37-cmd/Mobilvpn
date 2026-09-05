package httpapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeSelfSignedCert generates a throwaway self-signed cert/key pair for
// commonName and writes PEM files at certPath/keyPath, using only the
// standard library (no `openssl` binary dependency for tests).
func writeSelfSignedCert(t *testing.T, certPath, keyPath, commonName string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	certOut, err := os.Create(certPath)
	if err != nil {
		t.Fatalf("creating %s: %v", certPath, err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("encoding cert: %v", err)
	}
	certOut.Close()

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshaling key: %v", err)
	}
	keyOut, err := os.Create(keyPath)
	if err != nil {
		t.Fatalf("creating %s: %v", keyPath, err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		t.Fatalf("encoding key: %v", err)
	}
	keyOut.Close()
}

func TestCertReloaderLoadsInitialCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	writeSelfSignedCert(t, certPath, keyPath, "first")

	r, err := NewCertReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewCertReloader: %v", err)
	}
	cert, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parsing served certificate: %v", err)
	}
	if parsed.Subject.CommonName != "first" {
		t.Fatalf("CommonName = %q, want %q", parsed.Subject.CommonName, "first")
	}
}

func TestCertReloaderPicksUpRenewalAfterMtimeChange(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	writeSelfSignedCert(t, certPath, keyPath, "before-renewal")

	r, err := NewCertReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewCertReloader: %v", err)
	}
	// Force the throttle window open so the next GetCertificate call
	// actually re-stats the file, instead of waiting a real minute.
	r.mu.Lock()
	r.lastChecked = time.Time{}
	r.mu.Unlock()

	time.Sleep(10 * time.Millisecond) // ensure a distinguishable mtime
	writeSelfSignedCert(t, certPath, keyPath, "after-renewal")

	cert, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate after renewal: %v", err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parsing served certificate: %v", err)
	}
	if parsed.Subject.CommonName != "after-renewal" {
		t.Fatalf("CertReloader served %q after a renewal, want %q (it should have picked up the mtime change)", parsed.Subject.CommonName, "after-renewal")
	}
}

func TestCertReloaderKeepsOldCertIfFileGoesMissing(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	writeSelfSignedCert(t, certPath, keyPath, "stable")

	r, err := NewCertReloader(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewCertReloader: %v", err)
	}
	r.mu.Lock()
	r.lastChecked = time.Time{}
	r.mu.Unlock()

	if err := os.Remove(certPath); err != nil {
		t.Fatalf("removing cert: %v", err)
	}

	cert, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate should keep serving the last good cert, got error: %v", err)
	}
	parsed, _ := x509.ParseCertificate(cert.Certificate[0])
	if parsed.Subject.CommonName != "stable" {
		t.Fatalf("expected the last good certificate to still be served, got CommonName=%q", parsed.Subject.CommonName)
	}
}
