package ratelimit

import "testing"

func TestAllowRespectsBurstThenBlocks(t *testing.T) {
	l := New(60, 3) // 1/sec sustained, burst of 3
	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("request %d within burst should be allowed", i)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Fatalf("request beyond burst should be denied")
	}
}

func TestAllowIsPerKey(t *testing.T) {
	l := New(60, 1)
	if !l.Allow("1.1.1.1") {
		t.Fatalf("first request for 1.1.1.1 should be allowed")
	}
	if !l.Allow("2.2.2.2") {
		t.Fatalf("a different key must have its own independent bucket")
	}
	if l.Allow("1.1.1.1") {
		t.Fatalf("second immediate request for 1.1.1.1 should be denied (burst=1)")
	}
}
