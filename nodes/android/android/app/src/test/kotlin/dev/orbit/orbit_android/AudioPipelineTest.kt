package dev.orbit.orbit_android

import org.junit.Assert.*
import org.junit.Test
import java.nio.file.Files
import java.util.UUID
import io.github.jaredmdobson.concentus.*

class AudioPipelineTest {
    @Test fun unifiedLogoutNeverFallsBackToAnOldLegacyIdentity() {
        assertTrue(AudioConfigurationPolicy.legacyAllowed(false, false))
        assertFalse(AudioConfigurationPolicy.legacyAllowed(false, true))
        assertFalse(AudioConfigurationPolicy.legacyAllowed(true, true))
        assertFalse(AudioConfigurationPolicy.legacyAllowed(true, false))
    }
    @Test fun connectionSwitchRestartsAfterFlushOnlyWhileForegroundAndEnabled() {
        val policy = AudioStartPolicy()
        assertFalse(policy.request(active = false, draining = true, visible = true, enabled = true))
        assertTrue(policy.drained(visible = true, enabled = true))
        assertFalse(policy.drained(visible = true, enabled = true))
        assertFalse(policy.request(active = false, draining = true, visible = true, enabled = true))
        assertFalse(policy.drained(visible = false, enabled = true))
        assertTrue(policy.request(active = false, draining = false, visible = true, enabled = true))
        assertFalse(policy.request(active = false, draining = true, visible = true, enabled = true))
        assertFalse(policy.drained(visible = true, enabled = false))
    }
    private fun frame(index: Int, speech: Boolean) = AudioFrame(ShortArray(320), index * 20L, speech)
    @Test fun silenceIsNotEmittedAndSpeechHasPreRollAndTail() {
        val emitted = mutableListOf<List<AudioFrame>>()
        val segmenter = AudioSegmenter({ emitted.add(it) })
        repeat(100) { segmenter.accept(frame(it, false)) }
        assertTrue(emitted.isEmpty())
        repeat(5) { segmenter.accept(frame(100 + it, true)) }
        repeat(30) { segmenter.accept(frame(105 + it, false)) }
        assertEquals(1, emitted.size)
        assertEquals(50, emitted[0].size)
        assertEquals(1700L, emitted[0].first().startedAt)
        assertEquals(5, emitted[0].count { it.speech })
        segmenter.flush()
        assertEquals(1, emitted.size)
    }
    @Test fun continuousSpeechSplitsWithoutDroppingBoundaryFrame() {
        val emitted = mutableListOf<List<AudioFrame>>()
        val segmenter = AudioSegmenter({ emitted.add(it) })
        repeat(1001) { segmenter.accept(frame(it, true)) }
        segmenter.flush()
        assertEquals(listOf(500, 500, 1), emitted.map { it.size })
        assertEquals((0..1000).map { it * 20L }, emitted.flatten().map { it.startedAt })
    }
    @Test fun outboxSurvivesReopenAndOnlyAckRemovesPayload() {
        val root = Files.createTempDirectory("orbit-audio-test").toFile()
        try {
            val id = UUID.randomUUID().toString()
            val payload = "immutable segment".toByteArray()
            AudioOutbox(root).put(id, payload)
            val reopened = AudioOutbox(root)
            assertArrayEquals(payload, reopened.pending().single().second)
            assertEquals(1, reopened.count()) // A publish/PUBACK must not delete it.
            reopened.put(id, payload)
            assertEquals(1, reopened.count())
            reopened.acknowledge(UUID.randomUUID().toString())
            assertEquals(1, reopened.count())
            reopened.acknowledge(id)
            assertEquals(0, AudioOutbox(root).count())
        } finally { root.deleteRecursively() }
    }
    @Test fun fullOutboxPreservesExistingRecordingAndRejectsNewOne() {
        val root = Files.createTempDirectory("orbit-audio-test").toFile()
        try {
            val box = AudioOutbox(root, maxBytes = 4, maxEntries = 2)
            val id = UUID.randomUUID().toString()
            box.put(id, byteArrayOf(1, 2, 3))
            assertThrows(IllegalStateException::class.java) { box.put(UUID.randomUUID().toString(), byteArrayOf(4, 5)) }
            assertEquals(1, box.count())
            assertArrayEquals(byteArrayOf(1, 2, 3), box.pending().single().second)
            assertThrows(IllegalStateException::class.java) { box.put(id, byteArrayOf(4)) }
        } finally { root.deleteRecursively() }
    }
    @Test fun queueBudgetIncludesRetainedRecordingsFromOtherConnections() {
        val root = Files.createTempDirectory("orbit-audio-budget").toFile()
        try {
            val old = AudioOutbox(java.io.File(root, "old"), maxBytes = 4, budgetDirectory = root)
            old.put(UUID.randomUUID().toString(), byteArrayOf(1, 2, 3))
            val current = AudioOutbox(java.io.File(root, "current"), maxBytes = 4, budgetDirectory = root)
            assertThrows(IllegalStateException::class.java) { current.put(UUID.randomUUID().toString(), byteArrayOf(4, 5)) }
            assertEquals(1, old.count())
            assertEquals(0, current.count())
        } finally { root.deleteRecursively() }
    }
    @Test fun encoderProducesDecodableTwentyMillisecondPacketsWithFreshSegmentState() {
        repeat(2) {
            val encoder = OpusEncoder(16000, 1, OpusApplication.OPUS_APPLICATION_VOIP).apply {
                bitrate = 24000; useVBR = false
            }
            val decoder = OpusDecoder(16000, 1)
            repeat(5) {
                val output = ByteArray(1275)
                val size = encoder.encode(ShortArray(320), 0, 320, output, 0, output.size)
                assertTrue(size in 1..1275)
                assertEquals(320, decoder.decode(output, 0, size, ShortArray(320), 0, 320, false))
            }
        }
    }
}
