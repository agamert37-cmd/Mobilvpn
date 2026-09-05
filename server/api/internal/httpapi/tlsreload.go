package httpapi

import (
	"crypto/tls"
	"fmt"
	"os"
	"sync"
	"time"
)

// CertReloader serves a TLS certificate loaded from disk and transparently
// picks up renewals (e.g. from certbot's own timer — see
// scripts/70-tls-certbot.sh) by re-reading the files whenever their mtime
// changes, without needing an ACME client inside vpn-api itself or a
// restart to pick up a renewed certificate.
type CertReloader struct {
	certFile, keyFile string

	mu          sync.RWMutex
	cert        *tls.Certificate
	certModTime time.Time
	lastChecked time.Time
}

func NewCertReloader(certFile, keyFile string) (*CertReloader, error) {
	r := &CertReloader{certFile: certFile, keyFile: keyFile}
	if err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *CertReloader) reload() error {
	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return fmt.Errorf("httpapi: loading TLS certificate: %w", err)
	}
	info, statErr := os.Stat(r.certFile)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.cert = &cert
	r.lastChecked = time.Now()
	if statErr == nil {
		r.certModTime = info.ModTime()
	}
	return nil
}

// maybeReload re-reads the certificate if the file's mtime has advanced
// since the last check. Checks are throttled to once a minute — a renewal
// happens at most every ~60 days, so stat()-ing on literally every TLS
// handshake would be pure overhead on a busy node.
func (r *CertReloader) maybeReload() {
	r.mu.RLock()
	stale := time.Since(r.lastChecked) > time.Minute
	r.mu.RUnlock()
	if !stale {
		return
	}

	info, err := os.Stat(r.certFile)
	if err != nil {
		return // keep serving the last good certificate
	}
	r.mu.Lock()
	r.lastChecked = time.Now()
	changed := info.ModTime().After(r.certModTime)
	r.mu.Unlock()

	if changed {
		_ = r.reload() // best-effort: a half-written new cert just means we keep the old one
	}
}

// GetCertificate is a tls.Config.GetCertificate callback.
func (r *CertReloader) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.maybeReload()
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cert, nil
}
