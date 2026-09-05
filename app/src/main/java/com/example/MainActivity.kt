package com.example

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.lifecycle.viewmodel.compose.viewModel
import com.example.ui.VpnMainScreen
import com.example.ui.theme.AuraVpnTheme
import com.example.viewmodel.VpnViewModel

class MainActivity : ComponentActivity() {
  override fun onCreate(savedInstanceState: Bundle?) {
    super.onCreate(savedInstanceState)
    enableEdgeToEdge()
    setContent {
      AuraVpnTheme {
        val viewModel: VpnViewModel = viewModel()
        VpnMainScreen(viewModel = viewModel)
      }
    }
  }
}

