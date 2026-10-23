package dev.orbit.orbit_android

/** Frames are always 20 ms / 320 signed PCM samples. Called only by the processor. */
data class AudioFrame(val samples: ShortArray, val startedAt: Long, val speech: Boolean)
class AudioSegmenter(
    private val emit: (List<AudioFrame>) -> Unit,
    private val preRollFrames: Int = 15,
    private val tailFrames: Int = 30,
    private val maxFrames: Int = 500,
) {
    private val preRoll = ArrayDeque<AudioFrame>()
    private var active = mutableListOf<AudioFrame>()
    private var silent = 0
    fun accept(frame: AudioFrame) {
        require(frame.samples.size == 320)
        if (active.isEmpty()) {
            if (!frame.speech) {
                preRoll.addLast(frame)
                while (preRoll.size > preRollFrames) preRoll.removeFirst()
                return
            }
            active.addAll(preRoll)
            preRoll.clear()
        }
        active.add(frame)
        silent = if (frame.speech) 0 else silent + 1
        if (active.size == maxFrames || silent >= tailFrames) flush()
    }
    fun flush() {
        if (active.any { it.speech }) emit(active.toList())
        active.clear()
        silent = 0
    }
}
