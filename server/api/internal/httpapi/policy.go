package httpapi

import "sync"

// Policy holds the node-wide security toggles pushed by POST
// /api/v1/settings. The client DTO carries no session/user id, so there is
// no way to scope these per-device; they are applied as this node's
// current default policy, consulted by future /connect calls (e.g.
// splitTunneling shapes the AllowedIps handed to new WireGuard peers) and,
// for threatProtection, pushed into the shared DNS resolver's blocklist
// immediately. See docs/API.md for the full rationale.
type Policy struct {
	mu               sync.RWMutex
	killSwitch       bool
	threatProtection bool
	splitTunneling   bool
	autoConnect      bool
}

func NewPolicy(defaultThreatProtection bool) *Policy {
	return &Policy{
		killSwitch:       true,
		threatProtection: defaultThreatProtection,
		splitTunneling:   false,
		autoConnect:      false,
	}
}

func (p *Policy) Update(req UpdateSettingsRequestDto) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.killSwitch = req.KillSwitch
	p.threatProtection = req.ThreatProtection
	p.splitTunneling = req.SplitTunneling
	p.autoConnect = req.AutoConnect
}

func (p *Policy) Snapshot() UpdateSettingsRequestDto {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return UpdateSettingsRequestDto{
		KillSwitch:       p.killSwitch,
		ThreatProtection: p.threatProtection,
		SplitTunneling:   p.splitTunneling,
		AutoConnect:      p.autoConnect,
	}
}

func (p *Policy) SplitTunneling() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.splitTunneling
}
