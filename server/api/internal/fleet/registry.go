// Package fleet maintains the list of VPN exit nodes this API instance
// knows about (itself, plus optionally sibling nodes running the same
// stack) and keeps a live, cached view of their health/load so /api/v1/servers
// stays fast (it is served from cache, never blocks on a network round
// trip to another node).
package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

type Node struct {
	ID                  string   `json:"id"`
	Country             string   `json:"country"`
	City                string   `json:"city"`
	FlagEmoji           string   `json:"flagEmoji"`
	IPAddress           string   `json:"ipAddress"`
	BaselinePingMs      int      `json:"baselinePingMs"`
	BaselineLoadPercent int      `json:"baselineLoadPercent"`
	Categories          []string `json:"categories"`
	IsFastest           bool     `json:"isFastest"`
	IsSelf              bool     `json:"isSelf"`
	// InternalAPIURL, when set on a non-self node, is a base URL (reachable
	// only on a private/trusted network, e.g. a VPN mesh or VPC-internal
	// address) this node forwards connect/disconnect/telemetry calls to
	// when a client picks that node. Left empty, the node is display-only.
	InternalAPIURL string `json:"internalApiUrl"`
}

type liveStatus struct {
	healthy     bool
	loadPercent int
	observedAt  time.Time
}

type NodeView struct {
	ID          string
	Country     string
	City        string
	FlagEmoji   string
	IPAddress   string
	PingMs      int
	LoadPercent int
	Categories  []string
	IsFastest   bool
	IsSelf      bool
	Healthy     bool
}

type SelfLoadFunc func() (loadPercent int)

type Registry struct {
	mu       sync.RWMutex
	nodes    []Node
	selfID   string
	live     map[string]liveStatus
	client   *http.Client
	secret   string
	interval time.Duration
	timeout  time.Duration
	selfLoad SelfLoadFunc
}

// Load reads nodes from a JSON file. A missing file is not an error: the
// registry falls back to a single synthetic self-node built from the
// caller-supplied defaults, so a single-node deployment needs zero fleet
// configuration to work.
func Load(path, selfID string, selfDefaults Node, sharedSecret string, pollInterval time.Duration, pollTimeout time.Duration) (*Registry, error) {
	r := &Registry{
		selfID:   selfID,
		live:     make(map[string]liveStatus),
		client:   &http.Client{Timeout: pollTimeout},
		secret:   sharedSecret,
		interval: pollInterval,
		timeout:  pollTimeout,
		selfLoad: func() int { return 0 },
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("fleet: reading %s: %w", path, err)
		}
		selfDefaults.ID = selfID
		selfDefaults.IsSelf = true
		r.nodes = []Node{selfDefaults}
		return r, nil
	}

	var nodes []Node
	if err := json.Unmarshal(data, &nodes); err != nil {
		return nil, fmt.Errorf("fleet: parsing %s: %w", path, err)
	}
	foundSelf := false
	for i := range nodes {
		if nodes[i].ID == selfID {
			nodes[i].IsSelf = true
			foundSelf = true
		}
	}
	if !foundSelf {
		selfDefaults.ID = selfID
		selfDefaults.IsSelf = true
		nodes = append([]Node{selfDefaults}, nodes...)
	}
	r.nodes = nodes
	return r, nil
}

func (r *Registry) SetSelfLoadFunc(fn SelfLoadFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selfLoad = fn
}

func (r *Registry) Self() (Node, bool) {
	return r.Lookup(r.selfID)
}

func (r *Registry) Lookup(id string) (Node, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, n := range r.nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

func (r *Registry) IsSelf(id string) bool {
	return id == r.selfID
}

// Views returns a display-ready snapshot of every known node, self first,
// using the most recently polled health/load for remote nodes (or their
// static baseline if never successfully polled yet).
func (r *Registry) Views() []NodeView {
	r.mu.RLock()
	defer r.mu.RUnlock()

	views := make([]NodeView, 0, len(r.nodes))
	for _, n := range r.nodes {
		v := NodeView{
			ID:          n.ID,
			Country:     n.Country,
			City:        n.City,
			FlagEmoji:   n.FlagEmoji,
			IPAddress:   n.IPAddress,
			PingMs:      n.BaselinePingMs,
			LoadPercent: n.BaselineLoadPercent,
			Categories:  n.Categories,
			IsFastest:   n.IsFastest,
			IsSelf:      n.IsSelf,
			Healthy:     true,
		}
		if n.IsSelf {
			v.LoadPercent = r.selfLoad()
		} else if status, ok := r.live[n.ID]; ok {
			v.Healthy = status.healthy
			if status.healthy {
				v.LoadPercent = status.loadPercent
			}
		}
		views = append(views, v)
	}
	return views
}

// ActiveCount returns how many known nodes (self included) are currently
// considered healthy, used for BackendHealthDto.activeNodes.
func (r *Registry) ActiveCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, n := range r.nodes {
		if n.IsSelf {
			count++
			continue
		}
		if status, ok := r.live[n.ID]; ok && status.healthy {
			count++
		}
	}
	return count
}

// StartPolling launches a background goroutine that periodically refreshes
// health/load for every non-self node that has an InternalAPIURL, until ctx
// is cancelled. It is a no-op (returns immediately) if there are no such
// nodes, so single-node deployments don't spin up an idle goroutine.
func (r *Registry) StartPolling(ctx context.Context) {
	r.mu.RLock()
	hasRemote := false
	for _, n := range r.nodes {
		if !n.IsSelf && n.InternalAPIURL != "" {
			hasRemote = true
			break
		}
	}
	r.mu.RUnlock()
	if !hasRemote || r.interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		r.pollOnce(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.pollOnce(ctx)
			}
		}
	}()
}

type internalStatusResponse struct {
	Healthy     bool `json:"healthy"`
	LoadPercent int  `json:"loadPercent"`
}

func (r *Registry) pollOnce(ctx context.Context) {
	r.mu.RLock()
	targets := make([]Node, 0, len(r.nodes))
	for _, n := range r.nodes {
		if !n.IsSelf && n.InternalAPIURL != "" {
			targets = append(targets, n)
		}
	}
	r.mu.RUnlock()

	var wg sync.WaitGroup
	results := make(chan struct {
		id     string
		status liveStatus
	}, len(targets))

	for _, node := range targets {
		wg.Add(1)
		go func(n Node) {
			defer wg.Done()
			status := r.fetchStatus(ctx, n)
			results <- struct {
				id     string
				status liveStatus
			}{n.ID, status}
		}(node)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	r.mu.Lock()
	for res := range results {
		r.live[res.id] = res.status
	}
	r.mu.Unlock()
}

func (r *Registry) fetchStatus(ctx context.Context, n Node) liveStatus {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.InternalAPIURL+"/internal/v1/status", nil)
	if err != nil {
		return liveStatus{healthy: false, observedAt: time.Now()}
	}
	if r.secret != "" {
		req.Header.Set("X-Internal-Fleet-Secret", r.secret)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return liveStatus{healthy: false, observedAt: time.Now()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return liveStatus{healthy: false, observedAt: time.Now()}
	}
	var body internalStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return liveStatus{healthy: false, observedAt: time.Now()}
	}
	return liveStatus{healthy: body.Healthy, loadPercent: body.LoadPercent, observedAt: time.Now()}
}
