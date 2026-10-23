package dev.orbit.orbit_android

/** A start requested while an old segment is flushing is retried only while visible. */
class AudioStartPolicy {
    private var deferred = false
    @Synchronized fun request(active: Boolean, draining: Boolean, visible: Boolean, enabled: Boolean): Boolean {
        if (!enabled) { deferred = false; return false }
        if (active) return false
        if (draining) { deferred = true; return false }
        deferred = false
        return visible
    }
    @Synchronized fun drained(visible: Boolean, enabled: Boolean): Boolean {
        val restart = deferred && visible && enabled
        deferred = false
        return restart
    }
}

object AudioConfigurationPolicy {
    fun legacyAllowed(unifiedApp: Boolean, hasAppConnection: Boolean): Boolean =
        !unifiedApp && !hasAppConnection
}
