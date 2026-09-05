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

	first := sess.Sample(0, 0)
	if first.DownMbps != 0 || first.UpMbps != 0 || len(first.History) != 1 {
		t.Fatalf("first sample should be baseline-only: %+v", first)
	}

	// Direction/sign rather than an exact figure: wall-clock timing in CI
	// can't be relied on for an exact Mbps match.
	time.Sleep(10 * time.Millisecond)
	second := sess.Sample(1_000_000, 500_000)
	if second.DownMbps <= 0 || second.UpMbps <= 0 {
		t.Fatalf("expected positive throughput after bytes increased, got %+v", second)
	}
	if len(second.History) != 2 {
		t.Fatalf("history length = %d, want 2", len(second.History))
	}
}

// The client ADDS each reading to its own running total once a second
// (VpnViewModel.startLiveServerTelemetry), so what we report has to be the
// bytes moved since the previous poll. Reporting the cumulative counter
// makes the user's displayed total grow quadratically.
func TestSampleReportsPerIntervalDeltasNotCumulative(t *testing.T) {
	sess := &Session{ID: "sess-delta"}

	sess.Sample(1_000, 500) // establish a baseline

	second := sess.Sample(4_000, 1_500)
	if second.DownDelta != 3_000 {
		t.Errorf("DownDelta = %d, want 3000 (4000-1000), not the cumulative 4000", second.DownDelta)
	}
	if second.UpDelta != 1_000 {
		t.Errorf("UpDelta = %d, want 1000 (1500-500), not the cumulative 1500", second.UpDelta)
	}

	// Summing the deltas has to reconstruct the true total, which is the
	// whole point of sending them.
	third := sess.Sample(10_000, 2_000)
	if got := second.DownDelta + third.DownDelta; got != 9_000 {
		t.Errorf("deltas sum to %d, want 9000 (10000 - the 1000 baseline)", got)
	}
	if got := second.UpDelta + third.UpDelta; got != 1_500 {
		t.Errorf("up deltas sum to %d, want 1500 (2000 - the 500 baseline)", got)
	}
}

// A tunnel restart resets the backend's counter. Subtracting a larger
// previous reading would underflow uint64 into an astronomic delta, which
// the client would then add to its total.
func TestSampleHandlesCounterResetWithoutUnderflow(t *testing.T) {
	sess := &Session{ID: "sess-reset"}
	sess.Sample(5_000_000, 2_000_000)

	after := sess.Sample(120, 40) // tunnel restarted, counters back near zero
	if after.DownDelta != 0 || after.UpDelta != 0 {
		t.Fatalf("counter reset should report zero, got down=%d up=%d", after.DownDelta, after.UpDelta)
	}
	if after.DownMbps != 0 || after.UpMbps != 0 {
		t.Errorf("counter reset should report zero throughput, got %+v", after)
	}

	// And it recovers on the next poll rather than staying stuck.
	next := sess.Sample(1_120, 1_040)
	if next.DownDelta != 1_000 || next.UpDelta != 1_000 {
		t.Errorf("after reset, next delta = down %d up %d, want 1000/1000", next.DownDelta, next.UpDelta)
	}
}

// trafficSamples is a non-null List<Float> on the client and Moshi throws
// on an explicit null, so History must never marshal to null.
func TestSampleHistoryIsNeverNil(t *testing.T) {
	sess := &Session{ID: "sess-hist"}
	if got := sess.Sample(0, 0); got.History == nil {
		t.Fatal("History is nil on the first sample; it would marshal to JSON null")
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
