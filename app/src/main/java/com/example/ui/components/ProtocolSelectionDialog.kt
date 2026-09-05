package com.example.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
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
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.VpnKey
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.RadioButtonDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import com.example.model.VpnProtocol
import com.example.ui.theme.CardBackgroundElevated
import com.example.ui.theme.CyberCyan
import com.example.ui.theme.CyberEmerald
import com.example.ui.theme.SlateBorder
import com.example.ui.theme.SpaceNavy700
import com.example.ui.theme.SpaceNavy800
import com.example.ui.theme.TextMuted
import com.example.ui.theme.TextPrimary
import com.example.ui.theme.TextSecondary

@Composable
fun ProtocolSelectionDialog(
  selectedProtocol: VpnProtocol,
  onSelectProtocol: (VpnProtocol) -> Unit,
  onDismiss: () -> Unit
) {
  Dialog(onDismissRequest = onDismiss) {
    Card(
      modifier = Modifier
        .fillMaxWidth()
        .clip(RoundedCornerShape(24.dp))
        .border(1.dp, SlateBorder, RoundedCornerShape(24.dp))
        .testTag("protocol_dialog"),
      colors = CardDefaults.cardColors(containerColor = SpaceNavy800)
    ) {
      Column(
        modifier = Modifier
          .fillMaxWidth()
          .padding(20.dp)
      ) {
        Row(
          modifier = Modifier.fillMaxWidth(),
          horizontalArrangement = Arrangement.SpaceBetween,
          verticalAlignment = Alignment.CenterVertically
        ) {
          Row(verticalAlignment = Alignment.CenterVertically) {
            Box(
              modifier = Modifier
                .size(36.dp)
                .clip(CircleShape)
                .background(CyberCyan.copy(alpha = 0.15f)),
              contentAlignment = Alignment.Center
            ) {
              Icon(
                imageVector = Icons.Default.VpnKey,
                contentDescription = null,
                tint = CyberCyan,
                modifier = Modifier.size(18.dp)
              )
            }
            Spacer(modifier = Modifier.width(10.dp))
            Column {
              Text(
                text = "Tünel Protokolü",
                style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.Bold),
                color = TextPrimary
              )
              Text(
                text = "Şifreleme ve Bağlantı Türü",
                style = MaterialTheme.typography.bodySmall,
                color = TextSecondary
              )
            }
          }

          IconButton(onClick = onDismiss) {
            Icon(
              imageVector = Icons.Default.Close,
              contentDescription = "Kapat",
              tint = TextSecondary
            )
          }
        }

        Spacer(modifier = Modifier.height(16.dp))

        Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
          VpnProtocol.entries.forEach { protocol ->
            val isSelected = protocol == selectedProtocol

            Row(
              modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(14.dp))
                .background(if (isSelected) SpaceNavy700 else CardBackgroundElevated)
                .border(
                  1.dp,
                  if (isSelected) CyberCyan.copy(alpha = 0.8f) else SlateBorder,
                  RoundedCornerShape(14.dp)
                )
                .clickable {
                  onSelectProtocol(protocol)
                  onDismiss()
                }
                .padding(12.dp)
                .testTag("protocol_item_${protocol.name}"),
              verticalAlignment = Alignment.CenterVertically
            ) {
              RadioButton(
                selected = isSelected,
                onClick = {
                  onSelectProtocol(protocol)
                  onDismiss()
                },
                colors = RadioButtonDefaults.colors(
                  selectedColor = CyberCyan,
                  unselectedColor = TextMuted
                )
              )

              Spacer(modifier = Modifier.width(6.dp))

              Column(modifier = Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                  Text(
                    text = protocol.displayName,
                    style = MaterialTheme.typography.bodyLarge.copy(fontWeight = FontWeight.Bold),
                    color = TextPrimary
                  )
                  Spacer(modifier = Modifier.width(8.dp))
                  Box(
                    modifier = Modifier
                      .clip(RoundedCornerShape(6.dp))
                      .background(CyberCyan.copy(alpha = 0.15f))
                      .padding(horizontal = 6.dp, vertical = 2.dp)
                  ) {
                    Text(
                      text = protocol.badge,
                      style = MaterialTheme.typography.labelSmall.copy(fontWeight = FontWeight.Bold),
                      color = CyberCyan,
                      fontSize = 9.sp
                    )
                  }
                }

                Text(
                  text = protocol.description,
                  style = MaterialTheme.typography.bodySmall,
                  color = TextSecondary,
                  fontSize = 11.sp,
                  lineHeight = 14.sp
                )

                Spacer(modifier = Modifier.height(3.dp))

                Text(
                  text = "Şifreleme: ${protocol.encryption}",
                  style = MaterialTheme.typography.labelSmall,
                  color = CyberEmerald,
                  fontSize = 10.sp
                )
              }
            }
          }
        }
      }
    }
  }
}
