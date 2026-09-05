// Package ikev2 drives strongSwan's modern swanctl control plane to
// provision one IKEv2/IPsec tunnel per session.
//
// IKEv2 exists here for the case WireGuard and OpenVPN don't cover well:
// phones roaming between Wi-Fi and cellular, where MOBIKE keeps the tunnel
// alive across an address change, and where Android/iOS both have a native
// IKEv2 client built into the OS.
//
// Every session gets its own swanctl connection, its own single-address
// pool and its own EAP credential, dropped into one file under
// swanctl's conf.d. That is what lets the API promise a specific virtual
// IP up front (as it already does for WireGuard and OpenVPN) and what
// keeps one session's credential from being usable on another session's
// connection.
package ikev2

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var ErrNotConfigured = errors.New("ikev2: strongSwan is not set up (run scripts/35-ikev2-setup.sh first)")

// validName mirrors the openvpn package's guard: section names are
// interpolated into a config file that a privileged daemon parses, so
// nothing but the session-ID alphabet is ever allowed through.
var validName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Config struct {
	// ConfDir is swanctl's include directory (/etc/swanctl/conf.d), which
	// its swanctl.conf pulls in via `include conf.d/*.conf`.
	ConfDir string
	// CertDir/KeyDir hold the server certificate and its key, used only to
	// report readiness — swanctl itself loads them by name.
	CertDir     string
	KeyDir      string
	ServerCert  string
	ServerKey   string
	CACertPath  string
	ServerID    string // the cert's SAN, what clients verify (e.g. vpn.example.com)
	DNSServers  []string
	SwanctlPath string
}

type Manager struct {
	cfg Config
}

func NewManager(cfg Config) *Manager {
	if cfg.SwanctlPath == "" {
		cfg.SwanctlPath = "swanctl"
	}
	return &Manager{cfg: cfg}
}

// Ready reports whether install-time setup has happened: the server
// keypair must exist and swanctl must be callable.
func (m *Manager) Ready() bool {
	if _, err := exec.LookPath(m.cfg.SwanctlPath); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(m.cfg.CertDir, m.cfg.ServerCert)); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(m.cfg.KeyDir, m.cfg.ServerKey))
	return err == nil
}

type Credential struct {
	Username string
	Password string
	ServerID string
	// CACertPEM lets a client pin this server's CA instead of trusting the
	// system store; empty when the CA file isn't readable.
	CACertPEM string
}

// Provision writes this session's connection/pool/credential and asks
// strongSwan to load it.
func (m *Manager) Provision(sessionID, virtualIP string) (*Credential, error) {
	if !validName.MatchString(sessionID) {
		return nil, fmt.Errorf("ikev2: invalid session name %q", sessionID)
	}
	if !m.Ready() {
		return nil, ErrNotConfigured
	}

	password, err := randomSecret()
	if err != nil {
		return nil, err
	}

	content := renderSessionConfig(sessionParams{
		Name:       sessionID,
		ServerID:   m.cfg.ServerID,
		ServerCert: m.cfg.ServerCert,
		Username:   sessionID, // unique per session, and not itself a secret
		Password:   password,
		VirtualIP:  virtualIP,
		DNSServers: m.cfg.DNSServers,
	})

	path := m.sessionPath(sessionID)
	// 0600: the file carries this session's EAP password.
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return nil, fmt.Errorf("ikev2: writing %s: %w", path, err)
	}
	if err := m.reload(); err != nil {
		_ = os.Remove(path) // don't leave a half-applied session behind
		return nil, err
	}

	cred := &Credential{Username: sessionID, Password: password, ServerID: m.cfg.ServerID}
	if m.cfg.CACertPath != "" {
		if ca, err := os.ReadFile(m.cfg.CACertPath); err == nil {
			cred.CACertPEM = string(ca)
		}
	}
	return cred, nil
}

// Deprovision tears the session down: the live SA is terminated first (so
// an already-connected client is actually dropped, not just prevented from
// reconnecting), then the config is removed and reloaded.
func (m *Manager) Deprovision(sessionID string) error {
	if !validName.MatchString(sessionID) {
		return fmt.Errorf("ikev2: invalid session name %q", sessionID)
	}
	// "no matching SAs to terminate found" is the normal case for a client
	// that never dialled in; swanctl exits 0 for it either way.
	_ = m.run("--terminate", "--ike", sessionID)

	if err := os.Remove(m.sessionPath(sessionID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ikev2: removing session config: %w", err)
	}
	return m.reload()
}

type SAStats struct {
	Established   bool
	BytesIn       uint64
	BytesOut      uint64
	AssignedIP    string
	RemoteAddress string
}

// Stats returns live counters for this session's IKE_SA, or (nil, nil)
// when the credential exists but no client has connected with it.
func (m *Manager) Stats(sessionID string) (*SAStats, error) {
	if !validName.MatchString(sessionID) {
		return nil, fmt.Errorf("ikev2: invalid session name %q", sessionID)
	}
	out, err := m.output("--list-sas", "--ike", sessionID, "--raw")
	if err != nil {
		return nil, err
	}
	stats := parseSAs(out)
	if stats == nil {
		return nil, nil
	}
	return stats, nil
}

func (m *Manager) sessionPath(sessionID string) string {
	return filepath.Join(m.cfg.ConfDir, "mobilvpn-"+sessionID+".conf")
}

func (m *Manager) reload() error {
	return m.run("--load-all", "--noprompt")
}

func (m *Manager) run(args ...string) error {
	out, err := exec.Command(m.cfg.SwanctlPath, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ikev2: swanctl %s failed: %w (%s)", strings.Join(args, " "), err, truncate(out, 400))
	}
	return nil
}

func (m *Manager) output(args ...string) (string, error) {
	out, err := exec.Command(m.cfg.SwanctlPath, args...).Output()
	if err != nil {
		return "", fmt.Errorf("ikev2: swanctl %s failed: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func randomSecret() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("ikev2: generating EAP secret: %w", err)
	}
	// URL-safe so it never needs quoting inside swanctl.conf.
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func truncate(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// parseSAs pulls the counters out of `swanctl --list-sas --ike <name> --raw`.
//
// The raw format is VICI's nested `key=value` / `section { ... }` dump. We
// don't need a full parser: the command is already filtered to one
// connection, so summing every child SA's byte counters in the output is
// exactly this session's traffic. Key names (bytes-in, bytes-out,
// remote-vips, state) were taken from the shipped
// libstrongswan-vici.so rather than from documentation.
func parseSAs(raw string) *SAStats {
	if !strings.Contains(raw, "bytes-in") && !strings.Contains(raw, "state=") {
		return nil // no SA for this connection
	}
	stats := &SAStats{}
	for _, tok := range tokenize(raw) {
		key, value, ok := strings.Cut(tok, "=")
		if !ok {
			continue
		}
		switch key {
		case "bytes-in":
			if n, err := strconv.ParseUint(value, 10, 64); err == nil {
				stats.BytesIn += n
			}
		case "bytes-out":
			if n, err := strconv.ParseUint(value, 10, 64); err == nil {
				stats.BytesOut += n
			}
		case "state":
			if value == "ESTABLISHED" {
				stats.Established = true
			}
		case "remote-host":
			if stats.RemoteAddress == "" {
				stats.RemoteAddress = value
			}
		}
	}
	if vip := firstListValue(raw, "remote-vips"); vip != "" {
		stats.AssignedIP = vip
	}
	return stats
}

// tokenize splits the raw dump into bare tokens, dropping the structural
// braces and bracket punctuation so `key=value` pairs stand alone.
func tokenize(raw string) []string {
	replacer := strings.NewReplacer("{", " ", "}", " ", "[", " ", "]", " ", ",", " ", "\n", " ")
	return strings.Fields(replacer.Replace(raw))
}

// firstListValue extracts the first element of a `key=[a b]` style list.
func firstListValue(raw, key string) string {
	idx := strings.Index(raw, key+"=[")
	if idx < 0 {
		return ""
	}
	rest := raw[idx+len(key)+2:]
	end := strings.IndexAny(rest, "]")
	if end < 0 {
		return ""
	}
	fields := strings.Fields(rest[:end])
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
