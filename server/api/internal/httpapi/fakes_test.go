package httpapi

import (
	"context"
	"sync"

	"vpnapi/internal/openvpn"
	"vpnapi/internal/wireguard"
)

// fakeWG is a deterministic, in-memory stand-in for the real WireGuard
// manager, used so HTTP-layer tests can run anywhere (including this
// codebase's own sandbox, which has no kernel WireGuard support) while
// still exercising every success/failure path the handlers branch on.
type fakeWG struct {
	mu          sync.Mutex
	available   bool
	unavailMsg  string
	addPeerErr  error
	peers       map[string]string // pubkey -> allowed-ips
	stats       map[string]*wireguard.PeerStat
	removeCalls []string
}

func newFakeWG() *fakeWG {
	return &fakeWG{available: true, peers: map[string]string{}, stats: map[string]*wireguard.PeerStat{}}
}

func (f *fakeWG) Available() bool           { return f.available }
func (f *fakeWG) UnavailableReason() string { return f.unavailMsg }

func (f *fakeWG) AddPeer(ctx context.Context, publicKey, allowedIPCIDR string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addPeerErr != nil {
		return f.addPeerErr
	}
	f.peers[publicKey] = allowedIPCIDR
	return nil
}

func (f *fakeWG) RemovePeer(ctx context.Context, publicKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.peers, publicKey)
	f.removeCalls = append(f.removeCalls, publicKey)
	return nil
}

func (f *fakeWG) FindPeer(ctx context.Context, publicKey string) (*wireguard.PeerStat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.stats[publicKey]; ok {
		return s, nil
	}
	if _, ok := f.peers[publicKey]; ok {
		return &wireguard.PeerStat{PublicKey: publicKey}, nil
	}
	return nil, nil
}

func (f *fakeWG) setStat(publicKey string, rx, tx uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stats[publicKey] = &wireguard.PeerStat{PublicKey: publicKey, ReceiveBytes: rx, TransmitBytes: tx}
}

func (f *fakeWG) peerCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.peers)
}

// fakeOVPN is the OpenVPN-backend equivalent of fakeWG.
type fakeOVPN struct {
	mu               sync.Mutex
	ready            bool
	provisionErr     error
	provisioned      map[string]bool
	stats            map[string]*openvpn.ClientStat
	deprovisionCalls []string
}

func newFakeOVPN() *fakeOVPN {
	return &fakeOVPN{ready: true, provisioned: map[string]bool{}, stats: map[string]*openvpn.ClientStat{}}
}

func (f *fakeOVPN) Ready() bool { return f.ready }

func (f *fakeOVPN) Provision(cn, virtualIP, virtualNetmask string, useTCP bool) (*openvpn.ProvisionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.provisionErr != nil {
		return nil, f.provisionErr
	}
	f.provisioned[cn] = true
	return &openvpn.ProvisionResult{
		Credential: &openvpn.ClientCredential{CertPEM: "CERT", KeyPEM: "KEY", CAPEM: "CA", TAKey: "TA"},
		Profile:    "client\n# profile for " + cn,
	}, nil
}

func (f *fakeOVPN) Deprovision(cn string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.provisioned, cn)
	f.deprovisionCalls = append(f.deprovisionCalls, cn)
	return nil
}

func (f *fakeOVPN) Stats(cn string, useTCP bool) (*openvpn.ClientStat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stats[cn], nil
}

func (f *fakeOVPN) setStat(cn string, rx, tx uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stats[cn] = &openvpn.ClientStat{CommonName: cn, BytesReceived: rx, BytesSent: tx}
}
