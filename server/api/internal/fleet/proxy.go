package fleet

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ProxyClient forwards a tunnel-management request (connect/disconnect/
// telemetry) to a sibling node's own API when the client picked a serverId
// that isn't this node. It is a thin, honest passthrough: the sibling node
// runs the exact same handlers and DTO contract, so the response is piped
// back to the app verbatim rather than reinterpreted.
type ProxyClient struct {
	client *http.Client
	secret string
}

func NewProxyClient(timeout time.Duration, sharedSecret string) *ProxyClient {
	return &ProxyClient{
		client: &http.Client{Timeout: timeout},
		secret: sharedSecret,
	}
}

type ProxyResponse struct {
	StatusCode  int
	Body        []byte
	ContentType string
}

// Forward issues method+path against node.InternalAPIURL with body as the
// request payload (nil for GET), returning the remote status code, body,
// and content-type unmodified.
func (p *ProxyClient) Forward(ctx context.Context, node Node, method, path string, body []byte) (*ProxyResponse, error) {
	if node.InternalAPIURL == "" {
		return nil, fmt.Errorf("fleet: node %q has no internalApiUrl configured for automatic routing", node.ID)
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, node.InternalAPIURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("fleet: building proxy request to %s: %w", node.ID, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.secret != "" {
		req.Header.Set("X-Internal-Fleet-Secret", p.secret)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fleet: forwarding to node %s: %w", node.ID, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("fleet: reading proxied response from %s: %w", node.ID, err)
	}
	return &ProxyResponse{
		StatusCode:  resp.StatusCode,
		Body:        respBody,
		ContentType: resp.Header.Get("Content-Type"),
	}, nil
}
