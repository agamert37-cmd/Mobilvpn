// Package ipam allocates virtual tunnel IP addresses out of a CIDR block.
//
// It is intentionally a plain in-memory bitmap-free map guarded by a mutex:
// tunnel pools top out at low tens of thousands of addresses (a /16 at
// most), so a map lookup is more than fast enough and far simpler to get
// right than a bitset.
package ipam

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
)

type Pool struct {
	mu        sync.Mutex
	name      string
	network   uint32
	maskLen   int
	numHosts  uint32 // number of usable host addresses (excludes network/broadcast/gateway)
	gateway   uint32
	allocated map[uint32]string // ip -> owner id (session id), for diagnostics
	cursor    uint32
}

// NewPool builds an allocator over cidr (e.g. "10.66.0.0/16"). gatewayIP
// (e.g. "10.66.0.1", normally the server's own tunnel address) is reserved
// and never handed out to a peer.
func NewPool(name, cidr, gatewayIP string) (*Pool, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("ipam: invalid CIDR %q: %w", cidr, err)
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("ipam: only IPv4 pools are supported, got %q", cidr)
	}
	hostBits := bits - ones
	if hostBits < 2 {
		return nil, fmt.Errorf("ipam: CIDR %q is too small for a tunnel pool", cidr)
	}
	total := uint32(1) << uint(hostBits)

	gw := net.ParseIP(gatewayIP)
	if gw == nil || gw.To4() == nil {
		return nil, fmt.Errorf("ipam: invalid gateway IP %q", gatewayIP)
	}
	if !ipnet.Contains(gw) {
		return nil, fmt.Errorf("ipam: gateway %q is not inside %q", gatewayIP, cidr)
	}
	gwOffset := ipToUint32(gw.To4()) - ipToUint32(ipnet.IP.To4())
	if gwOffset == 0 || gwOffset == total-1 {
		return nil, fmt.Errorf("ipam: gateway %q must not be the network or broadcast address", gatewayIP)
	}

	p := &Pool{
		name:      name,
		network:   ipToUint32(ipnet.IP.To4()),
		maskLen:   ones,
		numHosts:  total - 2, // exclude network + broadcast addresses
		gateway:   ipToUint32(gw.To4()),
		allocated: make(map[uint32]string),
		cursor:    1,
	}
	// Reserve network address(offset 0), broadcast (offset total-1) and the
	// gateway implicitly by pre-marking them as allocated to "reserved".
	p.allocated[p.network] = "reserved:network"
	p.allocated[p.network+total-1] = "reserved:broadcast"
	p.allocated[p.gateway] = "reserved:gateway"
	return p, nil
}

// Allocate hands out the next free address in the pool for owner (typically
// a session ID), skipping the round-robin cursor forward so repeated churn
// does not immediately reuse an address that was just released (a small
// privacy nicety: avoids trivially correlating "new session got the IP a
// just-departed session had").
func (p *Pool) Allocate(owner string) (net.IP, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	total := p.numHosts + 2
	for i := uint32(0); i < total; i++ {
		offset := (p.cursor + i) % total
		if offset == 0 {
			continue
		}
		candidate := p.network + offset
		if _, taken := p.allocated[candidate]; taken {
			continue
		}
		p.allocated[candidate] = owner
		p.cursor = (offset + 1) % total
		return uint32ToIP(candidate), nil
	}
	return nil, fmt.Errorf("ipam: pool %q exhausted (%d addresses)", p.name, p.numHosts)
}

// Reserve marks a specific address as taken (used to restore state on
// startup, or to pin a well-known address such as an OpenVPN CCD entry).
func (p *Pool) Reserve(ip net.IP, owner string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	v4 := ip.To4()
	if v4 == nil {
		return fmt.Errorf("ipam: %q is not an IPv4 address", ip)
	}
	key := ipToUint32(v4)
	if existing, taken := p.allocated[key]; taken && existing != owner {
		return fmt.Errorf("ipam: %s already allocated to %s", ip, existing)
	}
	p.allocated[key] = owner
	return nil
}

// Release returns an address to the free pool.
func (p *Pool) Release(ip net.IP) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v4 := ip.To4()
	if v4 == nil {
		return
	}
	delete(p.allocated, ipToUint32(v4))
}

func (p *Pool) InUse() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.allocated) - 3 // minus the three permanently reserved slots
}

// Capacity returns how many peer addresses this pool can actually hand out
// (host addresses in the CIDR, minus the one reserved for the gateway).
func (p *Pool) Capacity() int {
	return int(p.numHosts) - 1
}

func ipToUint32(ip net.IP) uint32 {
	return binary.BigEndian.Uint32(ip.To4())
}

func uint32ToIP(v uint32) net.IP {
	b := make(net.IP, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}
