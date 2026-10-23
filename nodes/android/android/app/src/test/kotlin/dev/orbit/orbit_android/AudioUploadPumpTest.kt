package dev.orbit.orbit_android

import org.junit.Assert.*
import org.junit.Test
import java.nio.file.Files
import java.io.File
import java.util.UUID

class AudioUploadPumpTest {
    @Test fun drainsMoreThanFiveHundredWithoutCoreAcksOrTimerAndRespectsLiveLimit() {
        val directory = Files.createTempDirectory("orbit-upload").toFile()
        try {
            val box = AudioOutbox(directory)
            repeat(520) { box.put(UUID.randomUUID().toString(), byteArrayOf(1)) }
            val callbacks = linkedMapOf<String, (Boolean) -> Unit>()
            var limit = 16
            var refill = false
            val pump = AudioUploadPump(box, { limit }, { id, _, done ->
                assertNull(callbacks.put(id, done))
            }, { refill = true }, { 0L })
            pump.pump()
            assertEquals(16, callbacks.size)
            assertEquals(520, box.count()) // Merely starting publish is not success.
            limit = 3 // Existing sends finish; no new ones exceed the smaller window.
            var sent = 0
            while (callbacks.isNotEmpty()) {
                val id = callbacks.keys.first()
                callbacks.remove(id)!!(true)
                sent++
                assertTrue(refill)
                refill = false
                val before = callbacks.size
                pump.pump() // Immediate completion-driven dispatch, no time advanced.
                assertTrue(callbacks.size <= maxOf(limit, before))
            }
            assertEquals(520, sent)
            assertEquals(0, box.count())
        } finally { directory.deleteRecursively() }
    }

    @Test fun newSpeechGoesBeforeBacklogAndFailuresDoNotBlockOtherSegments() {
        val directory = Files.createTempDirectory("orbit-upload").toFile()
        try {
            val box = AudioOutbox(directory)
            val old = UUID.randomUUID().toString()
            val fresh = UUID.randomUUID().toString()
            box.put(old, byteArrayOf(1))
            File(directory, "$old.json").setLastModified(1000)
            var now = 0L
            val callbacks = linkedMapOf<String, (Boolean) -> Unit>()
            val pump = AudioUploadPump(box, { 1 }, { id, _, done -> callbacks[id] = done }, {}, { now })
            box.put(fresh, byteArrayOf(2))
            pump.pump()
            assertEquals(fresh, callbacks.keys.single())
            callbacks.remove(fresh)!!(false)
            assertEquals(2, box.count())
            pump.pump()
            assertEquals(old, callbacks.keys.single())
            callbacks.remove(old)!!(true)
            pump.pump()
            assertTrue(callbacks.isEmpty())
            now = 2000
            pump.pump()
            callbacks.remove(fresh)!!(true)
            assertEquals(0, box.count())
        } finally { directory.deleteRecursively() }
    }

    @Test fun disconnectPreservesInFlightAndLateCallbacksCannotDeleteReopenedQueue() {
        val directory = Files.createTempDirectory("orbit-upload").toFile()
        try {
            val box = AudioOutbox(directory)
            val id = UUID.randomUUID().toString()
            box.put(id, byteArrayOf(1))
            var done: ((Boolean) -> Unit)? = null
            var now = 0L
            val pump = AudioUploadPump(box, { 1 }, { _, _, complete -> done = complete }, {}, { now })
            pump.pump()
            assertFalse(pump.timedOut)
            now = 15000
            assertTrue(pump.timedOut)
            pump.close()
            done!!(true)
            assertEquals(1, AudioOutbox(directory).count())
        } finally { directory.deleteRecursively() }
    }

    @Test fun synchronousSendExceptionRetainsSegmentForRetry() {
        val directory = Files.createTempDirectory("orbit-upload").toFile()
        try {
            val box = AudioOutbox(directory)
            box.put(UUID.randomUUID().toString(), byteArrayOf(1))
            val pump = AudioUploadPump(box, { 64 }, { _, _, _ -> error("offline") }, {}, { 0L })
            pump.pump()
            assertEquals(0, pump.inFlight)
            assertEquals(1, box.count())
        } finally { directory.deleteRecursively() }
    }
}
