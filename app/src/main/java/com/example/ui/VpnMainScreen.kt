package com.example.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Cached
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CloudDone
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Security
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material.icons.filled.Vibration
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.example.model.ConnectionStatus
import com.example.ui.components.BackendServerDialog
import com.example.ui.components.ConnectButton
import com.example.ui.components.ProtocolSelectionDialog
import com.example.ui.components.SecuritySettingsCard
import com.example.ui.components.SelectedServerCard
import com.example.ui.components.ServerSelectionBottomSheet
import com.example.ui.components.TrafficStatsCard
import com.example.ui.theme.CyberAmber
import com.example.ui.theme.CyberCyan
import com.example.ui.theme.CyberEmerald
import com.example.ui.theme.CyberRed
import com.example.ui.theme.SlateBorder
import com.example.ui.theme.SpaceNavy700
import com.example.ui.theme.SpaceNavy800
import com.example.ui.theme.SpaceNavy900
import com.example.ui.theme.TextMuted
import com.example.ui.theme.TextPrimary
import com.example.ui.theme.TextSecondary
import com.example.util.HapticHelper
import com.example.viewmodel.VpnViewModel
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun VpnMainScreen(
  viewModel: VpnViewModel,
  modifier: Modifier = Modifier
) {
  val context = LocalContext.current
  val composeHaptic = LocalHapticFeedback.current
  val hapticHelper = remember { HapticHelper(context) }

  val uiState by viewModel.uiState.collectAsStateWithLifecycle()
  val scope = rememberCoroutineScope()
  val snackbarHostState = remember { SnackbarHostState() }

  val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
  var isServerSheetOpen by remember { mutableStateOf(false) }
  var isProtocolDialogOpen by remember { mutableStateOf(false) }
  var isBackendDialogOpen by remember { mutableStateOf(false) }

  Scaffold(
    modifier = modifier
      .fillMaxSize()
      .background(SpaceNavy900)
      .navigationBarsPadding(),
    containerColor = SpaceNavy900,
    snackbarHost = { SnackbarHost(snackbarHostState) }
  ) { innerPadding ->
    Box(
      modifier = Modifier
        .fillMaxSize()
        .padding(innerPadding)
    ) {
      // Subtle ambient cyber background gradient
      Box(
        modifier = Modifier
          .fillMaxWidth()
          .height(300.dp)
          .background(
            Brush.verticalGradient(
              colors = listOf(
                when (uiState.connectionStatus) {
                  ConnectionStatus.DISCONNECTED -> CyberCyan.copy(alpha = 0.08f)
                  ConnectionStatus.CONNECTING -> CyberAmber.copy(alpha = 0.12f)
                  ConnectionStatus.CONNECTED -> CyberEmerald.copy(alpha = 0.14f)
                },
                Color.Transparent
              )
            )
          )
      )

      Column(
        modifier = Modifier
          .fillMaxSize()
          .statusBarsPadding()
          .verticalScroll(rememberScrollState())
          .padding(horizontal = 20.dp, vertical = 12.dp),
        horizontalAlignment = Alignment.CenterHorizontally
      ) {
        // TOP APP BAR WITH SERVER SYNC BADGE
        VpnTopBar(
          protocolName = uiState.selectedProtocol.displayName,
          backendUrl = uiState.backendUrl,
          onProtocolClick = {
            hapticHelper.performTick(composeHaptic)
            isProtocolDialogOpen = true
          },
          onBackendClick = {
            hapticHelper.performTick(composeHaptic)
            isBackendDialogOpen = true
          },
          onRefreshPings = {
            hapticHelper.performTick(composeHaptic)
            viewModel.refreshPings()
            scope.launch {
              snackbarHostState.showSnackbar("Sunucu gecikme süreleri (ping) güncellendi.")
            }
          }
        )

        Spacer(modifier = Modifier.height(14.dp))

        // BACKEND ARCHITECTURE PILL
        ServerDrivenIndicatorBanner(
          statusText = uiState.backendStatusText,
          isOnline = uiState.isBackendConnected,
          onClick = {
            hapticHelper.performTick(composeHaptic)
            isBackendDialogOpen = true
          }
        )

        Spacer(modifier = Modifier.height(14.dp))

        // STATUS BANNER
        ConnectionStatusBanner(
          status = uiState.connectionStatus,
          serverCountry = uiState.selectedServer.country
        )

        Spacer(modifier = Modifier.height(20.dp))

        // HERO CONNECT BUTTON (Trigger haptics upon user touch)
        ConnectButton(
          connectionStatus = uiState.connectionStatus,
          onToggle = {
            if (uiState.connectionStatus == ConnectionStatus.DISCONNECTED) {
              hapticHelper.performConnectTrigger(composeHaptic)
              viewModel.toggleConnection(
                onConnectSuccess = { hapticHelper.performSuccess() },
                onDisconnectSuccess = { hapticHelper.performDisconnect() }
              )
            } else if (uiState.connectionStatus == ConnectionStatus.CONNECTED) {
              hapticHelper.performDisconnect()
              viewModel.toggleConnection(
                onConnectSuccess = { hapticHelper.performSuccess() },
                onDisconnectSuccess = { hapticHelper.performDisconnect() }
              )
            } else {
              hapticHelper.performTick(composeHaptic)
              viewModel.toggleConnection()
            }
          }
        )

        Spacer(modifier = Modifier.height(24.dp))

        // SELECTED SERVER CARD
        SelectedServerCard(
          server = uiState.selectedServer,
          onClick = {
            hapticHelper.performTick(composeHaptic)
            isServerSheetOpen = true
          }
        )

        Spacer(modifier = Modifier.height(16.dp))

        // LIVE TRAFFIC / SPEED STATS CARD (computed server-side)
        TrafficStatsCard(uiState = uiState)

        Spacer(modifier = Modifier.height(16.dp))

        // SECURITY SETTINGS & TOGGLES (syncs to server firewall)
        SecuritySettingsCard(
          uiState = uiState,
          onToggleKillSwitch = {
            hapticHelper.performClick(composeHaptic)
            viewModel.toggleKillSwitch(it)
          },
          onToggleThreatProtection = {
            hapticHelper.performClick(composeHaptic)
            viewModel.toggleThreatProtection(it)
          },
          onToggleSplitTunneling = {
            hapticHelper.performClick(composeHaptic)
            viewModel.toggleSplitTunneling(it)
          },
          onToggleAutoConnect = {
            hapticHelper.performClick(composeHaptic)
            viewModel.toggleAutoConnect(it)
          }
        )

        Spacer(modifier = Modifier.height(28.dp))
      }
    }

    // SERVER SELECTION BOTTOM SHEET
    if (isServerSheetOpen) {
      ServerSelectionBottomSheet(
        servers = uiState.servers,
        selectedServer = uiState.selectedServer,
        sheetState = sheetState,
        onDismiss = { isServerSheetOpen = false },
        onSelectServer = { server ->
          hapticHelper.performClick(composeHaptic)
          viewModel.selectServer(server) {
            hapticHelper.performSuccess()
          }
          scope.launch {
            snackbarHostState.showSnackbar("Sunucu tüneli ${server.country} (${server.city}) konumuna yönlendirildi.")
          }
        },
        onToggleFavorite = { serverId ->
          hapticHelper.performTick(composeHaptic)
          viewModel.toggleFavorite(serverId)
        }
      )
    }

    // PROTOCOL SELECTION DIALOG
    if (isProtocolDialogOpen) {
      ProtocolSelectionDialog(
        selectedProtocol = uiState.selectedProtocol,
        onSelectProtocol = { protocol ->
          hapticHelper.performClick(composeHaptic)
          viewModel.setProtocol(protocol) {
            hapticHelper.performSuccess()
          }
          scope.launch {
            snackbarHostState.showSnackbar("${protocol.displayName} sunucu protokolüne geçildi.")
          }
        },
        onDismiss = { isProtocolDialogOpen = false }
      )
    }

    // BACKEND SERVER & HAPTICS CONFIG DIALOG
    if (isBackendDialogOpen) {
      BackendServerDialog(
        currentUrl = uiState.backendUrl,
        sessionId = uiState.activeSessionId,
        onSaveUrl = { newUrl ->
          hapticHelper.performSuccess()
          viewModel.setBackendUrl(newUrl)
          scope.launch {
            snackbarHostState.showSnackbar("Sunucu API uç noktası güncellendi: $newUrl")
          }
        },
        onTestHaptic = {
          hapticHelper.performConnectTrigger(composeHaptic)
          scope.launch {
            snackbarHostState.showSnackbar("Dokunumsal titreşim bildirimi iletildi.")
          }
        },
        onDismiss = { isBackendDialogOpen = false }
      )
    }
  }
}

@Composable
private fun ServerDrivenIndicatorBanner(
  statusText: String,
  isOnline: Boolean,
  onClick: () -> Unit,
  modifier: Modifier = Modifier
) {
  Row(
    modifier = modifier
      .fillMaxWidth()
      .clip(RoundedCornerShape(12.dp))
      .background(SpaceNavy800)
      .border(1.dp, if (isOnline) CyberCyan.copy(alpha = 0.35f) else SlateBorder, RoundedCornerShape(12.dp))
      .clickable(onClick = onClick)
      .padding(horizontal = 12.dp, vertical = 8.dp)
      .testTag("backend_indicator_banner"),
    verticalAlignment = Alignment.CenterVertically,
    horizontalArrangement = Arrangement.SpaceBetween
  ) {
    Row(verticalAlignment = Alignment.CenterVertically) {
      Box(
        modifier = Modifier
          .size(8.dp)
          .clip(CircleShape)
          .background(if (isOnline) CyberEmerald else CyberAmber)
      )
      Spacer(modifier = Modifier.width(8.dp))
      Text(
        text = statusText,
        style = MaterialTheme.typography.bodySmall.copy(fontWeight = FontWeight.Medium),
        color = TextSecondary,
        fontSize = 11.sp
      )
    }

    Row(verticalAlignment = Alignment.CenterVertically) {
      Icon(
        imageVector = Icons.Default.CloudDone,
        contentDescription = null,
        tint = CyberCyan,
        modifier = Modifier.size(15.dp)
      )
      Spacer(modifier = Modifier.width(4.dp))
      Text(
        text = "Sunucu",
        style = MaterialTheme.typography.labelSmall.copy(fontWeight = FontWeight.Bold),
        color = CyberCyan,
        fontSize = 10.sp
      )
    }
  }
}

@Composable
private fun VpnTopBar(
  protocolName: String,
  backendUrl: String,
  onProtocolClick: () -> Unit,
  onBackendClick: () -> Unit,
  onRefreshPings: () -> Unit,
  modifier: Modifier = Modifier
) {
  Row(
    modifier = modifier.fillMaxWidth(),
    horizontalArrangement = Arrangement.SpaceBetween,
    verticalAlignment = Alignment.CenterVertically
  ) {
    // Brand & Logo
    Row(verticalAlignment = Alignment.CenterVertically) {
      Box(
        modifier = Modifier
          .size(38.dp)
          .clip(RoundedCornerShape(10.dp))
          .background(SpaceNavy800)
          .border(1.dp, CyberCyan.copy(alpha = 0.5f), RoundedCornerShape(10.dp)),
        contentAlignment = Alignment.Center
      ) {
        Icon(
          imageVector = Icons.Default.Shield,
          contentDescription = null,
          tint = CyberCyan,
          modifier = Modifier.size(22.dp)
        )
      }

      Spacer(modifier = Modifier.width(10.dp))

      Column {
        Row(verticalAlignment = Alignment.CenterVertically) {
          Text(
            text = "Aura",
            style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.Black),
            color = TextPrimary
          )
          Spacer(modifier = Modifier.width(4.dp))
          Text(
            text = "VPN",
            style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.Black),
            color = CyberCyan
          )
          Spacer(modifier = Modifier.width(6.dp))
          Box(
            modifier = Modifier
              .clip(RoundedCornerShape(6.dp))
              .background(CyberEmerald.copy(alpha = 0.18f))
              .border(1.dp, CyberEmerald.copy(alpha = 0.4f), RoundedCornerShape(6.dp))
              .padding(horizontal = 6.dp, vertical = 1.dp)
          ) {
            Text(
              text = "PRO",
              style = MaterialTheme.typography.labelSmall.copy(fontWeight = FontWeight.ExtraBold),
              color = CyberEmerald,
              fontSize = 9.sp
            )
          }
        }
        Text(
          text = "Askeri Düzey Şifreleme",
          style = MaterialTheme.typography.bodySmall,
          color = TextMuted,
          fontSize = 11.sp
        )
      }
    }

    // Right action buttons: Backend Pill, Protocol Pill & Ping Refresh
    Row(verticalAlignment = Alignment.CenterVertically) {
      // Backend Hub Button
      IconButton(
        onClick = onBackendClick,
        modifier = Modifier
          .size(36.dp)
          .clip(RoundedCornerShape(10.dp))
          .background(SpaceNavy700)
          .border(1.dp, SlateBorder, RoundedCornerShape(10.dp))
          .testTag("backend_server_button")
      ) {
        Icon(
          imageVector = Icons.Default.Dns,
          contentDescription = "Sunucu Bağlantısı",
          tint = CyberCyan,
          modifier = Modifier.size(18.dp)
        )
      }

      Spacer(modifier = Modifier.width(6.dp))

      // Protocol Pill Button
      Box(
        modifier = Modifier
          .clip(RoundedCornerShape(10.dp))
          .background(SpaceNavy700)
          .border(1.dp, SlateBorder, RoundedCornerShape(10.dp))
          .clickable(onClick = onProtocolClick)
          .padding(horizontal = 10.dp, vertical = 8.dp)
          .testTag("protocol_selector_button")
      ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
          Box(
            modifier = Modifier
              .size(6.dp)
              .clip(CircleShape)
              .background(CyberCyan)
          )
          Spacer(modifier = Modifier.width(6.dp))
          Text(
            text = protocolName,
            style = MaterialTheme.typography.labelSmall.copy(fontWeight = FontWeight.SemiBold),
            color = TextPrimary,
            fontSize = 11.sp
          )
        }
      }

      Spacer(modifier = Modifier.width(6.dp))

      // Refresh Ping Button
      IconButton(
        onClick = onRefreshPings,
        modifier = Modifier
          .size(36.dp)
          .clip(RoundedCornerShape(10.dp))
          .background(SpaceNavy700)
          .border(1.dp, SlateBorder, RoundedCornerShape(10.dp))
          .testTag("refresh_pings_button")
      ) {
        Icon(
          imageVector = Icons.Default.Cached,
          contentDescription = "Ping Yenile",
          tint = TextSecondary,
          modifier = Modifier.size(18.dp)
        )
      }
    }
  }
}

@Composable
private fun ConnectionStatusBanner(
  status: ConnectionStatus,
  serverCountry: String,
  modifier: Modifier = Modifier
) {
  val bannerBgColor by animateColorAsState(
    targetValue = when (status) {
      ConnectionStatus.DISCONNECTED -> SpaceNavy800
      ConnectionStatus.CONNECTING -> CyberAmber.copy(alpha = 0.12f)
      ConnectionStatus.CONNECTED -> CyberEmerald.copy(alpha = 0.14f)
    },
    animationSpec = tween(400),
    label = "banner_bg"
  )

  val bannerBorderColor by animateColorAsState(
    targetValue = when (status) {
      ConnectionStatus.DISCONNECTED -> SlateBorder
      ConnectionStatus.CONNECTING -> CyberAmber.copy(alpha = 0.4f)
      ConnectionStatus.CONNECTED -> CyberEmerald.copy(alpha = 0.5f)
    },
    animationSpec = tween(400),
    label = "banner_border"
  )

  val statusColor = when (status) {
    ConnectionStatus.DISCONNECTED -> CyberRed
    ConnectionStatus.CONNECTING -> CyberAmber
    ConnectionStatus.CONNECTED -> CyberEmerald
  }

  val statusTitle = when (status) {
    ConnectionStatus.DISCONNECTED -> "Korumasız Bağlantı"
    ConnectionStatus.CONNECTING -> "Güvenli Tünel Kuruluyor"
    ConnectionStatus.CONNECTED -> "Ağınız Tamamen Korunuyor"
  }

  val statusSubtitle = when (status) {
    ConnectionStatus.DISCONNECTED -> "Trafiğiniz ve gerçek IP adresiniz internet sağlayıcınıza görünür."
    ConnectionStatus.CONNECTING -> "$serverCountry sunucusu ile şifreli anahtar değişimi yapılıyor..."
    ConnectionStatus.CONNECTED -> "256-bit AES şifreleme aktif. Tüm veri trafiği sunucu üzerinden korunuyor."
  }

  val statusIcon = when (status) {
    ConnectionStatus.DISCONNECTED -> Icons.Default.Warning
    ConnectionStatus.CONNECTING -> Icons.Default.Info
    ConnectionStatus.CONNECTED -> Icons.Default.Check
  }

  Box(
    modifier = modifier
      .fillMaxWidth()
      .clip(RoundedCornerShape(16.dp))
      .background(bannerBgColor)
      .border(1.dp, bannerBorderColor, RoundedCornerShape(16.dp))
      .padding(horizontal = 14.dp, vertical = 12.dp)
      .testTag("status_banner")
  ) {
    Row(
      verticalAlignment = Alignment.CenterVertically,
      modifier = Modifier.fillMaxWidth()
    ) {
      Box(
        modifier = Modifier
          .size(36.dp)
          .clip(CircleShape)
          .background(statusColor.copy(alpha = 0.15f)),
        contentAlignment = Alignment.Center
      ) {
        Icon(
          imageVector = statusIcon,
          contentDescription = null,
          tint = statusColor,
          modifier = Modifier.size(18.dp)
        )
      }

      Spacer(modifier = Modifier.width(12.dp))

      Column(modifier = Modifier.weight(1f)) {
        Text(
          text = statusTitle,
          style = MaterialTheme.typography.titleSmall.copy(fontWeight = FontWeight.Bold),
          color = TextPrimary
        )
        Text(
          text = statusSubtitle,
          style = MaterialTheme.typography.bodySmall,
          color = TextSecondary,
          fontSize = 11.sp,
          lineHeight = 14.sp
        )
      }
    }
  }
}
