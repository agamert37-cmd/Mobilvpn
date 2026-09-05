package com.example.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.outlined.StarBorder
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.SheetState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.example.model.ServerCategory
import com.example.model.VpnServer
import com.example.ui.theme.CardBackgroundElevated
import com.example.ui.theme.CyberAmber
import com.example.ui.theme.CyberCyan
import com.example.ui.theme.CyberEmerald
import com.example.ui.theme.CyberRed
import com.example.ui.theme.SlateBorder
import com.example.ui.theme.SpaceNavy600
import com.example.ui.theme.SpaceNavy700
import com.example.ui.theme.SpaceNavy800
import com.example.ui.theme.SpaceNavy900
import com.example.ui.theme.TextMuted
import com.example.ui.theme.TextPrimary
import com.example.ui.theme.TextSecondary

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ServerSelectionBottomSheet(
  servers: List<VpnServer>,
  selectedServer: VpnServer,
  sheetState: SheetState,
  onDismiss: () -> Unit,
  onSelectServer: (VpnServer) -> Unit,
  onToggleFavorite: (String) -> Unit,
  modifier: Modifier = Modifier
) {
  var searchQuery by remember { mutableStateOf("") }
  var selectedCategory by remember { mutableStateOf<ServerCategory?>(null) }

  val filteredServers = remember(servers, searchQuery, selectedCategory) {
    servers.filter { server ->
      val matchesSearch = server.country.contains(searchQuery, ignoreCase = true) ||
          server.city.contains(searchQuery, ignoreCase = true)
      val matchesCategory = selectedCategory == null ||
          selectedCategory == ServerCategory.ALL ||
          server.categories.contains(selectedCategory)
      matchesSearch && matchesCategory
    }
  }

  ModalBottomSheet(
    onDismissRequest = onDismiss,
    sheetState = sheetState,
    containerColor = SpaceNavy800,
    contentColor = TextPrimary,
    dragHandle = {
      Box(
        modifier = Modifier
          .padding(vertical = 12.dp)
          .size(width = 40.dp, height = 4.dp)
          .clip(CircleShape)
          .background(SpaceNavy600)
      )
    },
    modifier = modifier.testTag("server_selection_bottom_sheet")
  ) {
    Column(
      modifier = Modifier
        .fillMaxWidth()
        .fillMaxHeight(0.85f)
        .padding(horizontal = 20.dp)
    ) {
      // Sheet Title
      Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically
      ) {
        Column {
          Text(
            text = "Sunucu Konumları",
            style = MaterialTheme.typography.titleLarge.copy(fontWeight = FontWeight.Bold),
            color = TextPrimary
          )
          Text(
            text = "${servers.size} Güvenli Konum Aktif",
            style = MaterialTheme.typography.bodySmall,
            color = TextSecondary
          )
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

      // Search Bar
      OutlinedTextField(
        value = searchQuery,
        onValueChange = { searchQuery = it },
        modifier = Modifier
          .fillMaxWidth()
          .testTag("server_search_input"),
        placeholder = { Text("Ülke veya şehir ara...", color = TextMuted) },
        leadingIcon = {
          Icon(
            imageVector = Icons.Default.Search,
            contentDescription = null,
            tint = CyberCyan
          )
        },
        trailingIcon = {
          if (searchQuery.isNotEmpty()) {
            IconButton(onClick = { searchQuery = "" }) {
              Icon(
                imageVector = Icons.Default.Close,
                contentDescription = "Temizle",
                tint = TextSecondary,
                modifier = Modifier.size(18.dp)
              )
            }
          }
        },
        singleLine = true,
        shape = RoundedCornerShape(14.dp),
        colors = OutlinedTextFieldDefaults.colors(
          focusedContainerColor = SpaceNavy900,
          unfocusedContainerColor = SpaceNavy900,
          focusedBorderColor = CyberCyan,
          unfocusedBorderColor = SlateBorder,
          focusedTextColor = TextPrimary,
          unfocusedTextColor = TextPrimary
        )
      )

      Spacer(modifier = Modifier.height(12.dp))

      // Category Chips
      LazyRow(
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        contentPadding = PaddingValues(horizontal = 2.dp)
      ) {
        val categories = listOf(
          ServerCategory.ALL,
          ServerCategory.FASTEST,
          ServerCategory.STREAMING,
          ServerCategory.P2P
        )

        items(categories) { category ->
          val isSelected = (selectedCategory == null && category == ServerCategory.ALL) ||
              selectedCategory == category

          FilterChip(
            selected = isSelected,
            onClick = {
              selectedCategory = if (category == ServerCategory.ALL) null else category
            },
            label = { Text(category.title, fontSize = 12.sp) },
            shape = RoundedCornerShape(10.dp),
            colors = FilterChipDefaults.filterChipColors(
              selectedContainerColor = CyberCyan.copy(alpha = 0.15f),
              selectedLabelColor = CyberCyan,
              containerColor = SpaceNavy700,
              labelColor = TextSecondary
            ),
            border = FilterChipDefaults.filterChipBorder(
              enabled = true,
              selected = isSelected,
              borderColor = SlateBorder,
              selectedBorderColor = CyberCyan
            )
          )
        }
      }

      Spacer(modifier = Modifier.height(14.dp))

      // Server List
      LazyColumn(
        verticalArrangement = Arrangement.spacedBy(10.dp),
        contentPadding = PaddingValues(bottom = 24.dp)
      ) {
        items(filteredServers, key = { it.id }) { server ->
          val isCurrent = server.id == selectedServer.id

          Row(
            modifier = Modifier
              .fillMaxWidth()
              .clip(RoundedCornerShape(14.dp))
              .background(if (isCurrent) SpaceNavy700 else CardBackgroundElevated)
              .border(
                1.dp,
                if (isCurrent) CyberCyan.copy(alpha = 0.7f) else SlateBorder,
                RoundedCornerShape(14.dp)
              )
              .clickable {
                onSelectServer(server)
                onDismiss()
              }
              .padding(14.dp)
              .testTag("server_item_${server.id}"),
            verticalAlignment = Alignment.CenterVertically
          ) {
            // Flag Icon
            Box(
              modifier = Modifier
                .size(40.dp)
                .clip(CircleShape)
                .background(SpaceNavy800),
              contentAlignment = Alignment.Center
            ) {
              Text(
                text = server.flagEmoji,
                fontSize = 20.sp
              )
            }

            Spacer(modifier = Modifier.width(12.dp))

            // Country & City
            Column(modifier = Modifier.weight(1f)) {
              Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                  text = server.country,
                  style = MaterialTheme.typography.bodyLarge.copy(fontWeight = FontWeight.Bold),
                  color = TextPrimary
                )
                if (server.isFastest) {
                  Spacer(modifier = Modifier.width(6.dp))
                  Text(
                    text = "• En Hızlı",
                    style = MaterialTheme.typography.labelSmall.copy(fontWeight = FontWeight.Bold),
                    color = CyberCyan,
                    fontSize = 10.sp
                  )
                }
              }
              Text(
                text = "${server.city}  (Yük: %${server.loadPercent})",
                style = MaterialTheme.typography.bodySmall,
                color = TextMuted
              )
            }

            // Ping badge
            val pingColor = when {
              server.pingMs < 45 -> CyberEmerald
              server.pingMs < 110 -> CyberAmber
              else -> CyberRed
            }

            Box(
              modifier = Modifier
                .clip(RoundedCornerShape(8.dp))
                .background(SpaceNavy800)
                .padding(horizontal = 8.dp, vertical = 4.dp)
            ) {
              Text(
                text = "${server.pingMs} ms",
                style = MaterialTheme.typography.labelSmall.copy(fontWeight = FontWeight.SemiBold),
                color = pingColor,
                fontSize = 11.sp
              )
            }

            Spacer(modifier = Modifier.width(8.dp))

            // Favorite star button
            IconButton(
              onClick = { onToggleFavorite(server.id) },
              modifier = Modifier.size(32.dp)
            ) {
              Icon(
                imageVector = if (server.isFavorite) Icons.Default.Star else Icons.Outlined.StarBorder,
                contentDescription = "Favori",
                tint = if (server.isFavorite) CyberAmber else TextMuted,
                modifier = Modifier.size(20.dp)
              )
            }

            if (isCurrent) {
              Spacer(modifier = Modifier.width(4.dp))
              Icon(
                imageVector = Icons.Default.CheckCircle,
                contentDescription = "Seçili",
                tint = CyberCyan,
                modifier = Modifier.size(20.dp)
              )
            }
          }
        }
      }
    }
  }
}
