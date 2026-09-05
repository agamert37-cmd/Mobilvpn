package com.example.model

enum class ConnectionStatus {
  DISCONNECTED,
  CONNECTING,
  CONNECTED
}

enum class ServerCategory(val title: String) {
  FASTEST("En Hızlı"),
  STREAMING("Yayın & Dizi"),
  P2P("P2P & Torrent"),
  ALL("Tüm Konumlar")
}

enum class VpnProtocol(
  val displayName: String,
  val badge: String,
  val description: String,
  val encryption: String
) {
  WIREGUARD(
    displayName = "WireGuard®",
    badge = "Önerilen",
    description = "Ultra yüksek hız, modern kriptografi ve minimum pil tüketimi.",
    encryption = "ChaCha20-Poly1305"
  ),
  OPENVPN_UDP(
    displayName = "OpenVPN (UDP)",
    badge = "Hızlı",
    description = "Yayın izleme ve çevrimiçi oyunlar için optimize edilmiş hız.",
    encryption = "AES-256-GCM"
  ),
  OPENVPN_TCP(
    displayName = "OpenVPN (TCP)",
    badge = "Kararlı",
    description = "Sıkı güvenlik duvarları ve kısıtlı ağları aşmak için kararlı protokol.",
    encryption = "AES-256-CBC"
  ),
  IKEV2(
    displayName = "IKEv2 / IPsec",
    badge = "Mobil",
    description = "Wi-Fi ve hücresel veri arasında hızlı ve kesintisiz otomatik geçiş.",
    encryption = "AES-256"
  )
}

data class VpnServer(
  val id: String,
  val country: String,
  val city: String,
  val flagEmoji: String,
  val ipAddress: String,
  val pingMs: Int,
  val loadPercent: Int,
  val categories: List<ServerCategory>,
  val isFavorite: Boolean = false,
  val isFastest: Boolean = false
)

data class VpnUiState(
  val connectionStatus: ConnectionStatus = ConnectionStatus.DISCONNECTED,
  val selectedServer: VpnServer = defaultServers.first(),
  val servers: List<VpnServer> = defaultServers,
  val realIp: String = "85.105.142.68",
  val virtualIp: String = "",
  val sessionDurationSeconds: Long = 0L,
  val downloadSpeedMbps: Float = 0f,
  val uploadSpeedMbps: Float = 0f,
  val totalDownloadedBytes: Long = 0L,
  val totalUploadedBytes: Long = 0L,
  val selectedProtocol: VpnProtocol = VpnProtocol.WIREGUARD,
  val killSwitchEnabled: Boolean = true,
  val threatProtectionEnabled: Boolean = true,
  val splitTunnelingEnabled: Boolean = false,
  val autoConnectEnabled: Boolean = false,
  val trafficHistory: List<Float> = List(16) { 0f },
  val backendUrl: String = "https://api.auravpn.internal/",
  val isBackendConnected: Boolean = true,
  val backendStatusText: String = "Sunucu Yönetimi Aktif",
  val activeSessionId: String? = null
)

val defaultServers = listOf(
  VpnServer(
    id = "de_fra",
    country = "Almanya",
    city = "Frankfurt #12",
    flagEmoji = "🇩🇪",
    ipAddress = "194.26.29.110",
    pingMs = 18,
    loadPercent = 28,
    categories = listOf(ServerCategory.FASTEST, ServerCategory.ALL),
    isFastest = true
  ),
  VpnServer(
    id = "nl_ams",
    country = "Hollanda",
    city = "Amsterdam #04",
    flagEmoji = "🇳🇱",
    ipAddress = "185.107.56.204",
    pingMs = 24,
    loadPercent = 34,
    categories = listOf(ServerCategory.FASTEST, ServerCategory.STREAMING, ServerCategory.ALL)
  ),
  VpnServer(
    id = "tr_ist",
    country = "Türkiye",
    city = "İstanbul #01",
    flagEmoji = "🇹🇷",
    ipAddress = "176.240.12.8",
    pingMs = 8,
    loadPercent = 22,
    categories = listOf(ServerCategory.FASTEST, ServerCategory.ALL)
  ),
  VpnServer(
    id = "gb_lon",
    country = "Birleşik Krallık",
    city = "Londra #08",
    flagEmoji = "🇬🇧",
    ipAddress = "185.220.101.5",
    pingMs = 32,
    loadPercent = 46,
    categories = listOf(ServerCategory.STREAMING, ServerCategory.ALL)
  ),
  VpnServer(
    id = "us_nyc",
    country = "Amerika Birleşik Devletleri",
    city = "New York #19",
    flagEmoji = "🇺🇸",
    ipAddress = "104.28.19.112",
    pingMs = 88,
    loadPercent = 54,
    categories = listOf(ServerCategory.STREAMING, ServerCategory.ALL)
  ),
  VpnServer(
    id = "us_lax",
    country = "Amerika Birleşik Devletleri",
    city = "Los Angeles #07",
    flagEmoji = "🇺🇸",
    ipAddress = "198.54.130.40",
    pingMs = 115,
    loadPercent = 62,
    categories = listOf(ServerCategory.STREAMING, ServerCategory.P2P, ServerCategory.ALL)
  ),
  VpnServer(
    id = "jp_tyo",
    country = "Japonya",
    city = "Tokyo #03",
    flagEmoji = "🇯🇵",
    ipAddress = "133.130.54.12",
    pingMs = 158,
    loadPercent = 41,
    categories = listOf(ServerCategory.STREAMING, ServerCategory.ALL)
  ),
  VpnServer(
    id = "sg_sin",
    country = "Singapur",
    city = "Singapur #02",
    flagEmoji = "🇸🇬",
    ipAddress = "128.199.202.15",
    pingMs = 138,
    loadPercent = 37,
    categories = listOf(ServerCategory.P2P, ServerCategory.ALL)
  ),
  VpnServer(
    id = "ch_zrh",
    country = "İsviçre",
    city = "Zürih #05",
    flagEmoji = "🇨🇭",
    ipAddress = "194.230.14.99",
    pingMs = 26,
    loadPercent = 19,
    categories = listOf(ServerCategory.FASTEST, ServerCategory.ALL)
  ),
  VpnServer(
    id = "fr_par",
    country = "Fransa",
    city = "Paris #11",
    flagEmoji = "🇫🇷",
    ipAddress = "51.15.110.82",
    pingMs = 30,
    loadPercent = 48,
    categories = listOf(ServerCategory.STREAMING, ServerCategory.ALL)
  ),
  VpnServer(
    id = "ca_tor",
    country = "Kanada",
    city = "Toronto #06",
    flagEmoji = "🇨🇦",
    ipAddress = "142.44.210.15",
    pingMs = 96,
    loadPercent = 50,
    categories = listOf(ServerCategory.P2P, ServerCategory.ALL)
  )
)
