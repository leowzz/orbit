package dev.orbit.orbit_android

import org.junit.Assert.*
import org.junit.Test

class AudioSegmenterSettingsTest {
    private class Stream(settings: AudioCaptureSettings = AudioCaptureSettings()) {
        val output = mutableListOf<List<AudioFrame>>()
        val segmenter = AudioSegmenter({ output.add(it) }, settings)
        var index = 0
        fun feed(count: Int, speech: Boolean) {
            repeat(count) { segmenter.accept(AudioFrame(ShortArray(320), index++ * 20L, speech)) }
        }
    }
    @Test fun belowThresholdIsDiscardedButExactThresholdFlushesWithAllCandidateFrames() {
        val short = Stream()
        short.feed(14, true)
        short.segmenter.flush()
        assertTrue(short.output.isEmpty())
        val exact = Stream()
        exact.feed(15, true)
        assertTrue(exact.output.isEmpty())
        exact.segmenter.flush()
        assertEquals((0..14).map { it * 20L }, exact.output.single().map { it.startedAt })
    }
    @Test fun interruptedSpeechNeverAccumulatesAcrossNonSpeechFrames() {
        val stream = Stream()
        repeat(4) { stream.feed(10, true); stream.feed(1, false) }
        stream.feed(30, false)
        stream.segmenter.flush()
        assertTrue(stream.output.isEmpty())
    }
    @Test fun thresholdRetainsPreRollAndEntireSpeechOnset() {
        val stream = Stream()
        stream.feed(50, false)
        stream.feed(15, true)
        stream.feed(30, false)
        assertEquals((35..94).map { it * 20L }, stream.output.single().map { it.startedAt })
        assertEquals(15, stream.output.single().count { it.speech })
    }
    @Test fun hardCutsKeepConfirmedUtteranceAndLastShortPiece() {
        val stream = Stream(AudioCaptureSettings(maxSegmentMs = 3000))
        stream.feed(301, true)
        stream.feed(30, false)
        assertEquals(listOf(150, 150, 31), stream.output.map { it.size })
        assertEquals((0..330).map { it * 20L }, stream.output.flatten().map { it.startedAt })
        stream.segmenter.flush()
        assertEquals(3, stream.output.size)
    }
    @Test fun preRollSurvivesAnUtteranceEndingJustAfterHardCut() {
        val stream = Stream(AudioCaptureSettings(maxSegmentMs = 3000))
        stream.feed(121, true)
        stream.feed(30, false) // 29 tail frames are in the first hard-cut segment.
        assertEquals(150, stream.output.single().size)
        stream.feed(15, true)
        stream.segmenter.flush()
        assertEquals((136..165).map { it * 20L }, stream.output.last().map { it.startedAt })
        assertEquals(15, stream.output.last().count { it.speech })
    }
    @Test fun stopFlushPreservesShortContinuationButNeverUploadsPureSilence() {
        val stream = Stream(AudioCaptureSettings(maxSegmentMs = 3000))
        stream.feed(151, true)
        stream.segmenter.flush()
        assertEquals(listOf(150, 1), stream.output.map { it.size })
        val silent = Stream()
        silent.feed(1000, false)
        silent.segmenter.flush()
        assertTrue(silent.output.isEmpty())
    }
    @Test fun idleSettingsApplyWithoutWaitingAndTrimPreRoll() {
        val stream = Stream()
        stream.feed(20, false)
        val next = AudioCaptureSettings(minSpeechMs = 100, preRollMs = 0, vadMode = 1)
        stream.segmenter.updateSettings(next)
        assertEquals(next, stream.segmenter.settings)
        stream.feed(5, true)
        stream.segmenter.flush()
        assertEquals((20..24).map { it * 20L }, stream.output.single().map { it.startedAt })
    }
    @Test fun settingsDuringCandidateWaitRatherThanDroppingOrRegatingItsSpeech() {
        val stream = Stream()
        stream.feed(10, true)
        val next = AudioCaptureSettings(minSpeechMs = 2000)
        stream.segmenter.updateSettings(next)
        assertNotEquals(next, stream.segmenter.settings)
        stream.feed(5, true) // Reaches the original threshold; candidate remains intact.
        stream.segmenter.flush()
        assertEquals(15, stream.output.single().size)
        assertEquals(next, stream.segmenter.settings)
    }
    @Test fun rejectedCandidateAllowsPendingSettingsAtItsInterruption() {
        val stream = Stream()
        stream.feed(10, true)
        val next = AudioCaptureSettings(minSpeechMs = 100, preRollMs = 0)
        stream.segmenter.updateSettings(next)
        stream.feed(1, false)
        assertEquals(next, stream.segmenter.settings)
        stream.feed(5, true)
        stream.segmenter.flush()
        assertEquals((11..15).map { it * 20L }, stream.output.single().map { it.startedAt })
    }
    @Test fun hotSettingsApplyAtHardBoundaryWithoutRegatingAlreadyConfirmedTail() {
        val stream = Stream(AudioCaptureSettings(maxSegmentMs = 3000))
        stream.feed(140, true)
        val next = AudioCaptureSettings(minSpeechMs = 2000, silenceMs = 200, preRollMs = 0, maxSegmentMs = 4000, vadMode = 3)
        stream.segmenter.updateSettings(next)
        assertNotEquals(next, stream.segmenter.settings)
        stream.feed(10, true)
        assertEquals(next, stream.segmenter.settings)
        stream.feed(1, true)
        stream.feed(10, false)
        assertEquals(listOf(150, 11), stream.output.map { it.size })
        assertEquals((0..160).map { it * 20L }, stream.output.flatten().map { it.startedAt })
        stream.feed(99, true) // New utterances use the new 2000 ms threshold.
        stream.segmenter.flush()
        assertEquals(2, stream.output.size)
    }
    @Test fun silenceEndUsesOldThresholdThenAppliesPendingSettings() {
        val stream = Stream()
        stream.feed(15, true)
        stream.feed(10, false)
        val next = AudioCaptureSettings(silenceMs = 200)
        stream.segmenter.updateSettings(next)
        stream.feed(19, false)
        assertTrue(stream.output.isEmpty())
        assertNotEquals(next, stream.segmenter.settings)
        stream.feed(1, false)
        assertEquals(45, stream.output.single().size)
        assertEquals(next, stream.segmenter.settings)
    }
    @Test fun latestSettingWinsAndRestoringCurrentValueCancelsPendingChange() {
        val stream = Stream(AudioCaptureSettings(maxSegmentMs = 3000))
        stream.feed(20, true)
        val current = stream.segmenter.settings
        stream.segmenter.updateSettings(current.copy(vadMode = 3))
        stream.segmenter.updateSettings(current.copy(vadMode = 0))
        stream.segmenter.updateSettings(current)
        stream.feed(130, true)
        assertEquals(current, stream.segmenter.settings)
    }
}
