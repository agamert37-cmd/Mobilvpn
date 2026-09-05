package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"vpnapi/internal/config"
	"vpnapi/internal/fleet"
	"vpnapi/internal/ipam"
	"vpnapi/internal/openvpn"
	"vpnapi/internal/ratelimit"
	"vpnapi/internal/session"
	"vpnapi/internal/wireguard"
)

const Version = "1.0.0"
const maxBodyBytes = 64 * 1024

// App wires every backend (WireGuard, OpenVPN, the fleet registry, rate
// limiter, session store) into the HTTP surface described in docs/API.md.
type App struct {
	cfg      config.Config
	logger   *slog.Logger
	sessions *session.Store
	policy   *Policy

	wg     wireGuardBackend
	wgPub  string
	wgPool *ipam.Pool

	ovpn     openVPNBackend
	ovpnPool *ipam.Pool

	registry       *fleet.Registry
	proxy          *fleet.ProxyClient
	connectLimiter *ratelimit.Limiter
	mutateLimiter  *ratelimit.Limiter

	startedAt time.Time
}

func NewApp(cfg config.Config, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}
	a := &App{
		cfg:            cfg,
		logger:         logger,
		sessions:       session.NewStore(cfg.StateFile),
		policy:         NewPolicy(cfg.DNS.BlocklistEnabledDefault),
		connectLimiter: ratelimit.New(cfg.RateLimit.RequestsPerMinutePerIP, cfg.RateLimit.Burst),
		mutateLimiter:  ratelimit.New(cfg.RateLimit.RequestsPerMinutePerIP*2, cfg.RateLimit.Burst*2),
		startedAt:      time.Now(),
	}

	if cfg.WireGuard.Enabled {
		// The private half never leaves EnsureServerIdentity's return value:
		// it's already durably persisted on disk (see identity.go) and the
		// running wg0 kernel interface is configured from that same file by
		// scripts/20-wireguard-setup.sh — vpn-api only ever needs the public
		// key, to hand to clients.
		_, pub, err := wireguard.EnsureServerIdentity(cfg.WireGuard.KeyDir)
		if err != nil {
			return nil, fmt.Errorf("httpapi: bootstrapping WireGuard identity: %w", err)
		}
		a.wgPub = pub
		a.wg = wireguard.NewManager(cfg.WireGuard.Interface)
		if !a.wg.Available() {
			logger.Warn("WireGuard tools unavailable; /connect for WIREGUARD will report UNAVAILABLE", "reason", a.wg.UnavailableReason())
		}
		pool, err := ipam.NewPool("wireguard", cfg.WireGuard.SubnetCIDR, cfg.WireGuard.ServerVirtualIP)
		if err != nil {
			return nil, fmt.Errorf("httpapi: WireGuard IP pool: %w", err)
		}
		a.wgPool = pool
	}

	if cfg.OpenVPN.Enabled {
		mgmtPassword := readManagementPassword(cfg.OpenVPN.ManagementPasswordFile, logger)
		a.ovpn = openvpn.NewManager(openvpn.Config{
			EasyRSADir:         cfg.OpenVPN.EasyRSADir,
			ServerDir:          cfg.OpenVPN.ServerDir,
			CCDDir:             cfg.OpenVPN.CCDDir,
			ManagementUDPAddr:  cfg.OpenVPN.ManagementUDPAddr,
			ManagementTCPAddr:  cfg.OpenVPN.ManagementTCPAddr,
			ManagementPassword: mgmtPassword,
			UDPPort:            cfg.OpenVPN.UDPPort,
			TCPPort:            cfg.OpenVPN.TCPPort,
			PublicHost:         cfg.PublicEndpointHost,
		})
		if !a.ovpn.Ready() {
			logger.Warn("OpenVPN PKI not initialized; /connect for OPENVPN_* will report UNAVAILABLE", "easyRsaDir", cfg.OpenVPN.EasyRSADir)
		}
		pool, err := ipam.NewPool("openvpn", cfg.OpenVPN.SubnetCIDR, subnetGateway(cfg.OpenVPN.SubnetCIDR))
		if err != nil {
			return nil, fmt.Errorf("httpapi: OpenVPN IP pool: %w", err)
		}
		a.ovpnPool = pool
	}

	selfNode := fleet.Node{
		Country:   cfg.NodeRegion,
		City:      cfg.NodeRegion,
		IPAddress: cfg.PublicEndpointHost,
		IsFastest: true,
	}
	registry, err := fleet.Load(cfg.Fleet.NodesFile, cfg.NodeID, selfNode, cfg.Fleet.SharedSecret,
		time.Duration(cfg.Fleet.HealthPollIntervalSec)*time.Second,
		time.Duration(cfg.Fleet.HealthPollTimeoutMs)*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("httpapi: loading fleet registry: %w", err)
	}
	registry.SetSelfLoadFunc(a.computeSelfLoad)
	a.registry = registry
	a.proxy = fleet.NewProxyClient(time.Duration(cfg.Fleet.HealthPollTimeoutMs)*3*time.Millisecond, cfg.Fleet.SharedSecret)

	restored, err := a.sessions.Load()
	if err != nil {
		logger.Warn("could not load persisted session state; starting with an empty session table", "error", err)
	}
	for _, sess := range restored {
		a.reserveRestoredSession(sess)
	}
	if len(restored) > 0 {
		logger.Info("restored sessions from state file", "count", len(restored))
	}

	return a, nil
}

// reserveRestoredSession re-marks a virtual IP as taken after a process
// restart. It does not re-add kernel WireGuard peers or OpenVPN CCD
// entries: those survive an API-process restart untouched (they live in
// the kernel / on disk independently), so all that's needed here is to
// stop ipam from handing the same address to someone else.
func (a *App) reserveRestoredSession(sess *session.Session) {
	ip := net.ParseIP(sess.VirtualIP)
	if ip == nil {
		return
	}
	switch sess.Protocol {
	case session.ProtocolWireGuard:
		if a.wgPool != nil {
			_ = a.wgPool.Reserve(ip, sess.ID)
		}
	case session.ProtocolOpenVPNUDP, session.ProtocolOpenVPNTCP:
		if a.ovpnPool != nil {
			_ = a.ovpnPool.Reserve(ip, sess.ID)
		}
	}
}

// Routes builds the full HTTP handler: public /api/v1/* endpoints for the
// Android client, plus an /internal/v1/* endpoint used only by sibling
// fleet nodes' health polling.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", a.handleHealth)
	mux.HandleFunc("GET /api/v1/servers", a.handleServers)
	mux.HandleFunc("POST /api/v1/connect", a.rateLimited(a.connectLimiter, a.handleConnect))
	mux.HandleFunc("POST /api/v1/disconnect", a.rateLimited(a.mutateLimiter, a.handleDisconnect))
	mux.HandleFunc("GET /api/v1/telemetry", a.handleTelemetry)
	mux.HandleFunc("POST /api/v1/settings", a.rateLimited(a.mutateLimiter, a.handleSettings))

	mux.HandleFunc("GET /internal/v1/status", a.requireInternalSecret(a.handleInternalStatus))

	return a.recoverMiddleware(a.loggingMiddleware(mux))
}

// StartBackgroundTasks launches the fleet health poller and the idle
// session reaper. Both stop when ctx is cancelled.
func (a *App) StartBackgroundTasks(ctx context.Context) {
	a.registry.StartPolling(ctx)

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.reapIdleSessions(ctx)
			}
		}
	}()
}

func (a *App) reapIdleSessions(ctx context.Context) {
	expired := a.sessions.ExpireIdle(a.cfg.IdleTimeout())
	if len(expired) == 0 {
		return
	}
	for _, sess := range expired {
		a.releaseSession(ctx, sess)
	}
	a.logger.Info("reaped idle sessions", "count", len(expired))
	if err := a.sessions.Persist(); err != nil {
		a.logger.Warn("persisting session state after reap failed", "error", err)
	}
}

// Shutdown persists live session state so a planned restart doesn't orphan
// active tunnels' bookkeeping.
func (a *App) Shutdown() {
	if err := a.sessions.Persist(); err != nil {
		a.logger.Warn("persisting session state on shutdown failed", "error", err)
	}
}

func (a *App) computeSelfLoad() int {
	total, used := 0, 0
	if a.wgPool != nil {
		total += a.wgPool.Capacity()
		used += a.wgPool.InUse()
	}
	if a.ovpnPool != nil {
		total += a.ovpnPool.Capacity()
		used += a.ovpnPool.InUse()
	}
	if total <= 0 {
		return 0
	}
	pct := used * 100 / total
	if pct > 100 {
		pct = 100
	}
	return pct
}

func (a *App) releaseSession(ctx context.Context, sess *session.Session) {
	switch sess.Protocol {
	case session.ProtocolWireGuard:
		if a.wg != nil {
			if err := a.wg.RemovePeer(ctx, sess.PeerKey); err != nil {
				a.logger.Warn("removing wireguard peer failed", "error", err)
			}
		}
		if a.wgPool != nil {
			if ip := net.ParseIP(sess.VirtualIP); ip != nil {
				a.wgPool.Release(ip)
			}
		}
	case session.ProtocolOpenVPNUDP, session.ProtocolOpenVPNTCP:
		if a.ovpn != nil {
			if err := a.ovpn.Deprovision(sess.PeerKey); err != nil {
				a.logger.Warn("revoking openvpn credential failed", "error", err)
			}
		}
		if a.ovpnPool != nil {
			if ip := net.ParseIP(sess.VirtualIP); ip != nil {
				a.ovpnPool.Release(ip)
			}
		}
	}
}

// findOwningNode determines, without any shared cross-node state, which
// fleet node a session ID belongs to: session IDs are minted as
// "sess_<nodeId>_<random>" (see ids.go), so this just checks the prefix
// against every node this instance currently knows about.
func (a *App) findOwningNode(sessionID string) (fleet.Node, bool) {
	for _, v := range a.registry.Views() {
		if strings.HasPrefix(sessionID, "sess_"+sanitizeNodeID(v.ID)+"_") {
			return a.registry.Lookup(v.ID)
		}
	}
	return fleet.Node{}, false
}

func (a *App) proxyProtocolCall(w http.ResponseWriter, r *http.Request, node fleet.Node, method, path string, body []byte) {
	resp, err := a.proxy.Forward(r.Context(), node, method, path, body)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Uzak sunucu düğümüne (%s) ulaşılamadı: %v", node.ID, err))
		return
	}
	if resp.ContentType != "" {
		w.Header().Set("Content-Type", resp.ContentType)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(resp.Body)
}

func (a *App) rateLimited(limiter *ratelimit.Limiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := clientIP(r, a.cfg.TrustProxyHeaders)
		if !limiter.Allow(key) {
			writeError(w, http.StatusTooManyRequests, "Çok fazla istek. Lütfen birkaç saniye sonra tekrar deneyin.")
			return
		}
		next(w, r)
	}
}

func (a *App) requireInternalSecret(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.cfg.Fleet.SharedSecret == "" {
			writeError(w, http.StatusServiceUnavailable, "internal fleet endpoint disabled (no shared secret configured)")
			return
		}
		got := r.Header.Get("X-Internal-Fleet-Secret")
		if subtle.ConstantTimeCompare([]byte(got), []byte(a.cfg.Fleet.SharedSecret)) != 1 {
			writeError(w, http.StatusForbidden, "invalid internal fleet secret")
			return
		}
		next(w, r)
	}
}

func clientIP(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
		if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
			return xrip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Success: false, Message: message})
}

// subnetGateway picks the first usable host address of cidr as a default
// gateway when the caller doesn't configure one explicitly (OpenVPN's own
// TUN gateway is managed by the openvpn server process itself via its
// `server` directive, so this pool's "gateway" reservation is really just
// "the address openvpn.conf's `server` line implies it will keep for
// itself").
func subnetGateway(cidr string) string {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return ""
	}
	ip := ipnet.IP.To4()
	if ip == nil {
		return ""
	}
	gw := make(net.IP, 4)
	copy(gw, ip)
	gw[3]++
	return gw.String()
}

// readManagementPassword loads the shared secret scripts/30-openvpn-setup.sh
// writes for the OpenVPN management sockets. A missing file just means an
// operator set one up without it (or hasn't run that script yet); OpenVPN's
// own management interface falls back to no-auth in that case too, so we
// log once and match that behavior rather than failing to start.
func readManagementPassword(path string, logger *slog.Logger) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		logger.Warn("OpenVPN management password file unreadable; connecting without authentication", "path", path, "error", err)
		return ""
	}
	return strings.TrimSpace(string(data))
}

func ipStringOrEmpty(ip net.IP) string {
	if ip == nil {
		return ""
	}
	return ip.String()
}

// netmaskOf renders a CIDR's mask in dotted-decimal form, as OpenVPN's
// client-config-dir `ifconfig-push <ip> <netmask>` directive expects.
func netmaskOf(cidr string) string {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil || len(ipnet.Mask) != 4 {
		return "255.255.255.0"
	}
	return net.IP(ipnet.Mask).String()
}
