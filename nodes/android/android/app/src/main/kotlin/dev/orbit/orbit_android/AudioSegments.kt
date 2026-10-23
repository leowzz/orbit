package dev.orbit.orbit_android

/** Frames are always 20 ms / 320 signed PCM samples. Called only by the processor. */
data class AudioFrame(val samples: ShortArray, val startedAt: Long, val speech: Boolean)
class AudioSegmenter(
    private val emit: (List<AudioFrame>) -> Unit,
    settings: AudioCaptureSettings = AudioCaptureSettings(),
) {
    var settings: AudioCaptureSettings = settings
        private set
    private var nextSettings: AudioCaptureSettings? = null
    private val preRoll = ArrayDeque<AudioFrame>()
    private val candidate = mutableListOf<AudioFrame>()
    private val active = mutableListOf<AudioFrame>()
    // Remains true across hard segment cuts, even when active is temporarily empty.
    private var confirmed = false
    private var silent = 0

    fun updateSettings(value: AudioCaptureSettings) {
        nextSettings = value.takeIf { it != settings }
        if (!confirmed && candidate.isEmpty()) applySettings()
    }
    private fun applySettings() {
        nextSettings?.let { settings = it }
        nextSettings = null
        trimPreRoll()
    }
    private fun trimPreRoll() {
        while (preRoll.size > settings.preRollMs / 20) preRoll.removeFirst()
    }
    private fun remember(frames: Iterable<AudioFrame>) {
        frames.forEach { preRoll.addLast(it) }
        trimPreRoll()
    }
    fun accept(frame: AudioFrame) {
        require(frame.samples.size == 320)
        if (!confirmed) {
            if (!frame.speech) {
                // A rejected short sound may be context for a later valid utterance,
                // but never contributes to its consecutive-speech threshold.
                remember(candidate)
                candidate.clear()
                remember(listOf(frame))
                applySettings()
                return
            }
            candidate.add(frame)
            if (candidate.size * 20 < settings.minSpeechMs) return
            active.addAll(preRoll)
            preRoll.clear()
            active.addAll(candidate)
            candidate.clear()
            confirmed = true
            silent = 0
        } else {
            active.add(frame)
            silent = if (frame.speech) 0 else silent + 1
        }
        if (silent * 20 >= settings.silenceMs) {
            endUtterance()
        } else if (active.size * 20 >= settings.maxSegmentMs) {
            val context = active.takeLast(50) // Largest supported 1000 ms pre-roll.
            emitActive()
            applySettings()
            remember(context)
            // A shorter new silence threshold may already be satisfied at this cut.
            if (silent * 20 >= settings.silenceMs) endUtterance()
        }
    }
    private fun emitActive() {
        if (active.any { it.speech }) emit(active.toList())
        active.clear()
    }
    private fun endUtterance() {
        // Keep actual sampled context available if speech follows immediately.
        val context = active.takeLast(50)
        emitActive()
        confirmed = false
        silent = 0
        applySettings()
        remember(context)
    }
    fun flush() {
        // A confirmed utterance's last short piece must survive stop/max-duration.
        // An unconfirmed short candidate is intentionally discarded.
        if (confirmed) emitActive()
        candidate.clear()
        preRoll.clear()
        confirmed = false
        silent = 0
        applySettings()
    }
}
