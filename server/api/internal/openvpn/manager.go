package openvpn

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	EasyRSADir         string
	ServerDir          string
	CCDDir             string
	ManagementUDPAddr  string
	ManagementTCPAddr  string
	ManagementPassword string
	UDPPort            int
	TCPPort            int
	PublicHost         string
}

// Manager ties the PKI, the client-config-dir (CCD) virtual-IP pinning, and
// the two running openvpn server processes (UDP on :1194, TCP on :443,
// sharing one CA) into the single Provision/Deprovision/Stats surface the
// HTTP handlers use.
type Manager struct {
	cfg     Config
	pki     *PKI
	udpMgmt *Client
	tcpMgmt *Client
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:     cfg,
		pki:     NewPKI(cfg.EasyRSADir),
		udpMgmt: NewClient(cfg.ManagementUDPAddr, cfg.ManagementPassword),
		tcpMgmt: NewClient(cfg.ManagementTCPAddr, cfg.ManagementPassword),
	}
}

func (m *Manager) Ready() bool { return m.pki.Ready() }

type ProvisionResult struct {
	Credential *ClientCredential
	Profile    string
}

// Provision issues a fresh client certificate named cn (by convention, the
// session ID — see httpapi's connect handler) and pins its tunnel address
// via a client-config-dir entry, so the address advertised to the app
// (ConnectResponseDto.virtualIp) is the address OpenVPN will actually
// assign once the client dials in with this credential.
func (m *Manager) Provision(cn, virtualIP, virtualNetmask string, useTCP bool) (*ProvisionResult, error) {
	cred, err := m.pki.IssueClient(cn)
	if err != nil {
		return nil, err
	}
	if err := writeCCD(m.cfg.CCDDir, cn, virtualIP, virtualNetmask); err != nil {
		return nil, fmt.Errorf("openvpn: pinning virtual IP: %w", err)
	}
	return &ProvisionResult{
		Credential: cred,
		Profile:    renderProfile(m.cfg.PublicHost, useTCP, m.cfg.UDPPort, m.cfg.TCPPort, cred),
	}, nil
}

// Deprovision immediately kills any live session for cn, revokes its
// certificate so it can never reconnect, and removes its CCD pin. Unlike
// WireGuard (where removing the peer is instantaneous and sufficient),
// OpenVPN client credentials are longer-lived artifacts handed to the
// device, so a real disconnect has to be a real revocation.
func (m *Manager) Deprovision(cn string) error {
	_ = m.udpMgmt.Kill(cn) // best-effort: client may be on the other proto, or already gone
	_ = m.tcpMgmt.Kill(cn)
	removeCCD(m.cfg.CCDDir, cn)
	if err := m.pki.RevokeClient(cn); err != nil {
		return err
	}
	return m.pki.PublishCRL(filepath.Join(m.cfg.ServerDir, "crl.pem"))
}

// Stats returns live throughput counters for cn if it currently has an
// established OpenVPN tunnel, or (nil, nil) if the credential was issued
// but the client hasn't connected yet (or has disconnected).
func (m *Manager) Stats(cn string, useTCP bool) (*ClientStat, error) {
	if useTCP {
		return m.tcpMgmt.FindClient(cn)
	}
	return m.udpMgmt.FindClient(cn)
}

func writeCCD(dir, name, ip, netmask string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	content := fmt.Sprintf("ifconfig-push %s %s\n", ip, netmask)
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
}

func removeCCD(dir, name string) {
	_ = os.Remove(filepath.Join(dir, name))
}

// renderProfile builds a complete, self-contained .ovpn client profile
// (certs and the tls-crypt key embedded inline) so provisioning a device
// never depends on shipping extra files alongside the API response.
func renderProfile(host string, useTCP bool, udpPort, tcpPort int, cred *ClientCredential) string {
	proto := "udp"
	port := udpPort
	if useTCP {
		proto = "tcp-client"
		port = tcpPort
	}

	var b strings.Builder
	fmt.Fprintf(&b, "client\ndev tun\nproto %s\nremote %s %d\n", proto, host, port)
	b.WriteString("resolv-retry infinite\nnobind\npersist-key\npersist-tun\n")
	b.WriteString("remote-cert-tls server\ncipher AES-256-GCM\nauth SHA256\nverb 3\n\n")
	fmt.Fprintf(&b, "<ca>\n%s\n</ca>\n", strings.TrimSpace(cred.CAPEM))
	fmt.Fprintf(&b, "<cert>\n%s\n</cert>\n", strings.TrimSpace(cred.CertPEM))
	fmt.Fprintf(&b, "<key>\n%s\n</key>\n", strings.TrimSpace(cred.KeyPEM))
	fmt.Fprintf(&b, "<tls-crypt>\n%s\n</tls-crypt>\n", strings.TrimSpace(cred.TAKey))
	return b.String()
}
