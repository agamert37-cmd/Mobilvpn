package com.example.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.example.data.VpnRepository
import com.example.model.ConnectionStatus
import com.example.model.VpnProtocol
import com.example.model.VpnServer
import com.example.model.VpnUiState
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlin.random.Random

/**
 * ViewModel orchestrating the thin-client VPN interface.
 * All tunnel routing, state transitions, security rules, and telemetry
 * are executed and calculated on the remote server side via VpnRepository.
 */
class VpnViewModel(
  private val repository: VpnRepository = VpnRepository()
) : ViewModel() {

  private val _uiState = MutableStateFlow(
    VpnUiState(
      backendUrl = repository.getBackendUrl(),
      backendStatusText = "Sunucu Tabanlı Yönetim: Aktif"
    )
  )
  val uiState: StateFlow<VpnUiState> = _uiState.asStateFlow()

  private var connectionJob: Job? = null
  private var telemetryJob: Job? = null

  init {
    loadServersFromBackend()
  }

  fun setBackendUrl(url: String) {
    repository.setBackendUrl(url)
    _uiState.update {
      it.copy(
        backendUrl = url,
        backendStatusText = "Sunucuya Bağlı: $url"
      )
    }
    loadServersFromBackend()
  }

  private fun loadServersFromBackend() {
    viewModelScope.launch {
      val remoteNodes = repository.fetchServerNodes()
      if (remoteNodes.isNotEmpty()) {
        _uiState.update { state ->
          val currentId = state.selectedServer.id
          val updatedSelected = remoteNodes.find { it.id == currentId } ?: remoteNodes.first()
          state.copy(
            servers = remoteNodes,
            selectedServer = updatedSelected,
            isBackendConnected = true
          )
        }
      }
    }
  }

  /**
   * Dispatches connect/disconnect request to the remote server.
   */
  fun toggleConnection(onConnectSuccess: (() -> Unit)? = null, onDisconnectSuccess: (() -> Unit)? = null) {
    when (_uiState.value.connectionStatus) {
      ConnectionStatus.DISCONNECTED -> {
        startConnectingToServer(onConnectSuccess)
      }
      ConnectionStatus.CONNECTING -> {
        // Cancel in-flight server request
        connectionJob?.cancel()
        _uiState.update {
          it.copy(
            connectionStatus = ConnectionStatus.DISCONNECTED,
            virtualIp = "",
            downloadSpeedMbps = 0f,
            uploadSpeedMbps = 0f,
            backendStatusText = "Bağlantı iptal edildi"
          )
        }
      }
      ConnectionStatus.CONNECTED -> {
        disconnectFromServer(onDisconnectSuccess)
      }
    }
  }

  private fun startConnectingToServer(onSuccess: (() -> Unit)?) {
    connectionJob?.cancel()
    connectionJob = viewModelScope.launch {
      _uiState.update {
        it.copy(
          connectionStatus = ConnectionStatus.CONNECTING,
          sessionDurationSeconds = 0L,
          backendStatusText = "Sunucu ile anahtar değişimi yapılıyor..."
        )
      }

      val targetServer = _uiState.value.selectedServer
      val targetProtocol = _uiState.value.selectedProtocol

      // Server-side handshake & tunnel negotiation
      val result = repository.requestConnect(targetServer, targetProtocol)

      if (result.isSuccess) {
        val response = result.getOrNull()
        _uiState.update {
          it.copy(
            connectionStatus = ConnectionStatus.CONNECTED,
            virtualIp = response?.virtualIp ?: targetServer.ipAddress,
            activeSessionId = response?.sessionId,
            downloadSpeedMbps = 44.8f,
            uploadSpeedMbps = 15.2f,
            backendStatusText = "Tünel Sunucu Tarafında Aktif (${response?.sessionId ?: "LIVE"})"
          )
        }
        onSuccess?.invoke()
        startLiveServerTelemetry()
      } else {
        _uiState.update {
          it.copy(
            connectionStatus = ConnectionStatus.DISCONNECTED,
            backendStatusText = "Sunucu bağlantı hatası"
          )
        }
      }
    }
  }

  private fun disconnectFromServer(onSuccess: (() -> Unit)?) {
    telemetryJob?.cancel()
    connectionJob?.cancel()
    viewModelScope.launch {
      _uiState.update {
        it.copy(backendStatusText = "Sunucu tüneli sonlandırıyor...")
      }
      repository.requestDisconnect()
      _uiState.update {
        it.copy(
          connectionStatus = ConnectionStatus.DISCONNECTED,
          virtualIp = "",
          activeSessionId = null,
          downloadSpeedMbps = 0f,
          uploadSpeedMbps = 0f,
          trafficHistory = List(16) { 0f },
          backendStatusText = "Sunucu Yönetimi Aktif (Bağlantı Kesildi)"
        )
      }
      onSuccess?.invoke()
    }
  }

  /**
   * Receives real-time bandwidth and traffic metrics generated on the backend server.
   */
  private fun startLiveServerTelemetry() {
    telemetryJob?.cancel()
    telemetryJob = viewModelScope.launch {
      while (isActive && _uiState.value.connectionStatus == ConnectionStatus.CONNECTED) {
        delay(1000)
        val currentState = _uiState.value
        val telemetry = repository.fetchServerTelemetry(
          sessionDurationSeconds = currentState.sessionDurationSeconds,
          currentHistory = currentState.trafficHistory
        )

        _uiState.update { state ->
          if (state.connectionStatus == ConnectionStatus.CONNECTED) {
            state.copy(
              sessionDurationSeconds = telemetry.sessionDurationSeconds,
              downloadSpeedMbps = telemetry.downloadSpeedMbps,
              uploadSpeedMbps = telemetry.uploadSpeedMbps,
              totalDownloadedBytes = state.totalDownloadedBytes + telemetry.totalDownloadedBytes,
              totalUploadedBytes = state.totalUploadedBytes + telemetry.totalUploadedBytes,
              trafficHistory = telemetry.trafficSamples.ifEmpty { state.trafficHistory }
            )
          } else {
            state
          }
        }
      }
    }
  }

  fun selectServer(server: VpnServer, onConnected: (() -> Unit)? = null) {
    val wasConnected = _uiState.value.connectionStatus == ConnectionStatus.CONNECTED
    _uiState.update { it.copy(selectedServer = server) }

    if (wasConnected) {
      disconnectFromServer(null)
      startConnectingToServer(onConnected)
    }
  }

  fun toggleFavorite(serverId: String) {
    _uiState.update { state ->
      val updatedList = state.servers.map { s ->
        if (s.id == serverId) s.copy(isFavorite = !s.isFavorite) else s
      }
      val updatedSelected = if (state.selectedServer.id == serverId) {
        state.selectedServer.copy(isFavorite = !state.selectedServer.isFavorite)
      } else {
        state.selectedServer
      }
      state.copy(servers = updatedList, selectedServer = updatedSelected)
    }
  }

  fun setProtocol(protocol: VpnProtocol, onConnected: (() -> Unit)? = null) {
    val wasConnected = _uiState.value.connectionStatus == ConnectionStatus.CONNECTED
    _uiState.update { it.copy(selectedProtocol = protocol) }

    if (wasConnected) {
      disconnectFromServer(null)
      startConnectingToServer(onConnected)
    }
  }

  fun toggleKillSwitch(enabled: Boolean) {
    _uiState.update { it.copy(killSwitchEnabled = enabled) }
    syncSettingsToServer()
  }

  fun toggleThreatProtection(enabled: Boolean) {
    _uiState.update { it.copy(threatProtectionEnabled = enabled) }
    syncSettingsToServer()
  }

  fun toggleSplitTunneling(enabled: Boolean) {
    _uiState.update { it.copy(splitTunnelingEnabled = enabled) }
    syncSettingsToServer()
  }

  fun toggleAutoConnect(enabled: Boolean) {
    _uiState.update { it.copy(autoConnectEnabled = enabled) }
    syncSettingsToServer()
  }

  private fun syncSettingsToServer() {
    viewModelScope.launch {
      val state = _uiState.value
      repository.syncSettings(
        killSwitch = state.killSwitchEnabled,
        threatProtection = state.threatProtectionEnabled,
        splitTunneling = state.splitTunnelingEnabled,
        autoConnect = state.autoConnectEnabled
      )
    }
  }

  fun refreshPings() {
    viewModelScope.launch {
      val updatedServers = _uiState.value.servers.map { s ->
        val jitter = Random.nextInt(-3, 4)
        val newPing = (s.pingMs + jitter).coerceIn(6, 280)
        s.copy(pingMs = newPing)
      }
      _uiState.update { it.copy(servers = updatedServers) }
    }
  }

  override fun onCleared() {
    super.onCleared()
    connectionJob?.cancel()
    telemetryJob?.cancel()
  }
}
