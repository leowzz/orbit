package dev.orbit.orbit_android

import org.junit.Assert.*
import org.junit.Test

class AudioCaptureSettingsTest {
    @Test fun defaultsAndInclusiveLimitsRoundTrip() {
        val defaults = AudioCaptureSettings()
        assertEquals(300, defaults.minSpeechMs)
        assertEquals(600, defaults.silenceMs)
        assertEquals(300, defaults.preRollMs)
        assertEquals(10000, defaults.maxSegmentMs)
        assertEquals(2, defaults.vadMode)
        assertEquals(16, defaults.uploadConcurrency)
        assertEquals(defaults, AudioCaptureSettings.fromMap(defaults.map()))
        assertEquals(defaults, AudioCaptureSettings.fromMap(defaults.map().mapValues { it.value.toLong() }))
        for (value in listOf(AudioCaptureSettings(100, 200, 0, 3000, 0), AudioCaptureSettings(2000, 2000, 1000, 10000, 3))) {
            assertEquals(value, AudioCaptureSettings.fromMap(value.map()))
        }
    }
    @Test fun channelRejectsMissingExtraNonIntegerAndOutOfRangeValues() {
        val defaults = AudioCaptureSettings().map()
        for (value in listOf(null, "not a map", defaults - "vadMode", defaults + ("other" to 1))) {
            assertThrows(IllegalArgumentException::class.java) { AudioCaptureSettings.fromMap(value) }
        }
        for (value in listOf(300.0, Float.NaN, Double.POSITIVE_INFINITY, "300", true, Long.MAX_VALUE, null)) {
            assertThrows(IllegalArgumentException::class.java) { AudioCaptureSettings.fromMap(defaults + ("minSpeechMs" to value)) }
        }
        for ((key, value) in listOf("minSpeechMs" to 80, "minSpeechMs" to 2020,
            "silenceMs" to 180, "silenceMs" to 2020, "preRollMs" to -20, "preRollMs" to 1020,
            "maxSegmentMs" to 2980, "maxSegmentMs" to 10020, "vadMode" to -1, "vadMode" to 4,
            "uploadConcurrency" to 0, "uploadConcurrency" to 65,
            "minSpeechMs" to 301, "silenceMs" to 601, "preRollMs" to 1, "maxSegmentMs" to 3001)) {
            assertThrows(IllegalArgumentException::class.java) { AudioCaptureSettings.fromMap(defaults + (key to value)) }
        }
    }
    @Test fun storePersistsAcrossInstancesAndMergesMissingDefaults() {
        var persisted: Map<String, *> = mapOf("minSpeechMs" to 500)
        fun store() = AudioCaptureSettingsStore({ persisted }, { persisted = it.toMap(); true })
        assertEquals(AudioCaptureSettings(minSpeechMs = 500), store().load())
        val value = AudioCaptureSettings(1000, 1000, 500, 5000, 3, 64)
        assertEquals(value, store().save(value))
        assertEquals(value, store().load())
        assertEquals(value.map(), persisted)
    }
    @Test fun failedCommitIsReportedAndDoesNotClaimSavedSettings() {
        val original = AudioCaptureSettings().map()
        val store = AudioCaptureSettingsStore({ original }, { false })
        assertThrows(IllegalStateException::class.java) { store.save(AudioCaptureSettings(minSpeechMs = 500)) }
        assertEquals(AudioCaptureSettings(), store.load())
    }
}
