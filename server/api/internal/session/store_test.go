package session

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPutGetDelete(t *testing.T) {
	s := NewStore("")
	sess := &Session{ID: "sess-1", Protocol: ProtocolWireGuard, VirtualIP: "10.66.0.2"}
	s.Put(sess)

	got, ok := s.Get("sess-1")
	if !ok || got.VirtualIP != "10.66.0.2" {
		t.Fatalf("Get returned %+v, ok=%v", got, ok)
	}

	deleted, ok := s.Delete("sess-1")
	if !ok || deleted.ID != "sess-1" {
		t.Fatalf("Delete returned %+v, ok=%v", deleted, ok)
	}
	if _, ok := s.Get("sess-1"); ok {
		t.Fatalf("session still present after Delete")
	}
}

func TestExpireIdle(t *testing.T) {
	s := NewStore("")
	fresh := &Session{ID: "fresh", LastSeenAt: time.Now()}
	stale := &Session{ID: "stale", LastSeenAt: time.Now().Add(-time.Hour)}
	s.Put(fresh)
	s.Put(stale)

	expired := s.ExpireIdle(time.Minute)
	if len(expired) != 1 || expired[0].ID != "stale" {
		t.Fatalf("ExpireIdle = %+v, want only 'stale'", expired)
	}
	if _, ok := s.Get("fresh"); !ok {
		t.Fatalf("fresh session was incorrectly expired")
	}
	if _, ok := s.Get("stale"); ok {
		t.Fatalf("stale session was not removed from the store")
	}
}

func TestPersistAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "state.json")

	s1 := NewStore(path)
	s1.Put(&Session{
		ID:        "sess-1",
		NodeID:    "node-a",
		Protocol:  ProtocolWireGuard,
		VirtualIP: "10.66.0.2",
		PeerKey:   "abc123pubkey",
		CreatedAt: time.Now().Truncate(time.Second),
	})
	if err := s1.Persist(); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	s2 := NewStore(path)
	restored, err := s2.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(restored) != 1 || restored[0].ID != "sess-1" || restored[0].PeerKey != "abc123pubkey" {
		t.Fatalf("Load restored = %+v", restored)
	}
	if got, ok := s2.Get("sess-1"); !ok || got.VirtualIP != "10.66.0.2" {
		t.Fatalf("session not queryable after Load: %+v ok=%v", got, ok)
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "does-not-exist.json"))
	restored, err := s.Load()
	if err != nil {
		t.Fatalf("Load on missing file returned error: %v", err)
	}
	if len(restored) != 0 {
		t.Fatalf("expected no sessions, got %d", len(restored))
	}
}

func TestSampleComputesThroughputBetweenPolls(t *testing.T) {
	sess := &Session{ID: "sess-1"}

	down, up, hist := sess.Sample(0, 0)
	if down != 0 || up != 0 || len(hist) != 1 {
		t.Fatalf("first sample should be baseline-only: down=%v up=%v hist=%v", down, up, hist)
	}

	// Simulate ~1MB/s down and ~0.5MB/s up over a fixed synthetic interval by
	// directly manipulating the unexported baseline via a second Sample call
	// and checking direction/sign rather than an exact figure (wall-clock
	// timing in CI can't be relied on for an exact Mbps match).
	time.Sleep(10 * time.Millisecond)
	down2, up2, hist2 := sess.Sample(1_000_000, 500_000)
	if down2 <= 0 || up2 <= 0 {
		t.Fatalf("expected positive throughput after bytes increased, got down=%v up=%v", down2, up2)
	}
	if len(hist2) != 2 {
		t.Fatalf("history length = %d, want 2", len(hist2))
	}
}

func TestCountByProtocol(t *testing.T) {
	s := NewStore("")
	s.Put(&Session{ID: "a", Protocol: ProtocolWireGuard})
	s.Put(&Session{ID: "b", Protocol: ProtocolWireGuard})
	s.Put(&Session{ID: "c", Protocol: ProtocolOpenVPNTCP})

	if n := s.CountByProtocol(ProtocolWireGuard); n != 2 {
		t.Fatalf("CountByProtocol(WIREGUARD) = %d, want 2", n)
	}
	if n := s.CountByProtocol(ProtocolOpenVPNUDP); n != 0 {
		t.Fatalf("CountByProtocol(OPENVPN_UDP) = %d, want 0", n)
	}
}
