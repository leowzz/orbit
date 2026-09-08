package dev.orbit.orbit_android

import com.google.protobuf.Timestamp
import orbit.v1.View.CodexSessionView
import org.junit.Assert.*
import org.junit.Test

class WidgetPresentationTest {
    @Test fun compactCostTruncatesWithoutFloatingPointRounding() {
        assertEquals("$12.34", WidgetPresentation.cost(12349999, "USD"))
        assertEquals("$12", WidgetPresentation.cost(12000000, "USD"))
        assertEquals("$0", WidgetPresentation.cost(0, "USD"))
        assertEquals("<$0.01", WidgetPresentation.cost(9999, "USD"))
        assertEquals("$0.01", WidgetPresentation.cost(10000, "USD"))
        assertEquals("€1.23", WidgetPresentation.cost(1234567, "EUR"))
    }
    @Test fun runningSessionsComeFirstThenRecentSessions() {
        fun session(id: String, status: Int, seconds: Long) = CodexSessionView.newBuilder()
            .setSessionId(id).setStatusValue(status).setUpdatedAt(Timestamp.newBuilder().setSeconds(seconds)).build()
        val sessions = listOf(session("old", 3, 1), session("failed", 4, 4), session("running", 2, 2))
        assertEquals(listOf("running", "failed"), WidgetPresentation.sessions(sessions, 2).map { it.sessionId })
        assertEquals("失败", WidgetPresentation.status(4).first)
    }
}
