package httpapi

import (
	"context"

	"vpnapi/internal/openvpn"
	"vpnapi/internal/wireguard"
)

// wireGuardBackend and openVPNBackend narrow *wireguard.Manager and
// *openvpn.Manager down to exactly what the HTTP handlers use. Depending on
// these small interfaces (rather than the concrete types directly) is what
// lets handlers_test.go exercise every routing/validation/lifecycle path
// with deterministic fakes, on any machine — including this codebase's own
// CI/dev sandbox, which has no kernel WireGuard support to actually stand
// up an interface against.
type wireGuardBackend interface {
	Available() bool
	UnavailableReason() string
	AddPeer(ctx context.Context, publicKey, allowedIPCIDR string) error
	RemovePeer(ctx context.Context, publicKey string) error
	FindPeer(ctx context.Context, publicKey string) (*wireguard.PeerStat, error)
}

type openVPNBackend interface {
	Ready() bool
	Provision(cn, virtualIP, virtualNetmask string, useTCP bool) (*openvpn.ProvisionResult, error)
	Deprovision(cn string) error
	Stats(cn string, useTCP bool) (*openvpn.ClientStat, error)
}

var (
	_ wireGuardBackend = (*wireguard.Manager)(nil)
	_ openVPNBackend   = (*openvpn.Manager)(nil)
)
