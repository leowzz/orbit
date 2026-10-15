package dev.orbit.orbit_android

import com.google.protobuf.Timestamp
import orbit.v1.View.CodexSessionView
import org.junit.Assert.*
import org.junit.Test
import kotlin.math.pow

class WidgetPresentationTest {
    @Test fun darkStatusColorsKeepLabelsAndReadableContrast() {
        fun luminance(color: Int): Double {
            fun channel(shift: Int): Double {
                val value = ((color shr shift) and 255) / 255.0
                return if (value <= 0.04045) value / 12.92 else ((value + 0.055) / 1.055).pow(2.4)
            }
            return 0.2126 * channel(16) + 0.7152 * channel(8) + 0.0722 * channel(0)
        }
        val background = luminance(0xFF151917.toInt())
        for (status in 0..6) {
            val (label, color) = WidgetPresentation.status(status, dark = true)
            assertEquals(WidgetPresentation.status(status).first, label)
            assertTrue("Unreadable status $status", (luminance(color) + 0.05) / (background + 0.05) >= 4.5)
        }
    }

    @Test fun compactCostTruncatesWithoutFloatingPointRounding() {
        assertEquals("$12.34", WidgetPresentation.cost(12349999, "USD"))
        assertEquals("$12", WidgetPresentation.cost(12000000, "USD"))
        assertEquals("$0", WidgetPresentation.cost(0, "USD"))
        assertEquals("<$0.01", WidgetPresentation.cost(9999, "USD"))
        assertEquals("$0.01", WidgetPresentation.cost(10000, "USD"))
        assertEquals("€1.23", WidgetPresentation.cost(1234567, "EUR"))
    }
    @Test fun metricsUseCompactUnitsWithoutRoundingUp() {
        assertEquals("999", WidgetPresentation.metric(999))
        assertEquals("1K", WidgetPresentation.metric(1000))
        assertEquals("1.9K", WidgetPresentation.metric(1999))
        assertEquals("12.3M", WidgetPresentation.metric(12399999))
        assertEquals("1B", WidgetPresentation.metric(1000000000))
    }
    @Test fun runningSessionsComeFirstThenRecentSessions() {
        fun session(id: String, status: Int, seconds: Long) = CodexSessionView.newBuilder()
            .setSessionId(id).setStatusValue(status).setUpdatedAt(Timestamp.newBuilder().setSeconds(seconds)).build()
        val sessions = listOf(session("old", 3, 1), session("failed", 4, 4), session("running", 2, 2))
        assertEquals(listOf("running", "failed"), WidgetPresentation.sessions(sessions, 2).map { it.sessionId })
        assertEquals("失败", WidgetPresentation.status(4).first)
    }
}
