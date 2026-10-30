package dev.orbit.orbit_android

/** All calls run on one executor; MQTT completions must be posted back to it. */
class AudioUploadPump(
    private val outbox: AudioOutbox,
    private val concurrency: () -> Int,
    private val send: (String, ByteArray, (Boolean) -> Unit) -> Unit,
    private val changed: () -> Unit,
    private val now: () -> Long,
    private val sent: (Int) -> Unit = {},
) {
    private val sending = mutableMapOf<String, Long>()
    private val retryAt = mutableMapOf<String, Long>()
    private var closed = false
    val inFlight: Int get() = sending.size
    val retrying: Boolean get() = retryAt.isNotEmpty()
    val timedOut: Boolean get() = sending.values.any { now() - it >= 15000 }

    fun pump() {
        if (closed) return
        val room = (concurrency() - sending.size).coerceAtLeast(0)
        if (room == 0) return
        retryAt.entries.removeAll { it.value <= now() }
        val batch = outbox.pending(room, sending.keys + retryAt.keys)
        // Reserve the entire window before invoking any potentially synchronous callbacks.
        batch.forEach { (id, _) -> sending[id] = now() }
        batch.forEach { (id, payload) ->
            try { send(id, payload) { success -> complete(id, success, payload.size) } }
            catch (_: Exception) { complete(id, false, payload.size) }
        }
    }

    private fun complete(id: String, success: Boolean, bytes: Int) {
        if (closed || sending.remove(id) == null) return
        if (success) {
            try {
                outbox.sent(id)
                sent(bytes)
            }
            catch (_: Exception) { retryAt[id] = now() + 2000 }
        } else retryAt[id] = now() + 2000
        changed() // The owner queues another pump immediately, without a batch timer.
    }

    fun close() { closed = true }
}
