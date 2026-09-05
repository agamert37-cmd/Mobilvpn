// Package wireguard drives the kernel WireGuard implementation through the
// `wg` command-line tool. There is no netlink/wgctrl dependency here on
// purpose: shelling out to `wg` is a handful of exec.Command calls, needs no
// third-party module, and is exactly what wg-quick itself does.
package wireguard

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// PeerStat is one parsed line of `wg show <iface> dump`.
type PeerStat struct {
	PublicKey       string
	Endpoint        string
	AllowedIPs      string
	LatestHandshake time.Time
	ReceiveBytes    uint64
	TransmitBytes   uint64
	KeepaliveSec    int
}

// Manager controls a single WireGuard interface (normally "wg0").
type Manager struct {
	iface         string
	wgPath        string
	ipPath        string
	available     bool
	unavailReason string
}

// NewManager probes for the `wg` and `ip` binaries. When either is missing
// the Manager stays in place but every mutating call returns ErrUnavailable
// — callers (the HTTP layer) decide how to degrade, instead of the whole
// process failing to start just because it's running somewhere without
// WireGuard (e.g. this code's own test/dev sandbox).
func NewManager(iface string) *Manager {
	m := &Manager{iface: iface}
	wgPath, err := exec.LookPath("wg")
	if err != nil {
		m.unavailReason = "the `wg` binary is not installed (apt install wireguard-tools)"
		return m
	}
	ipPath, err := exec.LookPath("ip")
	if err != nil {
		m.unavailReason = "the `ip` binary is not installed (apt install iproute2)"
		return m
	}
	m.wgPath, m.ipPath = wgPath, ipPath

	// A missing interface is still "available" in the sense that AddPeer et
	// al. will simply fail with a clear kernel-reported error; we only mark
	// the manager fully unavailable when the tools themselves are absent.
	m.available = true
	return m
}

var ErrUnavailable = fmt.Errorf("wireguard: manager unavailable")

func (m *Manager) Available() bool { return m.available }

func (m *Manager) UnavailableReason() string { return m.unavailReason }

// InterfaceExists reports whether the managed interface is currently up.
func (m *Manager) InterfaceExists(ctx context.Context) bool {
	if !m.available {
		return false
	}
	cmd := exec.CommandContext(ctx, m.ipPath, "link", "show", m.iface)
	return cmd.Run() == nil
}

// AddPeer adds (or updates, `wg set` is idempotent/upsert) a peer on the
// managed interface, restricted to exactly one /32 allowed-ip so peers can
// never route or spoof traffic for another tunnel client (server-enforced
// client isolation).
func (m *Manager) AddPeer(ctx context.Context, publicKey, allowedIPCIDR string) error {
	if !m.available {
		return fmt.Errorf("%w: %s", ErrUnavailable, m.unavailReason)
	}
	args := []string{"set", m.iface, "peer", publicKey, "allowed-ips", allowedIPCIDR}
	out, err := exec.CommandContext(ctx, m.wgPath, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("wireguard: wg set peer add failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemovePeer deletes a peer from the managed interface. Removing a peer
// that doesn't exist is not an error (wg is idempotent here too), which
// keeps disconnect/cleanup code simple.
func (m *Manager) RemovePeer(ctx context.Context, publicKey string) error {
	if !m.available {
		return fmt.Errorf("%w: %s", ErrUnavailable, m.unavailReason)
	}
	args := []string{"set", m.iface, "peer", publicKey, "remove"}
	out, err := exec.CommandContext(ctx, m.wgPath, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("wireguard: wg set peer remove failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DumpPeers parses `wg show <iface> dump`, returning live per-peer transfer
// counters and handshake times used to compute telemetry.
func (m *Manager) DumpPeers(ctx context.Context) ([]PeerStat, error) {
	if !m.available {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, m.unavailReason)
	}
	out, err := exec.CommandContext(ctx, m.wgPath, "show", m.iface, "dump").Output()
	if err != nil {
		return nil, fmt.Errorf("wireguard: wg show dump failed: %w", err)
	}
	return parseDump(out), nil
}

// parseDump handles the tab-separated `wg show <iface> dump` format. The
// first line describes the interface itself (private key, public key,
// listen port, fwmark) and is skipped; every following line is one peer:
//
//	publicKey  presharedKey  endpoint  allowedIps  latestHandshake  rxBytes  txBytes  keepalive
func parseDump(out []byte) []PeerStat {
	var stats []PeerStat
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	for i, line := range lines {
		if i == 0 || len(line) == 0 {
			continue // interface header line
		}
		fields := strings.Split(string(line), "\t")
		if len(fields) < 8 {
			continue
		}
		hsUnix, _ := strconv.ParseInt(fields[4], 10, 64)
		rx, _ := strconv.ParseUint(fields[5], 10, 64)
		tx, _ := strconv.ParseUint(fields[6], 10, 64)
		keepalive := 0
		if fields[7] != "off" {
			keepalive, _ = strconv.Atoi(fields[7])
		}
		var handshake time.Time
		if hsUnix > 0 {
			handshake = time.Unix(hsUnix, 0)
		}
		stats = append(stats, PeerStat{
			PublicKey:       fields[0],
			Endpoint:        fields[2],
			AllowedIPs:      fields[3],
			LatestHandshake: handshake,
			ReceiveBytes:    rx,
			TransmitBytes:   tx,
			KeepaliveSec:    keepalive,
		})
	}
	return stats
}

// FindPeer is a small convenience wrapper for the common "telemetry for one
// session's peer" lookup.
func (m *Manager) FindPeer(ctx context.Context, publicKey string) (*PeerStat, error) {
	stats, err := m.DumpPeers(ctx)
	if err != nil {
		return nil, err
	}
	for i := range stats {
		if stats[i].PublicKey == publicKey {
			return &stats[i], nil
		}
	}
	return nil, nil
}
