package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"vpnapi/internal/openvpn"
	"vpnapi/internal/session"
	"vpnapi/internal/wireguard"
)

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, BackendHealthDto{
		Status:        "OPERATIONAL",
		Version:       Version,
		ActiveNodes:   a.registry.ActiveCount(),
		ClusterRegion: a.cfg.NodeRegion,
	})
}

func (a *App) handleInternalStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, internalStatusDto{
		Healthy:     true,
		LoadPercent: a.computeSelfLoad(),
	})
}

func (a *App) handleServers(w http.ResponseWriter, r *http.Request) {
	views := a.registry.Views()
	out := make([]ServerNodeDto, 0, len(views))
	for _, v := range views {
		if !v.Healthy && !v.IsSelf {
			continue // don't advertise a fleet sibling that's currently down
		}
		out = append(out, ServerNodeDto{
			ID:            v.ID,
			Country:       v.Country,
			City:          v.City,
			FlagEmoji:     v.FlagEmoji,
			IPAddress:     v.IPAddress,
			PingMs:        v.PingMs,
			LoadPercent:   v.LoadPercent,
			CategoryNames: v.Categories,
			IsFastest:     v.IsFastest,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleConnect(w http.ResponseWriter, r *http.Request) {
	started := time.Now()

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "İstek gövdesi okunamadı.")
		return
	}
	var req ConnectRequestDto
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Geçersiz JSON gövdesi.")
		return
	}

	node, known := a.registry.Lookup(req.ServerID)
	if !known {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Bilinmeyen serverId: %q. Önce GET /api/v1/servers çağırın.", req.ServerID))
		return
	}
	if !a.registry.IsSelf(node.ID) {
		a.proxyProtocolCall(w, r, node, http.MethodPost, "/api/v1/connect", raw)
		return
	}

	// max < 0 is an explicit opt-in to "unlimited"; max == 0 (as an operator
	// might set to pause new connections) must actually mean zero capacity,
	// not silently fall back to unlimited.
	if max := a.cfg.RateLimit.MaxConcurrentSessions; max >= 0 && a.sessions.Count() >= max {
		writeError(w, http.StatusServiceUnavailable, "Sunucu kapasitesi dolu, lütfen daha sonra tekrar deneyin.")
		return
	}

	sessionID, err := newSessionID(a.cfg.NodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Oturum kimliği üretilemedi.")
		return
	}

	switch session.Protocol(req.Protocol) {
	case session.ProtocolWireGuard:
		a.connectWireGuard(w, r, req, sessionID, started)
	case session.ProtocolOpenVPNUDP:
		a.connectOpenVPN(w, r, req, sessionID, started, false)
	case session.ProtocolOpenVPNTCP:
		a.connectOpenVPN(w, r, req, sessionID, started, true)
	case session.ProtocolIKEv2:
		writeJSON(w, http.StatusNotImplemented, ConnectResponseDto{
			Success:  false,
			Status:   "UNSUPPORTED",
			ServerID: req.ServerID,
			Message:  "IKEv2/IPsec bu sunucu sürümünde henüz desteklenmiyor. Lütfen WireGuard veya OpenVPN protokollerini kullanın.",
		})
	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Bilinmeyen protokol: %q", req.Protocol))
	}
}

func (a *App) connectWireGuard(w http.ResponseWriter, r *http.Request, req ConnectRequestDto, sessionID string, started time.Time) {
	if a.wg == nil || !a.cfg.WireGuard.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, ConnectResponseDto{
			Success: false, Status: "UNAVAILABLE", ServerID: req.ServerID,
			Message: "Bu düğümde WireGuard etkin değil.",
		})
		return
	}
	if !a.wg.Available() {
		writeJSON(w, http.StatusServiceUnavailable, ConnectResponseDto{
			Success: false, Status: "UNAVAILABLE", ServerID: req.ServerID,
			Message: "WireGuard sunucu bileşenleri kurulu değil: " + a.wg.UnavailableReason(),
		})
		return
	}

	ephemeral := req.ClientPublicKey == ""
	clientPub := req.ClientPublicKey
	var clientPriv string
	if ephemeral {
		priv, pub, err := wireguard.GenerateKeypair()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "İstemci anahtarı üretilemedi.")
			return
		}
		clientPriv, clientPub = priv, pub
	} else if err := wireguard.ValidatePublicKey(clientPub); err != nil {
		writeError(w, http.StatusBadRequest, "Geçersiz clientPublicKey: "+err.Error())
		return
	}

	virtualIP, err := a.wgPool.Allocate(sessionID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, ConnectResponseDto{
			Success: false, Status: "UNAVAILABLE", ServerID: req.ServerID,
			Message: "Sanal IP havuzu dolu.",
		})
		return
	}

	if err := a.wg.AddPeer(r.Context(), clientPub, virtualIP.String()+"/32"); err != nil {
		a.wgPool.Release(virtualIP)
		a.logger.Error("wireguard AddPeer failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ConnectResponseDto{
			Success: false, Status: "ERROR", ServerID: req.ServerID,
			Message: "Tünel sunucu tarafında kurulamadı.",
		})
		return
	}

	sess := &session.Session{
		ID: sessionID, NodeID: a.cfg.NodeID, Protocol: session.ProtocolWireGuard,
		VirtualIP: virtualIP.String(), PeerKey: clientPub, Ephemeral: ephemeral,
		CreatedAt: time.Now(), LastSeenAt: time.Now(),
	}
	a.sessions.Put(sess)
	if err := a.sessions.Persist(); err != nil {
		a.logger.Warn("persisting session state failed", "error", err)
	}

	allowedIps := "0.0.0.0/0, ::/0"
	if a.policy.SplitTunneling() {
		allowedIps = a.cfg.WireGuard.SubnetCIDR
	}

	writeJSON(w, http.StatusOK, ConnectResponseDto{
		Success:                    true,
		SessionID:                  sessionID,
		Status:                     "ESTABLISHED",
		VirtualIP:                  virtualIP.String(),
		ServerID:                   req.ServerID,
		AssignedPort:               a.cfg.WireGuard.ListenPort,
		HandshakeDurationMs:        time.Since(started).Milliseconds(),
		Message:                    "WireGuard tüneli sunucu tarafında kuruldu.",
		Protocol:                   string(session.ProtocolWireGuard),
		ServerPublicKey:            a.wgPub,
		ClientPrivateKey:           clientPriv,
		Endpoint:                   fmt.Sprintf("%s:%d", a.cfg.PublicEndpointHost, a.cfg.WireGuard.ListenPort),
		AllowedIps:                 allowedIps,
		DNS:                        a.cfg.WireGuard.DNS,
		MTU:                        a.cfg.WireGuard.MTU,
		PersistentKeepaliveSeconds: a.cfg.WireGuard.PersistentKeepalive,
	})
}

func (a *App) connectOpenVPN(w http.ResponseWriter, r *http.Request, req ConnectRequestDto, sessionID string, started time.Time, useTCP bool) {
	proto := session.ProtocolOpenVPNUDP
	port := a.cfg.OpenVPN.UDPPort
	if useTCP {
		proto = session.ProtocolOpenVPNTCP
		port = a.cfg.OpenVPN.TCPPort
	}

	if a.ovpn == nil || !a.cfg.OpenVPN.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, ConnectResponseDto{
			Success: false, Status: "UNAVAILABLE", ServerID: req.ServerID,
			Message: "Bu düğümde OpenVPN etkin değil.",
		})
		return
	}
	if !a.ovpn.Ready() {
		writeJSON(w, http.StatusServiceUnavailable, ConnectResponseDto{
			Success: false, Status: "UNAVAILABLE", ServerID: req.ServerID,
			Message: "OpenVPN PKI henüz kurulmadı.",
		})
		return
	}

	virtualIP, err := a.ovpnPool.Allocate(sessionID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, ConnectResponseDto{
			Success: false, Status: "UNAVAILABLE", ServerID: req.ServerID,
			Message: "Sanal IP havuzu dolu.",
		})
		return
	}

	result, err := a.ovpn.Provision(sessionID, virtualIP.String(), netmaskOf(a.cfg.OpenVPN.SubnetCIDR), useTCP)
	if err != nil {
		a.ovpnPool.Release(virtualIP)
		a.logger.Error("openvpn Provision failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ConnectResponseDto{
			Success: false, Status: "ERROR", ServerID: req.ServerID,
			Message: "OpenVPN kimlik bilgisi oluşturulamadı.",
		})
		return
	}

	sess := &session.Session{
		ID: sessionID, NodeID: a.cfg.NodeID, Protocol: proto,
		VirtualIP: virtualIP.String(), PeerKey: sessionID, Ephemeral: false,
		CreatedAt: time.Now(), LastSeenAt: time.Now(),
	}
	a.sessions.Put(sess)
	if err := a.sessions.Persist(); err != nil {
		a.logger.Warn("persisting session state failed", "error", err)
	}

	writeJSON(w, http.StatusOK, ConnectResponseDto{
		Success:             true,
		SessionID:           sessionID,
		Status:              "PROVISIONED",
		VirtualIP:           virtualIP.String(),
		ServerID:            req.ServerID,
		AssignedPort:        port,
		HandshakeDurationMs: time.Since(started).Milliseconds(),
		Message:             "OpenVPN kimlik bilgisi oluşturuldu; istemci bağlantıyı başlattığında tünel etkinleşecek.",
		Protocol:            string(proto),
		Endpoint:            fmt.Sprintf("%s:%d", a.cfg.PublicEndpointHost, port),
		OvpnProfile:         result.Profile,
	})
}

func (a *App) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "İstek gövdesi okunamadı.")
		return
	}
	var req DisconnectRequestDto
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			writeError(w, http.StatusBadRequest, "Geçersiz JSON gövdesi.")
			return
		}
	}

	if req.SessionID == "" {
		writeJSON(w, http.StatusOK, DisconnectResponseDto{Success: true, Message: "Aktif oturum belirtilmedi."})
		return
	}

	if node, ok := a.findOwningNode(req.SessionID); ok && !a.registry.IsSelf(node.ID) {
		a.proxyProtocolCall(w, r, node, http.MethodPost, "/api/v1/disconnect", raw)
		return
	}

	sess, ok := a.sessions.Delete(req.SessionID)
	if !ok {
		writeJSON(w, http.StatusOK, DisconnectResponseDto{Success: true, Message: "Oturum zaten sonlanmış."})
		return
	}
	a.releaseSession(r.Context(), sess)
	if err := a.sessions.Persist(); err != nil {
		a.logger.Warn("persisting session state failed", "error", err)
	}
	writeJSON(w, http.StatusOK, DisconnectResponseDto{Success: true, Message: "Tünel sunucu tarafında sonlandırıldı."})
}

func (a *App) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "sessionId parametresi zorunludur.")
		return
	}

	if node, ok := a.findOwningNode(sessionID); ok && !a.registry.IsSelf(node.ID) {
		path := "/api/v1/telemetry?sessionId=" + url.QueryEscape(sessionID)
		a.proxyProtocolCall(w, r, node, http.MethodGet, path, nil)
		return
	}

	sess, ok := a.sessions.Get(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "Bilinmeyen veya süresi dolmuş oturum.")
		return
	}
	a.sessions.Touch(sessionID)

	var rx, tx uint64
	health := "OPTIMAL"
	switch sess.Protocol {
	case session.ProtocolWireGuard:
		if a.wg != nil {
			if peer, err := a.wg.FindPeer(r.Context(), sess.PeerKey); err == nil && peer != nil {
				rx, tx = peer.ReceiveBytes, peer.TransmitBytes
				if !peer.LatestHandshake.IsZero() && time.Since(peer.LatestHandshake) > 3*time.Minute {
					health = "DEGRADED"
				}
			} else {
				health = "CONNECTING"
			}
		}
	case session.ProtocolOpenVPNUDP, session.ProtocolOpenVPNTCP:
		if a.ovpn != nil {
			var stat *openvpn.ClientStat
			stat, _ = a.ovpn.Stats(sess.PeerKey, sess.Protocol == session.ProtocolOpenVPNTCP)
			if stat != nil {
				rx, tx = stat.BytesReceived, stat.BytesSent
			} else {
				health = "CONNECTING"
			}
		}
	}

	down, up, history := sess.Sample(rx, tx)
	writeJSON(w, http.StatusOK, ServerTelemetryDto{
		ServerID:               a.cfg.NodeID,
		Status:                 "CONNECTED",
		VirtualIP:              sess.VirtualIP,
		DownloadSpeedMbps:      down,
		UploadSpeedMbps:        up,
		TotalDownloadedBytes:   int64(rx),
		TotalUploadedBytes:     int64(tx),
		SessionDurationSeconds: int64(time.Since(sess.CreatedAt).Seconds()),
		ServerHealth:           health,
		TrafficSamples:         history,
	})
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "İstek gövdesi okunamadı.")
		return
	}
	var req UpdateSettingsRequestDto
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Geçersiz JSON gövdesi.")
		return
	}

	a.policy.Update(req)
	if err := applyDNSBlocklistPolicy(a.cfg, req.ThreatProtection); err != nil {
		a.logger.Warn("applying DNS blocklist policy failed", "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]bool{
		"killSwitch":       req.KillSwitch,
		"threatProtection": req.ThreatProtection,
		"splitTunneling":   req.SplitTunneling,
		"autoConnect":      req.AutoConnect,
	})
}
