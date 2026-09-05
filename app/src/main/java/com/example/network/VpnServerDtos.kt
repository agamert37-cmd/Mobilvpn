package com.example.network

data class ServerNodeDto(
  val id: String,
  val country: String,
  val city: String,
  val flagEmoji: String,
  val ipAddress: String,
  val pingMs: Int,
  val loadPercent: Int,
  val categoryNames: List<String>,
  val isFastest: Boolean = false
)

data class ConnectRequestDto(
  val serverId: String,
  val protocol: String,
  val clientTimestamp: Long = System.currentTimeMillis()
)

data class ConnectResponseDto(
  val success: Boolean,
  val sessionId: String,
  val status: String,
  val virtualIp: String,
  val serverId: String,
  val assignedPort: Int = 51820,
  val handshakeDurationMs: Long = 180L,
  val message: String? = null
)

data class DisconnectRequestDto(
  val sessionId: String?,
  val reason: String = "USER_REQUEST"
)

data class DisconnectResponseDto(
  val success: Boolean,
  val message: String
)

data class ServerTelemetryDto(
  val serverId: String,
  val status: String,
  val virtualIp: String,
  val downloadSpeedMbps: Float,
  val uploadSpeedMbps: Float,
  val totalDownloadedBytes: Long,
  val totalUploadedBytes: Long,
  val sessionDurationSeconds: Long,
  val serverHealth: String = "OPTIMAL",
  val trafficSamples: List<Float> = emptyList()
)

data class UpdateSettingsRequestDto(
  val killSwitch: Boolean,
  val threatProtection: Boolean,
  val splitTunneling: Boolean,
  val autoConnect: Boolean
)

data class BackendHealthDto(
  val status: String,
  val version: String,
  val activeNodes: Int,
  val clusterRegion: String
)
