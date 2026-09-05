package com.example.ui.components

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PowerSettingsNew
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material.icons.filled.Sync
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.ripple
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.draw.scale
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.example.model.ConnectionStatus
import com.example.ui.theme.CyberAmber
import com.example.ui.theme.CyberCyan
import com.example.ui.theme.CyberCyanLight
import com.example.ui.theme.CyberEmerald
import com.example.ui.theme.CyberEmeraldLight
import com.example.ui.theme.SpaceNavy600
import com.example.ui.theme.SpaceNavy700
import com.example.ui.theme.SpaceNavy800
import com.example.ui.theme.SpaceNavy900
import com.example.ui.theme.TextMuted
import com.example.ui.theme.TextPrimary

@Composable
fun ConnectButton(
  connectionStatus: ConnectionStatus,
  onToggle: () -> Unit,
  modifier: Modifier = Modifier
) {
  val infiniteTransition = rememberInfiniteTransition(label = "pulse_transition")

  val pulseScale1 by infiniteTransition.animateFloat(
    initialValue = 1f,
    targetValue = if (connectionStatus != ConnectionStatus.DISCONNECTED) 1.28f else 1.05f,
    animationSpec = infiniteRepeatable(
      animation = tween(1800, easing = FastOutSlowInEasing),
      repeatMode = RepeatMode.Reverse
    ),
    label = "pulse_scale_1"
  )

  val pulseAlpha1 by infiniteTransition.animateFloat(
    initialValue = if (connectionStatus != ConnectionStatus.DISCONNECTED) 0.35f else 0.1f,
    targetValue = 0.03f,
    animationSpec = infiniteRepeatable(
      animation = tween(1800, easing = FastOutSlowInEasing),
      repeatMode = RepeatMode.Reverse
    ),
    label = "pulse_alpha_1"
  )

  val connectingRotation by infiniteTransition.animateFloat(
    initialValue = 0f,
    targetValue = 360f,
    animationSpec = infiniteRepeatable(
      animation = tween(1000, easing = LinearEasing),
      repeatMode = RepeatMode.Restart
    ),
    label = "connecting_rotation"
  )

  val primaryGlowColor by animateColorAsState(
    targetValue = when (connectionStatus) {
      ConnectionStatus.DISCONNECTED -> CyberCyan
      ConnectionStatus.CONNECTING -> CyberAmber
      ConnectionStatus.CONNECTED -> CyberEmerald
    },
    animationSpec = tween(400),
    label = "glow_color"
  )

  val secondaryGlowColor by animateColorAsState(
    targetValue = when (connectionStatus) {
      ConnectionStatus.DISCONNECTED -> CyberCyanLight
      ConnectionStatus.CONNECTING -> CyberAmber
      ConnectionStatus.CONNECTED -> CyberEmeraldLight
    },
    animationSpec = tween(400),
    label = "glow_color_secondary"
  )

  Column(
    modifier = modifier,
    horizontalAlignment = Alignment.CenterHorizontally
  ) {
    Box(
      contentAlignment = Alignment.Center,
      modifier = Modifier.size(220.dp)
    ) {
      // Outer Ambient Glow Ripple Ring
      Box(
        modifier = Modifier
          .size(200.dp)
          .scale(pulseScale1)
          .clip(CircleShape)
          .background(primaryGlowColor.copy(alpha = pulseAlpha1))
      )

      // Middle Border Accent Ring
      Box(
        modifier = Modifier
          .size(175.dp)
          .clip(CircleShape)
          .background(SpaceNavy800.copy(alpha = 0.85f))
          .border(
            width = if (connectionStatus == ConnectionStatus.CONNECTED) 2.5.dp else 1.5.dp,
            brush = Brush.sweepGradient(
              listOf(
                primaryGlowColor.copy(alpha = 0.9f),
                secondaryGlowColor.copy(alpha = 0.4f),
                primaryGlowColor.copy(alpha = 0.9f)
              )
            ),
            shape = CircleShape
          )
      )

      // Main Interactive Core Button
      val buttonGradient = when (connectionStatus) {
        ConnectionStatus.DISCONNECTED -> Brush.verticalGradient(
          listOf(SpaceNavy700, SpaceNavy800, SpaceNavy900)
        )
        ConnectionStatus.CONNECTING -> Brush.verticalGradient(
          listOf(SpaceNavy600, CyberAmber.copy(alpha = 0.25f), SpaceNavy900)
        )
        ConnectionStatus.CONNECTED -> Brush.verticalGradient(
          listOf(CyberEmerald.copy(alpha = 0.35f), SpaceNavy700, SpaceNavy900)
        )
      }

      Box(
        contentAlignment = Alignment.Center,
        modifier = Modifier
          .size(136.dp)
          .shadow(
            elevation = if (connectionStatus == ConnectionStatus.CONNECTED) 20.dp else 8.dp,
            shape = CircleShape,
            ambientColor = primaryGlowColor,
            spotColor = primaryGlowColor
          )
          .clip(CircleShape)
          .background(buttonGradient)
          .border(
            width = 2.dp,
            color = primaryGlowColor.copy(alpha = if (connectionStatus == ConnectionStatus.CONNECTED) 0.9f else 0.5f),
            shape = CircleShape
          )
          .clickable(
            interactionSource = remember { MutableInteractionSource() },
            indication = ripple(bounded = true, color = primaryGlowColor),
            onClick = onToggle
          )
          .testTag("vpn_connect_button")
      ) {
        when (connectionStatus) {
          ConnectionStatus.DISCONNECTED -> {
            Icon(
              imageVector = Icons.Default.PowerSettingsNew,
              contentDescription = "Bağlan",
              tint = primaryGlowColor,
              modifier = Modifier.size(54.dp)
            )
          }
          ConnectionStatus.CONNECTING -> {
            Icon(
              imageVector = Icons.Default.Sync,
              contentDescription = "Bağlanılıyor",
              tint = CyberAmber,
              modifier = Modifier
                .size(54.dp)
                .rotate(connectingRotation)
            )
          }
          ConnectionStatus.CONNECTED -> {
            Icon(
              imageVector = Icons.Default.Shield,
              contentDescription = "Bağlantıyı Kes",
              tint = CyberEmerald,
              modifier = Modifier.size(54.dp)
            )
          }
        }
      }
    }

    Spacer(modifier = Modifier.height(8.dp))

    // Interactive Status Description Label
    val statusText = when (connectionStatus) {
      ConnectionStatus.DISCONNECTED -> "BAĞLANMAK İÇİN DOKUNUN"
      ConnectionStatus.CONNECTING -> "BAĞLANTI KURULUYOR..."
      ConnectionStatus.CONNECTED -> "GÜVENLİ BAĞLANTI AKTİF"
    }

    Text(
      text = statusText,
      style = MaterialTheme.typography.labelMedium.copy(
        fontWeight = FontWeight.Bold,
        letterSpacing = 1.8.sp
      ),
      color = if (connectionStatus == ConnectionStatus.CONNECTED) CyberEmerald else if (connectionStatus == ConnectionStatus.CONNECTING) CyberAmber else TextMuted
    )
  }
}
