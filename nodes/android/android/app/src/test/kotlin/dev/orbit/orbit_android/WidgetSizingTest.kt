package dev.orbit.orbit_android

import org.junit.Assert.*
import org.junit.Test

class WidgetSizingTest {
    @Test fun tallerWidgetsShowMoreRowsAndNarrowWidgetsUseSmallerText() {
        val small = WidgetSizing.session(110f, 110f, 1f)
        val normal = WidgetSizing.session(280f, 180f, 1f)
        val tall = WidgetSizing.session(280f, 400f, 1f)
        assertTrue(small.rows >= 1)
        assertTrue(small.textSize < normal.textSize)
        assertTrue(tall.rows > normal.rows)
        assertTrue(tall.rows > 6)
        assertEquals(20, WidgetSizing.session(400f, 2000f, 1f).rows)
    }
    @Test fun accessibilityFontScaleReducesRowsToAvoidClipping() {
        val normal = WidgetSizing.session(280f, 180f, 1f)
        val largeText = WidgetSizing.session(280f, 180f, 2f)
        assertTrue(largeText.rows < normal.rows)
    }
}
