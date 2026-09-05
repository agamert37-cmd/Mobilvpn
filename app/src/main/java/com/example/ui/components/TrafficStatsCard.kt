package com.example.ui.components

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowDownward
import androidx.compose.material.icons.filled.ArrowUpward
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Schedule
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.example.model.ConnectionStatus
import com.example.model.VpnUiState
import com.example.ui.theme.CardBackground
import com.example.ui.theme.CyberCyan
import com.example.ui.theme.CyberCyanLight
import com.example.ui.theme.CyberEmerald
import com.example.ui.theme.SlateBorder
import com.example.ui.theme.SpaceNavy700
import com.example.ui.theme.SpaceNavy800
import com.example.ui.theme.TextMuted
import com.example.ui.theme.TextPrimary
import com.example.ui.theme.TextSecondary
import java.util.Locale

@Composable
fun TrafficStatsCard(
  uiState: VpnUiState,
  modifier: Modifier = Modifier
) {
  val isConnected = uiState.connectionStatus == ConnectionStatus.CONNECTED

  Card(
    modifier = modifier
      .fillMaxWidth()
      .clip(RoundedCornerShape(20.dp))
      .border(1.dp, SlateBorder, RoundedCornerShape(20.dp))
      .testTag("traffic_stats_card"),
    colors = CardDefaults.cardColors(containerColor = CardBackground),
    elevation = CardDefaults.cardElevation(defaultElevation = 2.dp)
  ) {
    Column(
      modifier = Modifier
        .fillMaxWidth()
        .padding(16.dp)
    ) {
      // Header: Connection Duration & Virtual IP
      Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically
      ) {
        // Duration Timer
        Row(
          verticalAlignment = Alignment.CenterVertically,
          modifier = Modifier
            .clip(RoundedCornerShape(8.dp))
            .background(SpaceNavy700)
            .padding(horizontal = 8.dp, vertical = 4.dp)
        ) {
          Icon(
            imageVector = Icons.Default.Schedule,
            contentDescription = null,
            tint = if (isConnected) CyberCyan else TextMuted,
            modifier = Modifier.size(14.dp)
          )
          Spacer(modifier = Modifier.width(6.dp))
          val formattedTime = remember(uiState.sessionDurationSeconds) {
            val hours = uiState.sessionDurationSeconds / 3600
            val minutes = (uiState.sessionDurationSeconds % 3600) / 60
            val seconds = uiState.sessionDurationSeconds % 60
            String.format(Locale.getDefault(), "%02d:%02d:%02d", hours, minutes, seconds)
          }
          Text(
            text = formattedTime,
            style = MaterialTheme.typography.labelMedium.copy(
              fontWeight = FontWeight.Bold,
              fontFamily = FontFamily.Monospace
            ),
            color = if (isConnected) TextPrimary else TextMuted
          )
        }

        // IP Badge
        Row(
          verticalAlignment = Alignment.CenterVertically,
          modifier = Modifier
            .clip(RoundedCornerShape(8.dp))
            .background(if (isConnected) CyberEmerald.copy(alpha = 0.12f) else SpaceNavy700)
            .border(
              1.dp,
              if (isConnected) CyberEmerald.copy(alpha = 0.4f) else SlateBorder,
              RoundedCornerShape(8.dp)
            )
            .padding(horizontal = 8.dp, vertical = 4.dp)
        ) {
          Icon(
            imageVector = Icons.Default.Lock,
            contentDescription = null,
            tint = if (isConnected) CyberEmerald else TextMuted,
            modifier = Modifier.size(12.dp)
          )
          Spacer(modifier = Modifier.width(4.dp))
          Text(
            text = if (isConnected) "IP: ${uiState.virtualIp}" else "Gerçek IP: ${uiState.realIp}",
            style = MaterialTheme.typography.labelSmall.copy(
              fontWeight = FontWeight.Medium,
              fontFamily = FontFamily.Monospace
            ),
            color = if (isConnected) CyberEmerald else TextSecondary,
            fontSize = 11.sp
          )
        }
      }

      Spacer(modifier = Modifier.height(16.dp))

      // Speeds Row
      Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(12.dp)
      ) {
        // Download Speed Box
        SpeedStatBox(
          title = "İNDİRME",
          speed = String.format(Locale.getDefault(), "%.1f", uiState.downloadSpeedMbps),
          unit = "Mbps",
          total = formatBytes(uiState.totalDownloadedBytes),
          icon = Icons.Default.ArrowDownward,
          accentColor = CyberCyan,
          modifier = Modifier.weight(1f)
        )

        // Upload Speed Box
        SpeedStatBox(
          title = "YÜKLEME",
          speed = String.format(Locale.getDefault(), "%.1f", uiState.uploadSpeedMbps),
          unit = "Mbps",
          total = formatBytes(uiState.totalUploadedBytes),
          icon = Icons.Default.ArrowUpward,
          accentColor = CyberEmerald,
          modifier = Modifier.weight(1f)
        )
      }

      Spacer(modifier = Modifier.height(14.dp))

      // Live Bandwidth Waveform Graph
      AnimatedVisibility(
        visible = isConnected,
        enter = fadeIn(),
        exit = fadeOut()
      ) {
        Column(modifier = Modifier.fillMaxWidth()) {
          Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically
          ) {
            Text(
              text = "CANLI AĞ TRAFİĞİ",
              style = MaterialTheme.typography.labelSmall.copy(
                fontWeight = FontWeight.SemiBold,
                letterSpacing = 1.sp
              ),
              color = TextMuted,
              fontSize = 10.sp
            )
            Text(
              text = "Gerçek Zamanlı",
              style = MaterialTheme.typography.labelSmall,
              color = CyberCyan,
              fontSize = 10.sp
            )
          }

          Spacer(modifier = Modifier.height(6.dp))

          LiveWaveformCanvas(
            trafficData = uiState.trafficHistory,
            modifier = Modifier
              .fillMaxWidth()
              .height(48.dp)
              .clip(RoundedCornerShape(10.dp))
              .background(SpaceNavy800)
              .padding(horizontal = 4.dp, vertical = 2.dp)
          )
        }
      }
    }
  }
}

@Composable
private fun SpeedStatBox(
  title: String,
  speed: String,
  unit: String,
  total: String,
  icon: androidx.compose.ui.graphics.vector.ImageVector,
  accentColor: Color,
  modifier: Modifier = Modifier
) {
  Box(
    modifier = modifier
      .clip(RoundedCornerShape(14.dp))
      .background(SpaceNavy700)
      .padding(12.dp)
  ) {
    Column {
      Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
        modifier = Modifier.fillMaxWidth()
      ) {
        Text(
          text = title,
          style = MaterialTheme.typography.labelSmall.copy(
            fontWeight = FontWeight.Bold,
            letterSpacing = 0.8.sp
          ),
          color = TextMuted,
          fontSize = 10.sp
        )
        Box(
          modifier = Modifier
            .size(20.dp)
            .clip(CircleShape)
            .background(accentColor.copy(alpha = 0.15f)),
          contentAlignment = Alignment.Center
        ) {
          Icon(
            imageVector = icon,
            contentDescription = null,
            tint = accentColor,
            modifier = Modifier.size(12.dp)
          )
        }
      }

      Spacer(modifier = Modifier.height(6.dp))

      Row(verticalAlignment = Alignment.Bottom) {
        Text(
          text = speed,
          style = MaterialTheme.typography.titleLarge.copy(
            fontWeight = FontWeight.ExtraBold,
            fontFamily = FontFamily.Monospace
          ),
          color = TextPrimary
        )
        Spacer(modifier = Modifier.width(4.dp))
        Text(
          text = unit,
          style = MaterialTheme.typography.bodySmall,
          color = accentColor,
          modifier = Modifier.padding(bottom = 2.dp)
        )
      }

      Spacer(modifier = Modifier.height(4.dp))

      Text(
        text = "Toplam: $total",
        style = MaterialTheme.typography.labelSmall,
        color = TextSecondary,
        fontSize = 10.sp
      )
    }
  }
}

@Composable
private fun LiveWaveformCanvas(
  trafficData: List<Float>,
  modifier: Modifier = Modifier
) {
  Canvas(modifier = modifier) {
    if (trafficData.isEmpty()) return@Canvas

    val width = size.width
    val height = size.height
    val maxVal = (trafficData.maxOrNull() ?: 100f).coerceAtLeast(40f)
    val stepX = width / (trafficData.size - 1).coerceAtLeast(1)

    val strokePath = Path()
    val fillPath = Path()

    trafficData.forEachIndexed { index, value ->
      val x = index * stepX
      val normalizedY = 1f - (value / maxVal)
      val y = (normalizedY * (height - 8f) + 4f).coerceIn(4f, height - 4f)

      if (index == 0) {
        strokePath.moveTo(x, y)
        fillPath.moveTo(x, height)
        fillPath.lineTo(x, y)
      } else {
        strokePath.lineTo(x, y)
        fillPath.lineTo(x, y)
      }

      if (index == trafficData.size - 1) {
        fillPath.lineTo(x, height)
        fillPath.close()
      }
    }

    // Fill gradient
    drawPath(
      path = fillPath,
      brush = Brush.verticalGradient(
        colors = listOf(
          CyberCyan.copy(alpha = 0.35f),
          CyberCyan.copy(alpha = 0.03f)
        )
      )
    )

    // Stroke line
    drawPath(
      path = strokePath,
      brush = Brush.horizontalGradient(
        colors = listOf(CyberCyanLight, CyberCyan)
      ),
      style = Stroke(width = 2.5f, cap = StrokeCap.Round)
    )

    // Highlight dot at latest point
    if (trafficData.isNotEmpty()) {
      val lastIndex = trafficData.size - 1
      val lastX = lastIndex * stepX
      val lastNormY = 1f - (trafficData.last() / maxVal)
      val lastY = (lastNormY * (height - 8f) + 4f).coerceIn(4f, height - 4f)

      drawCircle(
        color = CyberCyan,
        radius = 4.5f,
        center = Offset(lastX, lastY)
      )
      drawCircle(
        color = Color.White,
        radius = 2f,
        center = Offset(lastX, lastY)
      )
    }
  }
}

private fun formatBytes(bytes: Long): String {
  if (bytes < 1024) return "$bytes B"
  val kb = bytes / 1024.0
  if (kb < 1024) return String.format(Locale.getDefault(), "%.1f KB", kb)
  val mb = kb / 1024.0
  if (mb < 1024) return String.format(Locale.getDefault(), "%.1f MB", mb)
  val gb = mb / 1024.0
  return String.format(Locale.getDefault(), "%.2f GB", gb)
}
