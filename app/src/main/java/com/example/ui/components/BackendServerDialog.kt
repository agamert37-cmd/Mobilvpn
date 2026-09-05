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
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.filled.Hub
import androidx.compose.material.icons.filled.Vibration
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import com.example.ui.theme.CyberCyan
import com.example.ui.theme.CyberEmerald
import com.example.ui.theme.SlateBorder
import com.example.ui.theme.SpaceNavy700
import com.example.ui.theme.SpaceNavy800
import com.example.ui.theme.SpaceNavy900
import com.example.ui.theme.TextMuted
import com.example.ui.theme.TextPrimary
import com.example.ui.theme.TextSecondary

@Composable
fun BackendServerDialog(
  currentUrl: String,
  sessionId: String?,
  onSaveUrl: (String) -> Unit,
  onTestHaptic: () -> Unit,
  onDismiss: () -> Unit
) {
  var inputUrl by remember { mutableStateOf(currentUrl) }

  Dialog(onDismissRequest = onDismiss) {
    Card(
      modifier = Modifier
        .fillMaxWidth()
        .clip(RoundedCornerShape(24.dp))
        .border(1.dp, SlateBorder, RoundedCornerShape(24.dp))
        .testTag("backend_server_dialog"),
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
                .size(38.dp)
                .clip(CircleShape)
                .background(CyberCyan.copy(alpha = 0.15f)),
              contentAlignment = Alignment.Center
            ) {
              Icon(
                imageVector = Icons.Default.Hub,
                contentDescription = null,
                tint = CyberCyan,
                modifier = Modifier.size(20.dp)
              )
            }
            Spacer(modifier = Modifier.width(10.dp))
            Column {
              Text(
                text = "Sunucu Mimarisi",
                style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.Bold),
                color = TextPrimary
              )
              Text(
                text = "GitHub / Uzak Sunucu Yönetimi",
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

        Spacer(modifier = Modifier.height(14.dp))

        // Info Box
        Box(
          modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(SpaceNavy900)
            .border(1.dp, SlateBorder, RoundedCornerShape(12.dp))
            .padding(12.dp)
        ) {
          Column {
            Row(verticalAlignment = Alignment.CenterVertically) {
              Icon(
                imageVector = Icons.Default.CheckCircle,
                contentDescription = null,
                tint = CyberEmerald,
                modifier = Modifier.size(16.dp)
              )
              Spacer(modifier = Modifier.width(6.dp))
              Text(
                text = "İnce İstemci (Thin Client) Modu",
                style = MaterialTheme.typography.labelMedium.copy(fontWeight = FontWeight.Bold),
                color = CyberEmerald
              )
            }
            Spacer(modifier = Modifier.height(4.dp))
            Text(
              text = "Tüm tünelleme, IP atamaları, şifreleme anahtarları ve bant genişliği telemetrisi sunucu tarafında işlenir. Mobil arayüz sadece dokunumsal titreşimleri ve sunucu durumunu yansıtır.",
              style = MaterialTheme.typography.bodySmall,
              color = TextSecondary,
              fontSize = 11.sp,
              lineHeight = 15.sp
            )
            if (sessionId != null) {
              Spacer(modifier = Modifier.height(6.dp))
              Text(
                text = "Aktif Oturum ID: $sessionId",
                style = MaterialTheme.typography.labelSmall,
                color = CyberCyan,
                fontSize = 10.sp
              )
            }
          }
        }

        Spacer(modifier = Modifier.height(14.dp))

        // URL Field
        Text(
          text = "Sunucu API Uç Noktası (Base URL)",
          style = MaterialTheme.typography.labelMedium.copy(fontWeight = FontWeight.SemiBold),
          color = TextPrimary
        )

        Spacer(modifier = Modifier.height(6.dp))

        OutlinedTextField(
          value = inputUrl,
          onValueChange = { inputUrl = it },
          modifier = Modifier
            .fillMaxWidth()
            .testTag("backend_url_input"),
          singleLine = true,
          shape = RoundedCornerShape(12.dp),
          colors = OutlinedTextFieldDefaults.colors(
            focusedBorderColor = CyberCyan,
            unfocusedBorderColor = SlateBorder,
            focusedTextColor = TextPrimary,
            unfocusedTextColor = TextPrimary,
            cursorColor = CyberCyan,
            focusedContainerColor = SpaceNavy700,
            unfocusedContainerColor = SpaceNavy700
          ),
          leadingIcon = {
            Icon(
              imageVector = Icons.Default.Dns,
              contentDescription = null,
              tint = CyberCyan,
              modifier = Modifier.size(18.dp)
            )
          }
        )

        Spacer(modifier = Modifier.height(14.dp))

        // Haptic Test Row
        Row(
          modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(SpaceNavy700)
            .padding(horizontal = 12.dp, vertical = 10.dp),
          horizontalArrangement = Arrangement.SpaceBetween,
          verticalAlignment = Alignment.CenterVertically
        ) {
          Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(
              imageVector = Icons.Default.Vibration,
              contentDescription = null,
              tint = CyberCyan,
              modifier = Modifier.size(18.dp)
            )
            Spacer(modifier = Modifier.width(8.dp))
            Column {
              Text(
                text = "Dokunumsal Bildirim",
                style = MaterialTheme.typography.labelMedium.copy(fontWeight = FontWeight.Bold),
                color = TextPrimary
              )
              Text(
                text = "Haptic titreşim geri bildirimi",
                style = MaterialTheme.typography.bodySmall,
                color = TextMuted,
                fontSize = 10.sp
              )
            }
          }

          Button(
            onClick = onTestHaptic,
            shape = RoundedCornerShape(8.dp),
            colors = ButtonDefaults.buttonColors(containerColor = CyberCyan.copy(alpha = 0.2f)),
            modifier = Modifier.testTag("test_haptic_button")
          ) {
            Text(
              text = "Titreşim Testi",
              color = CyberCyan,
              style = MaterialTheme.typography.labelSmall.copy(fontWeight = FontWeight.Bold)
            )
          }
        }

        Spacer(modifier = Modifier.height(16.dp))

        // Save & Connect Button
        Button(
          onClick = {
            onSaveUrl(inputUrl.trim())
            onDismiss()
          },
          modifier = Modifier
            .fillMaxWidth()
            .height(46.dp)
            .testTag("save_backend_url_button"),
          shape = RoundedCornerShape(12.dp),
          colors = ButtonDefaults.buttonColors(containerColor = CyberCyan)
        ) {
          Text(
            text = "Sunucu Adresini Güncelle",
            color = SpaceNavy900,
            style = MaterialTheme.typography.labelLarge.copy(fontWeight = FontWeight.Bold)
          )
        }
      }
    }
  }
}
