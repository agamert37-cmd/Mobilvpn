// Package config loads the vpn-api server configuration from a JSON file.
//
// JSON (not YAML/TOML) is used deliberately: it needs zero third-party
// dependencies, which keeps the binary auditable and buildable offline on a
// freshly installed Ubuntu box (no `go mod download` required for a
// production build).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type WireGuardConfig struct {
	Enabled             bool     `json:"enabled"`
	Interface           string   `json:"interface"`
	ListenPort          int      `json:"listenPort"`
	SubnetCIDR          string   `json:"subnetCIDR"`
	ServerVirtualIP     string   `json:"serverVirtualIP"`
	DNS                 []string `json:"dns"`
	MTU                 int      `json:"mtu"`
	PersistentKeepalive int      `json:"persistentKeepaliveSeconds"`
	KeyDir              string   `json:"keyDir"`
}

type OpenVPNConfig struct {
	Enabled           bool   `json:"enabled"`
	UDPPort           int    `json:"udpPort"`
	TCPPort           int    `json:"tcpPort"`
	SubnetCIDR        string `json:"subnetCIDR"`
	EasyRSADir        string `json:"easyRsaDir"`
	ServerDir         string `json:"serverDir"`
	ManagementUDPAddr string `json:"managementUdpAddr"`
	ManagementTCPAddr string `json:"managementTcpAddr"`
	// ManagementPasswordFile points at a root-only file holding the shared
	// secret scripts/30-openvpn-setup.sh generates and configures into both
	// running openvpn processes' `management` directive. OpenVPN itself
	// refuses to start quietly here — it just logs a loud warning — if this
	// is left unset, so vpn-api treats a missing/unreadable file the same
	// way (falls back to no-auth, logged once at startup).
	ManagementPasswordFile string `json:"managementPasswordFile"`
	CCDDir                 string `json:"ccdDir"`
}

type DNSConfig struct {
	BlocklistEnabledDefault bool   `json:"blocklistEnabledDefault"`
	ResolverAddr            string `json:"resolverAddr"`
	BlocklistControlFile    string `json:"blocklistControlFile"`
}

type FleetConfig struct {
	SharedSecret          string `json:"sharedSecret"`
	HealthPollIntervalSec int    `json:"healthPollIntervalSeconds"`
	HealthPollTimeoutMs   int    `json:"healthPollTimeoutMs"`
	NodesFile             string `json:"nodesFile"`
}

// TLSConfig lets vpn-api terminate HTTPS itself using certificates a
// separate tool (certbot, see scripts/70-tls-certbot.sh) provisions and
// renews on disk — no ACME client library needed inside vpn-api, keeping
// it dependency-free. The certificate/key are re-read (see
// internal/httpapi/tlsreload.go) whenever their file mtimes change, so a
// certbot renewal is picked up without restarting vpn-api.
type TLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"certFile"`
	KeyFile  string `json:"keyFile"`
}

type RateLimitConfig struct {
	RequestsPerMinutePerIP int `json:"requestsPerMinutePerIP"`
	Burst                  int `json:"burst"`
	// MaxConcurrentSessions caps total active tunnels on this node. 0 means
	// zero capacity (new connects are refused — useful to drain a node for
	// maintenance without stopping it); a negative value opts into no cap.
	MaxConcurrentSessions int `json:"maxConcurrentSessions"`
}

type Config struct {
	ListenAddr         string `json:"listenAddr"`
	NodeID             string `json:"nodeId"`
	NodeRegion         string `json:"nodeRegion"`
	PublicEndpointHost string `json:"publicEndpointHost"`
	APIKey             string `json:"apiKey"`
	StateFile          string `json:"stateFile"`
	SessionIdleTimeout int    `json:"sessionIdleTimeoutSeconds"`
	// TrustProxyHeaders should stay false unless you put your own reverse
	// proxy/CDN in front of vpn-api (which terminates TLS itself by
	// default — see TLS below): otherwise a client could spoof its rate
	// limit key via X-Forwarded-For.
	TrustProxyHeaders bool            `json:"trustProxyHeaders"`
	TLS               TLSConfig       `json:"tls"`
	WireGuard         WireGuardConfig `json:"wireguard"`
	OpenVPN           OpenVPNConfig   `json:"openvpn"`
	DNS               DNSConfig       `json:"dns"`
	Fleet             FleetConfig     `json:"fleet"`
	RateLimit         RateLimitConfig `json:"rateLimit"`
}

// Default returns a configuration usable for local development / smoke
// testing without any config file on disk (all backends degrade gracefully
// when their underlying binaries or privileges are unavailable).
func Default() Config {
	return Config{
		ListenAddr:         "127.0.0.1:8080",
		NodeID:             "local_dev",
		NodeRegion:         "Local Development",
		PublicEndpointHost: "127.0.0.1",
		StateFile:          "./vpn-api-state.json",
		SessionIdleTimeout: 180,
		WireGuard: WireGuardConfig{
			Enabled:             true,
			Interface:           "wg0",
			ListenPort:          51820,
			SubnetCIDR:          "10.66.0.0/16",
			ServerVirtualIP:     "10.66.0.1",
			DNS:                 []string{"10.66.0.1"},
			MTU:                 1420,
			PersistentKeepalive: 25,
			KeyDir:              "/etc/wireguard",
		},
		OpenVPN: OpenVPNConfig{
			Enabled:                true,
			UDPPort:                1194,
			TCPPort:                443,
			SubnetCIDR:             "10.77.0.0/16",
			EasyRSADir:             "/etc/openvpn/easy-rsa",
			ServerDir:              "/etc/openvpn/server",
			ManagementUDPAddr:      "127.0.0.1:7505",
			ManagementTCPAddr:      "127.0.0.1:7506",
			ManagementPasswordFile: "/etc/openvpn/server/mgmt.pass",
			CCDDir:                 "/etc/openvpn/server/ccd",
		},
		DNS: DNSConfig{
			BlocklistEnabledDefault: true,
			ResolverAddr:            "10.66.0.1",
			BlocklistControlFile:    "/etc/unbound/unbound.conf.d/blocklist-toggle.conf",
		},
		Fleet: FleetConfig{
			HealthPollIntervalSec: 20,
			HealthPollTimeoutMs:   800,
			NodesFile:             "./nodes.json",
		},
		RateLimit: RateLimitConfig{
			RequestsPerMinutePerIP: 60,
			Burst:                  20,
			MaxConcurrentSessions:  500,
		},
	}
}

// Load reads a JSON config file at path and overlays it onto Default().
// A missing file is not an error: the caller gets sane defaults, which is
// what makes local development and `go test` painless.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("config: reading %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config: parsing %s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) IdleTimeout() time.Duration {
	if c.SessionIdleTimeout <= 0 {
		return 180 * time.Second
	}
	return time.Duration(c.SessionIdleTimeout) * time.Second
}
