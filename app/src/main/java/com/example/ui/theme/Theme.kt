package com.example.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

private val DarkColorScheme =
  darkColorScheme(
    primary = CyberCyan,
    onPrimary = SpaceNavy900,
    primaryContainer = SpaceNavy700,
    onPrimaryContainer = CyberCyanLight,
    secondary = CyberEmerald,
    onSecondary = SpaceNavy900,
    secondaryContainer = SpaceNavy600,
    onSecondaryContainer = CyberEmeraldLight,
    tertiary = CyberAmber,
    onTertiary = SpaceNavy900,
    background = SpaceNavy900,
    onBackground = TextPrimary,
    surface = SpaceNavy800,
    onSurface = TextPrimary,
    surfaceVariant = SpaceNavy700,
    onSurfaceVariant = TextSecondary,
    outline = SlateBorder,
    outlineVariant = SlateBorderLight,
    error = CyberRed,
    onError = Color.White,
  )

private val LightColorScheme =
  lightColorScheme(
    primary = CyberCyanDark,
    onPrimary = Color.White,
    primaryContainer = Color(0xFFE0F7FA),
    onPrimaryContainer = Color(0xFF006064),
    secondary = CyberEmeraldDark,
    onSecondary = Color.White,
    secondaryContainer = Color(0xFFE8F5E9),
    onSecondaryContainer = Color(0xFF1B5E20),
    tertiary = CyberAmberDark,
    onTertiary = Color.White,
    background = Color(0xFFF8FAFC),
    onBackground = Color(0xFF0F172A),
    surface = Color(0xFFFFFFFF),
    onSurface = Color(0xFF0F172A),
    surfaceVariant = Color(0xFFF1F5F9),
    onSurfaceVariant = Color(0xFF475569),
    outline = Color(0xFFCBD5E1),
    outlineVariant = Color(0xFFE2E8F0),
    error = Color(0xFFD32F2F),
    onError = Color.White,
  )

@Composable
fun AuraVpnTheme(
  darkTheme: Boolean = true, // Default to sleek cyber dark mode for VPN UI
  content: @Composable () -> Unit,
) {
  val colorScheme = if (darkTheme) DarkColorScheme else LightColorScheme

  MaterialTheme(colorScheme = colorScheme, typography = Typography, content = content)
}

