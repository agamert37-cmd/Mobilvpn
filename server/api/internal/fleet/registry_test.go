package fleet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileFallsBackToSelfOnly(t *testing.T) {
	r, err := Load(filepath.Join(t.TempDir(), "missing.json"), "node-a",
		Node{Country: "Türkiye", City: "İstanbul"}, "", time.Second, time.Second)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	views := r.Views()
	if len(views) != 1 || !views[0].IsSelf || views[0].ID != "node-a" {
		t.Fatalf("Views() = %+v, want exactly one self node", views)
	}
	if r.ActiveCount() != 1 {
		t.Fatalf("ActiveCount() = %d, want 1", r.ActiveCount())
	}
}

func TestLoadFromFileMarksSelfAndKeepsSiblings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.json")
	writeJSON(t, path, []Node{
		{ID: "node-a", Country: "Türkiye", City: "İstanbul"},
		{ID: "node-b", Country: "Almanya", City: "Frankfurt", InternalAPIURL: "http://10.0.0.2:8080"},
	})

	r, err := Load(path, "node-a", Node{}, "", time.Second, time.Second)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	self, ok := r.Self()
	if !ok || !self.IsSelf || self.ID != "node-a" {
		t.Fatalf("Self() = %+v, ok=%v", self, ok)
	}
	if sibling, ok := r.Lookup("node-b"); !ok || sibling.IsSelf {
		t.Fatalf("Lookup(node-b) = %+v, ok=%v, want a non-self sibling", sibling, ok)
	}
}

func TestSelfLoadFuncFeedsViews(t *testing.T) {
	r, err := Load(filepath.Join(t.TempDir(), "missing.json"), "node-a", Node{}, "", time.Second, time.Second)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r.SetSelfLoadFunc(func() int { return 42 })
	views := r.Views()
	if views[0].LoadPercent != 42 {
		t.Fatalf("self LoadPercent = %d, want 42", views[0].LoadPercent)
	}
}

func TestPollingUpdatesSiblingHealthAndLoad(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("X-Internal-Fleet-Secret") != "s3cr3t" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(internalStatusResponse{Healthy: true, LoadPercent: 77})
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.json")
	writeJSON(t, path, []Node{
		{ID: "node-a"},
		{ID: "node-b", InternalAPIURL: srv.URL},
	})

	r, err := Load(path, "node-a", Node{}, "s3cr3t", time.Hour, time.Second)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.StartPolling(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for {
		views := r.Views()
		found := false
		for _, v := range views {
			if v.ID == "node-b" && v.Healthy && v.LoadPercent == 77 {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sibling health/load was never reflected in Views(): %+v", views)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeJSON(t *testing.T, path string, v interface{}) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
