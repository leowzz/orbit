package dev.orbit.orbit_android

import kotlin.random.Random

/** Bounded exponential retry; jitter prevents synchronized reconnects. No timer while offline. */
class RetryBackoff(private val jitter: (Long) -> Long = { Random.nextLong(it / 5 + 1) }) {
    private var delay = 15000L
    fun reset() { delay = 15000L }
    fun nextDelayMillis(): Long {
        val result = (delay + jitter(delay)).coerceAtMost(300000L)
        delay = (delay * 2).coerceAtMost(300000L)
        return result
    }
}
