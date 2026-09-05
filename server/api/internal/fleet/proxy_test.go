package fleet

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProxyForwardsMethodBodyAndSecretHeader(t *testing.T) {
	var gotMethod, gotPath, gotSecret, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotMethod = req.Method
		gotPath = req.URL.Path
		gotSecret = req.Header.Get("X-Internal-Fleet-Secret")
		b, _ := io.ReadAll(req.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	p := NewProxyClient(2*time.Second, "s3cr3t")
	node := Node{ID: "node-b", InternalAPIURL: srv.URL}

	resp, err := p.Forward(context.Background(), node, http.MethodPost, "/api/v1/connect", []byte(`{"serverId":"node-b"}`))
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("StatusCode = %d, want 201", resp.StatusCode)
	}
	if string(resp.Body) != `{"success":true}` {
		t.Errorf("Body = %q", resp.Body)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/connect" {
		t.Errorf("upstream saw method=%q path=%q", gotMethod, gotPath)
	}
	if gotSecret != "s3cr3t" {
		t.Errorf("upstream saw secret=%q, want s3cr3t", gotSecret)
	}
	if gotBody != `{"serverId":"node-b"}` {
		t.Errorf("upstream saw body=%q", gotBody)
	}
}

func TestProxyRejectsNodeWithoutInternalURL(t *testing.T) {
	p := NewProxyClient(time.Second, "")
	_, err := p.Forward(context.Background(), Node{ID: "display-only"}, http.MethodGet, "/api/v1/health", nil)
	if err == nil {
		t.Fatalf("expected an error forwarding to a node with no InternalAPIURL")
	}
}
