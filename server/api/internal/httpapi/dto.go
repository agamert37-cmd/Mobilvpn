// Package httpapi implements the REST contract the Android client's
// VpnBackendApi (Retrofit) interface expects, exactly matching its Moshi
// JSON field names, plus a set of purely additive fields (new, optional
// keys) that carry real WireGuard/OpenVPN provisioning material. Moshi's
// reflection-based Kotlin adapter silently ignores unknown JSON keys, so
// these additions are safe for the current app build and simply available
// to any future or alternate client that wants a fully working tunnel
// instead of the thin-client demo's local simulation fallback.
package httpapi

// ServerNodeDto mirrors com.example.network.ServerNodeDto.
type ServerNodeDto struct {
	ID            string   `json:"id"`
	Country       string   `json:"country"`
	City          string   `json:"city"`
	FlagEmoji     string   `json:"flagEmoji"`
	IPAddress     string   `json:"ipAddress"`
	PingMs        int      `json:"pingMs"`
	LoadPercent   int      `json:"loadPercent"`
	CategoryNames []string `json:"categoryNames"`
	IsFastest     bool     `json:"isFastest"`
}

// ConnectRequestDto mirrors com.example.network.ConnectRequestDto.
// ClientPublicKey is additive: today's app never sends it (its DTO has no
// such field), so WireGuard connects fall back to server-generated
// ("ephemeral") client keys. A client that adds the field gets the
// strictly-better bring-your-own-key path where the private key never
// leaves the device — see docs/SECURITY.md.
type ConnectRequestDto struct {
	ServerID        string `json:"serverId"`
	Protocol        string `json:"protocol"`
	ClientTimestamp int64  `json:"clientTimestamp"`
	ClientPublicKey string `json:"clientPublicKey,omitempty"`
}

// ConnectResponseDto mirrors com.example.network.ConnectResponseDto plus
// additive fields carrying whatever material is needed to actually
// configure a tunnel for the chosen protocol.
type ConnectResponseDto struct {
	Success             bool   `json:"success"`
	SessionID           string `json:"sessionId"`
	Status              string `json:"status"`
	VirtualIP           string `json:"virtualIp"`
	ServerID            string `json:"serverId"`
	AssignedPort        int    `json:"assignedPort"`
	HandshakeDurationMs int64  `json:"handshakeDurationMs"`
	Message             string `json:"message,omitempty"`

	// --- Additive (ignored by the current app build) ---
	Protocol string `json:"protocol,omitempty"`
	// VirtualIPv6 is only present on a dual-stack node; the client contract
	// has a single virtualIp field, so the v6 half travels as an addition.
	VirtualIPv6                string   `json:"virtualIpv6,omitempty"`
	ServerPublicKey            string   `json:"serverPublicKey,omitempty"`
	ClientPrivateKey           string   `json:"clientPrivateKey,omitempty"`
	Endpoint                   string   `json:"endpoint,omitempty"`
	AllowedIps                 string   `json:"allowedIps,omitempty"`
	DNS                        []string `json:"dns,omitempty"`
	MTU                        int      `json:"mtu,omitempty"`
	PersistentKeepaliveSeconds int      `json:"persistentKeepaliveSeconds,omitempty"`
	OvpnProfile                string   `json:"ovpnProfile,omitempty"`
}

// DisconnectRequestDto mirrors com.example.network.DisconnectRequestDto.
type DisconnectRequestDto struct {
	SessionID string `json:"sessionId"`
	Reason    string `json:"reason"`
}

// DisconnectResponseDto mirrors com.example.network.DisconnectResponseDto.
type DisconnectResponseDto struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ServerTelemetryDto mirrors com.example.network.ServerTelemetryDto.
type ServerTelemetryDto struct {
	ServerID               string    `json:"serverId"`
	Status                 string    `json:"status"`
	VirtualIP              string    `json:"virtualIp"`
	DownloadSpeedMbps      float32   `json:"downloadSpeedMbps"`
	UploadSpeedMbps        float32   `json:"uploadSpeedMbps"`
	TotalDownloadedBytes   int64     `json:"totalDownloadedBytes"`
	TotalUploadedBytes     int64     `json:"totalUploadedBytes"`
	SessionDurationSeconds int64     `json:"sessionDurationSeconds"`
	ServerHealth           string    `json:"serverHealth"`
	TrafficSamples         []float32 `json:"trafficSamples"`
}

// UpdateSettingsRequestDto mirrors com.example.network.UpdateSettingsRequestDto.
// The DTO carries no session/user identifier, so these are applied as this
// node's node-wide default policy (see policy.go) — documented explicitly
// in docs/API.md since it's the only defensible reading of the contract as
// given.
type UpdateSettingsRequestDto struct {
	KillSwitch       bool `json:"killSwitch"`
	ThreatProtection bool `json:"threatProtection"`
	SplitTunneling   bool `json:"splitTunneling"`
	AutoConnect      bool `json:"autoConnect"`
}

// BackendHealthDto mirrors com.example.network.BackendHealthDto.
type BackendHealthDto struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	ActiveNodes   int    `json:"activeNodes"`
	ClusterRegion string `json:"clusterRegion"`
}

// errorBody is used for every non-2xx response this API returns. It is not
// part of the client's declared contract, but shares enough shape
// (success/message) that it's self-explanatory to a developer inspecting
// responses with curl, and Moshi will happily ignore it on any endpoint
// that doesn't expect the "success"/"message" pair.
type errorBody struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// internalStatusDto is served only on /internal/v1/status, guarded by the
// fleet shared secret, and consumed by sibling nodes' fleet.Registry
// polling — never by the Android app.
type internalStatusDto struct {
	Healthy     bool `json:"healthy"`
	LoadPercent int  `json:"loadPercent"`
}
