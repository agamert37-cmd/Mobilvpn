package com.example.ui.components

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
import androidx.compose.material.icons.filled.GppGood
import androidx.compose.material.icons.filled.Security
import androidx.compose.material.icons.filled.SwapCalls
import androidx.compose.material.icons.filled.WifiFind
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.example.model.VpnUiState
import com.example.ui.theme.CardBackground
import com.example.ui.theme.CyberCyan
import com.example.ui.theme.CyberEmerald
import com.example.ui.theme.SlateBorder
import com.example.ui.theme.SpaceNavy600
import com.example.ui.theme.SpaceNavy700
import com.example.ui.theme.TextMuted
import com.example.ui.theme.TextPrimary
import com.example.ui.theme.TextSecondary

@Composable
fun SecuritySettingsCard(
  uiState: VpnUiState,
  onToggleKillSwitch: (Boolean) -> Unit,
  onToggleThreatProtection: (Boolean) -> Unit,
  onToggleSplitTunneling: (Boolean) -> Unit,
  onToggleAutoConnect: (Boolean) -> Unit,
  modifier: Modifier = Modifier
) {
  Card(
    modifier = modifier
      .fillMaxWidth()
      .clip(RoundedCornerShape(20.dp))
      .border(1.dp, SlateBorder, RoundedCornerShape(20.dp))
      .testTag("security_settings_card"),
    colors = CardDefaults.cardColors(containerColor = CardBackground),
    elevation = CardDefaults.cardElevation(defaultElevation = 2.dp)
  ) {
    Column(
      modifier = Modifier
        .fillMaxWidth()
        .padding(16.dp)
    ) {
      Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier.fillMaxWidth()
      ) {
        Icon(
          imageVector = Icons.Default.Security,
          contentDescription = null,
          tint = CyberCyan,
          modifier = Modifier.size(16.dp)
        )
        Spacer(modifier = Modifier.width(6.dp))
        Text(
          text = "GÜVENLİK VE GİZLİLİK KALKANI",
          style = MaterialTheme.typography.labelSmall.copy(
            fontWeight = FontWeight.SemiBold,
            letterSpacing = 1.2.sp
          ),
          color = TextMuted
        )
      }

      Spacer(modifier = Modifier.height(14.dp))

      SecurityToggleRow(
        title = "Kill Switch",
        subtitle = "Bağlantı kesildiğinde internet trafiğini anında durdur",
        icon = Icons.Default.GppGood,
        iconColor = CyberEmerald,
        checked = uiState.killSwitchEnabled,
        onCheckedChange = onToggleKillSwitch,
        testTag = "toggle_kill_switch"
      )

      Spacer(modifier = Modifier.height(10.dp))

      SecurityToggleRow(
        title = "Tehdit Koruması",
        subtitle = "Zararlı web sitelerini, reklamları ve izleyicileri engelle",
        icon = Icons.Default.Security,
        iconColor = CyberCyan,
        checked = uiState.threatProtectionEnabled,
        onCheckedChange = onToggleThreatProtection,
        testTag = "toggle_threat_protection"
      )

      Spacer(modifier = Modifier.height(10.dp))

      SecurityToggleRow(
        title = "Bölünmüş Tünelleme (Split Tunneling)",
        subtitle = "Seçilen bankacılık/yerel uygulamaları VPN dışı tut",
        icon = Icons.Default.SwapCalls,
        iconColor = TextSecondary,
        checked = uiState.splitTunnelingEnabled,
        onCheckedChange = onToggleSplitTunneling,
        testTag = "toggle_split_tunneling"
      )

      Spacer(modifier = Modifier.height(10.dp))

      SecurityToggleRow(
        title = "Otomatik Güvenli Bağlantı",
        subtitle = "Güvensiz açık Wi-Fi ağlarına bağlanıldığında otomatik aç",
        icon = Icons.Default.WifiFind,
        iconColor = TextSecondary,
        checked = uiState.autoConnectEnabled,
        onCheckedChange = onToggleAutoConnect,
        testTag = "toggle_auto_connect"
      )
    }
  }
}

@Composable
private fun SecurityToggleRow(
  title: String,
  subtitle: String,
  icon: ImageVector,
  iconColor: Color,
  checked: Boolean,
  onCheckedChange: (Boolean) -> Unit,
  testTag: String
) {
  Row(
    modifier = Modifier
      .fillMaxWidth()
      .clip(RoundedCornerShape(12.dp))
      .background(SpaceNavy700.copy(alpha = 0.6f))
      .padding(horizontal = 12.dp, vertical = 10.dp),
    verticalAlignment = Alignment.CenterVertically,
    horizontalArrangement = Arrangement.SpaceBetween
  ) {
    Row(
      verticalAlignment = Alignment.CenterVertically,
      modifier = Modifier.weight(1f)
    ) {
      Box(
        modifier = Modifier
          .size(34.dp)
          .clip(CircleShape)
          .background(iconColor.copy(alpha = 0.15f)),
        contentAlignment = Alignment.Center
      ) {
        Icon(
          imageVector = icon,
          contentDescription = null,
          tint = iconColor,
          modifier = Modifier.size(18.dp)
        )
      }

      Spacer(modifier = Modifier.width(12.dp))

      Column(modifier = Modifier.padding(end = 8.dp)) {
        Text(
          text = title,
          style = MaterialTheme.typography.bodyMedium.copy(fontWeight = FontWeight.SemiBold),
          color = TextPrimary
        )
        Text(
          text = subtitle,
          style = MaterialTheme.typography.bodySmall,
          color = TextSecondary,
          fontSize = 11.sp,
          lineHeight = 14.sp
        )
      }
    }

    Switch(
      checked = checked,
      onCheckedChange = onCheckedChange,
      modifier = Modifier.testTag(testTag),
      colors = SwitchDefaults.colors(
        checkedThumbColor = Color.White,
        checkedTrackColor = CyberCyan,
        uncheckedThumbColor = TextMuted,
        uncheckedTrackColor = SpaceNavy600
      )
    )
  }
}
