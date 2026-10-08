package dev.orbit.orbit_android

import kotlin.math.ceil

object WidgetSizing {
    data class Session(val padding: Int, val textSize: Float, val rows: Int)
    fun session(width: Float, height: Float, fontScale: Float): Session {
        val padding = if (width < 180) 10 else 14
        val text = when { width < 180 -> 10f; width >= 280 && height >= 180 -> 14f; else -> 12f }
        // Reserve the header, margin, and full line boxes, including accessibility font scaling.
        val line = ceil(text * fontScale.coerceAtLeast(1f) * 1.5f).toInt()
        val available = height - padding * 2 - line - 6
        return Session(padding, text, (available / (line + 8)).toInt().coerceIn(1, 20))
    }
}
