// Package session tracks live tunnel sessions in memory.
//
// Privacy note: this store is intentionally ephemeral. It persists only the
// minimum needed to survive an API process restart without orphaning live
// kernel WireGuard peers or OpenVPN certificates (public key / virtual IP /
// protocol / timestamps) — never traffic contents, destinations, or any
// browsing metadata. See docs/SECURITY.md for the full no-logs policy.
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Protocol string

const (
	ProtocolWireGuard  Protocol = "WIREGUARD"
	ProtocolOpenVPNUDP Protocol = "OPENVPN_UDP"
	ProtocolOpenVPNTCP Protocol = "OPENVPN_TCP"
	ProtocolIKEv2      Protocol = "IKEV2"
)

// Session represents one provisioned tunnel lease.
type Session struct {
	ID         string    `json:"id"`
	NodeID     string    `json:"nodeId"`
	Protocol   Protocol  `json:"protocol"`
	VirtualIP  string    `json:"virtualIp"`
	PeerKey    string    `json:"peerKey"`   // WireGuard peer public key, or OpenVPN common name
	Ephemeral  bool      `json:"ephemeral"` // true if the server generated the client's private key
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`

	// Telemetry sampling baseline, used to turn cumulative wg/openvpn byte
	// counters into an instantaneous Mbps figure between polls. Never
	// persisted to disk and never exposed directly.
	sampleMu      sync.Mutex
	lastSampleAt  time.Time
	lastDownBytes uint64
	lastUpBytes   uint64
	history       []float32
}

// SampleResult is one telemetry reading, shaped for what the Android client
// actually does with each field.
type SampleResult struct {
	DownMbps float32
	UpMbps   float32

	// DownDelta/UpDelta are the bytes moved SINCE THE PREVIOUS SAMPLE, not
	// the running totals — even though the wire field is called
	// "totalDownloadedBytes".
	//
	// That name is the client's, and the client accumulates it itself:
	//
	//   totalDownloadedBytes = state.totalDownloadedBytes + telemetry.totalDownloadedBytes
	//
	// (viewmodel/VpnViewModel.kt, startLiveServerTelemetry). It polls once a
	// second, so sending the cumulative tunnel counter makes the number the
	// user sees grow quadratically — a session sitting at 100 MB would show
	// ~3 GB after a minute and ~180 GB after an hour.
	DownDelta uint64
	UpDelta   uint64

	// History mirrors the client's own 16-slot trafficHistory so a fresh
	// client immediately has a full sparkline instead of ramping up from
	// zero. Never nil: the client's trafficSamples is a non-null
	// List<Float>, and Moshi throws on an explicit null.
	History []float32
}

// Sample records a new cumulative byte reading for the session and returns
// the throughput, the per-interval deltas, and the rolling history.
//
// Both counters are from the CLIENT's point of view — callers are
// responsible for flipping the server-side rx/tx their tunnel backend
// reports (see the telemetry handler).
func (s *Session) Sample(downBytes, upBytes uint64) SampleResult {
	s.sampleMu.Lock()
	defer s.sampleMu.Unlock()

	out := SampleResult{}
	now := time.Now()
	if !s.lastSampleAt.IsZero() {
		// A counter that went backwards means the tunnel restarted and the
		// backend's counter reset. Report zero rather than a nonsense
		// wrapped-around delta, and let the next sample resume normally.
		if downBytes >= s.lastDownBytes {
			out.DownDelta = downBytes - s.lastDownBytes
		}
		if upBytes >= s.lastUpBytes {
			out.UpDelta = upBytes - s.lastUpBytes
		}
		if elapsed := now.Sub(s.lastSampleAt).Seconds(); elapsed > 0 {
			out.DownMbps = float32(float64(out.DownDelta) * 8 / elapsed / 1_000_000)
			out.UpMbps = float32(float64(out.UpDelta) * 8 / elapsed / 1_000_000)
		}
	}
	s.lastSampleAt = now
	s.lastDownBytes = downBytes
	s.lastUpBytes = upBytes

	s.history = append(s.history, out.DownMbps)
	if len(s.history) > 16 {
		s.history = s.history[len(s.history)-16:]
	}
	out.History = make([]float32, len(s.history))
	copy(out.History, s.history)
	return out
}

// persistedSession is the on-disk shape: no mutexes, no unexported sampling
// state (that is runtime-only and rebuilt from live tunnel counters after a
// restart).
type persistedSession struct {
	ID         string    `json:"id"`
	NodeID     string    `json:"nodeId"`
	Protocol   Protocol  `json:"protocol"`
	VirtualIP  string    `json:"virtualIp"`
	PeerKey    string    `json:"peerKey"`
	Ephemeral  bool      `json:"ephemeral"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

type Store struct {
	mu        sync.RWMutex
	sessions  map[string]*Session
	statePath string
}

func NewStore(statePath string) *Store {
	return &Store{
		sessions:  make(map[string]*Session),
		statePath: statePath,
	}
}

func (s *Store) Put(sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
}

func (s *Store) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

func (s *Store) Delete(id string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
	}
	return sess, ok
}

func (s *Store) Touch(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.LastSeenAt = time.Now()
	}
}

func (s *Store) All() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out
}

func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// CountByProtocol is used for capacity checks scoped to one tunnel backend
// (e.g. don't let OpenVPN churn starve the WireGuard pool's headroom).
func (s *Store) CountByProtocol(p Protocol) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, sess := range s.sessions {
		if sess.Protocol == p {
			n++
		}
	}
	return n
}

// ExpireIdle removes and returns sessions whose LastSeenAt is older than
// ttl, so the caller can release their tunnel resources (wg peer, ipam
// lease, openvpn cert). This is the server-side complement to a client that
// crashed or lost connectivity without ever calling /disconnect.
func (s *Store) ExpireIdle(ttl time.Duration) []*Session {
	cutoff := time.Now().Add(-ttl)
	s.mu.Lock()
	defer s.mu.Unlock()
	var expired []*Session
	for id, sess := range s.sessions {
		if sess.LastSeenAt.Before(cutoff) {
			expired = append(expired, sess)
			delete(s.sessions, id)
		}
	}
	return expired
}

// Persist writes the current session set to disk atomically (write to a
// temp file, then rename) so a crash mid-write can never corrupt the state
// file. The file contains only public keys / CNs and virtual IPs — no
// traffic data — and should be root-only (0600), which the caller is
// responsible for since the file is created with that mode here.
func (s *Store) Persist() error {
	if s.statePath == "" {
		return nil
	}
	s.mu.RLock()
	out := make([]persistedSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, persistedSession{
			ID:         sess.ID,
			NodeID:     sess.NodeID,
			Protocol:   sess.Protocol,
			VirtualIP:  sess.VirtualIP,
			PeerKey:    sess.PeerKey,
			Ephemeral:  sess.Ephemeral,
			CreatedAt:  sess.CreatedAt,
			LastSeenAt: sess.LastSeenAt,
		})
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.statePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp := s.statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.statePath)
}

// Load restores sessions from disk (best-effort: a missing or unreadable
// file just means "start empty", which is always safe).
func (s *Store) Load() ([]*Session, error) {
	if s.statePath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var in []persistedSession
	if err := json.Unmarshal(data, &in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	restored := make([]*Session, 0, len(in))
	for _, p := range in {
		sess := &Session{
			ID:         p.ID,
			NodeID:     p.NodeID,
			Protocol:   p.Protocol,
			VirtualIP:  p.VirtualIP,
			PeerKey:    p.PeerKey,
			Ephemeral:  p.Ephemeral,
			CreatedAt:  p.CreatedAt,
			LastSeenAt: p.LastSeenAt,
		}
		s.sessions[sess.ID] = sess
		restored = append(restored, sess)
	}
	return restored, nil
}
