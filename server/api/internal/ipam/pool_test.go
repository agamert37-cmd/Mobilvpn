package ipam

import (
	"net"
	"testing"
)

func TestAllocateSkipsReserved(t *testing.T) {
	p, err := NewPool("test", "10.66.0.0/30", "10.66.0.1")
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	// /30 = 4 addresses: .0 network, .1 gateway, .2 usable, .3 broadcast.
	if p.Capacity() != 1 {
		t.Fatalf("capacity = %d, want 1", p.Capacity())
	}
	ip, err := p.Allocate("sess-1")
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if ip.String() != "10.66.0.2" {
		t.Fatalf("got %s, want 10.66.0.2 (network/gateway/broadcast must be skipped)", ip)
	}

	if _, err := p.Allocate("sess-2"); err == nil {
		t.Fatalf("expected pool exhaustion error, got none")
	}
}

func TestReleaseAllowsReuse(t *testing.T) {
	p, err := NewPool("test", "10.66.0.0/29", "10.66.0.1")
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	ip, err := p.Allocate("sess-1")
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	p.Release(ip)
	if got := p.InUse(); got != 0 {
		t.Fatalf("InUse after release = %d, want 0", got)
	}
	if _, err := p.Allocate("sess-2"); err != nil {
		t.Fatalf("Allocate after release: %v", err)
	}
}

func TestReserveConflict(t *testing.T) {
	p, err := NewPool("test", "10.66.0.0/29", "10.66.0.1")
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	if err := p.Reserve(net.ParseIP("10.66.0.2"), "sess-1"); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := p.Reserve(net.ParseIP("10.66.0.2"), "sess-2"); err == nil {
		t.Fatalf("expected conflict error reserving an already-owned IP for a different owner")
	}
	// Re-reserving for the same owner (idempotent restore-on-restart) must succeed.
	if err := p.Reserve(net.ParseIP("10.66.0.2"), "sess-1"); err != nil {
		t.Fatalf("idempotent Reserve: %v", err)
	}
}

func TestDeriveIPv6EmbedsTheV4Address(t *testing.T) {
	got, err := DeriveIPv6(net.ParseIP("10.66.0.2"), "fd00:66::/64")
	if err != nil {
		t.Fatalf("DeriveIPv6: %v", err)
	}
	if want := net.ParseIP("fd00:66::a42:2"); !got.Equal(want) {
		t.Fatalf("DeriveIPv6 = %s, want %s", got, want)
	}
}

func TestDeriveIPv6IsUniquePerV4(t *testing.T) {
	seen := map[string]string{}
	p, err := NewPool("test", "10.66.0.0/24", "10.66.0.1")
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	for i := 0; i < p.Capacity(); i++ {
		v4, err := p.Allocate("s")
		if err != nil {
			t.Fatalf("Allocate #%d: %v", i, err)
		}
		v6, err := DeriveIPv6(v4, "fd00:66::/64")
		if err != nil {
			t.Fatalf("DeriveIPv6(%s): %v", v4, err)
		}
		if prev, dup := seen[v6.String()]; dup {
			t.Fatalf("v6 collision: %s derived from both %s and %s", v6, prev, v4)
		}
		seen[v6.String()] = v4.String()
	}
}

func TestDeriveIPv6RejectsBadInput(t *testing.T) {
	if _, err := DeriveIPv6(net.ParseIP("fd00::1"), "fd00:66::/64"); err == nil {
		t.Errorf("expected an error passing an IPv6 address as the v4 argument")
	}
	if _, err := DeriveIPv6(net.ParseIP("10.66.0.2"), "10.66.0.0/16"); err == nil {
		t.Errorf("expected an error passing an IPv4 CIDR as the v6 prefix")
	}
	if _, err := DeriveIPv6(net.ParseIP("10.66.0.2"), "not-a-cidr"); err == nil {
		t.Errorf("expected an error on an unparseable prefix")
	}
	// A /112 leaves only 16 bits, too few to hold a 32-bit v4 address.
	if _, err := DeriveIPv6(net.ParseIP("10.66.0.2"), "fd00:66::/112"); err == nil {
		t.Errorf("expected an error on a prefix too small to embed an IPv4 address")
	}
}

func TestGatewayNeverAllocated(t *testing.T) {
	p, err := NewPool("test", "10.66.0.0/28", "10.66.0.5")
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	for i := 0; i < p.Capacity(); i++ {
		ip, err := p.Allocate("s")
		if err != nil {
			t.Fatalf("Allocate #%d: %v", i, err)
		}
		if ip.String() == "10.66.0.5" {
			t.Fatalf("gateway address was handed out to a peer")
		}
	}
}
