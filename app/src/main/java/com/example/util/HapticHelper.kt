package com.example.util

import android.content.Context
import android.os.Build
import android.os.VibrationEffect
import android.os.Vibrator
import android.os.VibratorManager
import androidx.compose.ui.hapticfeedback.HapticFeedback
import androidx.compose.ui.hapticfeedback.HapticFeedbackType

/**
 * Manages tactile and haptic feedback responses for mobile user touches.
 * The mobile app delegates all business logic to the backend server
 * while delivering immediate, crisp tactile feedback to the user.
 */
class HapticHelper(private val context: Context) {

  private val vibrator: Vibrator? by lazy {
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
      val vibratorManager = context.getSystemService(Context.VIBRATOR_MANAGER_SERVICE) as? VibratorManager
      vibratorManager?.defaultVibrator
    } else {
      @Suppress("DEPRECATION")
      context.getSystemService(Context.VIBRATOR_SERVICE) as? Vibrator
    }
  }

  /**
   * Subtle tick for normal button taps and chip selections.
   */
  fun performTick(composeHaptic: HapticFeedback? = null) {
    try {
      composeHaptic?.performHapticFeedback(HapticFeedbackType.TextHandleMove)
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
        vibrator?.vibrate(VibrationEffect.createPredefined(VibrationEffect.EFFECT_TICK))
      } else {
        @Suppress("DEPRECATION")
        vibrator?.vibrate(15)
      }
    } catch (_: Exception) {
      // Graceful ignore if vibration is disabled on device
    }
  }

  /**
   * Firm tactile click for switches and toggles.
   */
  fun performClick(composeHaptic: HapticFeedback? = null) {
    try {
      composeHaptic?.performHapticFeedback(HapticFeedbackType.LongPress)
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
        vibrator?.vibrate(VibrationEffect.createPredefined(VibrationEffect.EFFECT_CLICK))
      } else {
        @Suppress("DEPRECATION")
        vibrator?.vibrate(25)
      }
    } catch (_: Exception) {
    }
  }

  /**
   * Triggered when the user presses the Connect button to dispatch tunnel establishment to the server.
   */
  fun performConnectTrigger(composeHaptic: HapticFeedback? = null) {
    try {
      composeHaptic?.performHapticFeedback(HapticFeedbackType.LongPress)
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
        val timings = longArrayOf(0, 35, 50, 45)
        val amplitudes = intArrayOf(0, 180, 0, 240)
        vibrator?.vibrate(VibrationEffect.createWaveform(timings, amplitudes, -1))
      } else {
        @Suppress("DEPRECATION")
        vibrator?.vibrate(50)
      }
    } catch (_: Exception) {
    }
  }

  /**
   * Triggered when the backend server confirms that the VPN tunnel is successfully encrypted & connected.
   */
  fun performSuccess() {
    try {
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
        vibrator?.vibrate(VibrationEffect.createPredefined(VibrationEffect.EFFECT_HEAVY_CLICK))
      } else if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
        val timings = longArrayOf(0, 40, 60, 80)
        val amplitudes = intArrayOf(0, 120, 0, 255)
        vibrator?.vibrate(VibrationEffect.createWaveform(timings, amplitudes, -1))
      } else {
        @Suppress("DEPRECATION")
        vibrator?.vibrate(70)
      }
    } catch (_: Exception) {
    }
  }

  /**
   * Triggered when disconnecting the VPN tunnel.
   */
  fun performDisconnect() {
    try {
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
        val timings = longArrayOf(0, 30, 40, 25)
        val amplitudes = intArrayOf(0, 160, 0, 100)
        vibrator?.vibrate(VibrationEffect.createWaveform(timings, amplitudes, -1))
      } else {
        @Suppress("DEPRECATION")
        vibrator?.vibrate(30)
      }
    } catch (_: Exception) {
    }
  }
}
