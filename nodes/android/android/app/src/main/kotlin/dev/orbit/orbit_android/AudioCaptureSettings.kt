package dev.orbit.orbit_android

import android.content.Context

/** Persisted durations are milliseconds; every processing frame is exactly 20 ms. */
data class AudioCaptureSettings(
    val minSpeechMs: Int = 300,
    val silenceMs: Int = 600,
    val preRollMs: Int = 300,
    val maxSegmentMs: Int = 10000,
    val vadMode: Int = 2,
    val uploadConcurrency: Int = 16,
) {
    init {
        duration(minSpeechMs, "连续人声时长", 100, 2000)
        duration(silenceMs, "结束静音时长", 200, 2000)
        duration(preRollMs, "语音预留时长", 0, 1000)
        duration(maxSegmentMs, "最长片段时长", 3000, 10000)
        require(uploadConcurrency in 1..64) { "最大上传并发需为 1–64 的整数" }
        require(vadMode in 0..3) { "语音检测严格度需为 0–3 的整数" }
    }
    fun map(): Map<String, Int> = mapOf(
        "minSpeechMs" to minSpeechMs, "silenceMs" to silenceMs,
        "preRollMs" to preRollMs, "maxSegmentMs" to maxSegmentMs, "vadMode" to vadMode,
        "uploadConcurrency" to uploadConcurrency,
    )
    companion object {
        private val keys = setOf("minSpeechMs", "silenceMs", "preRollMs", "maxSegmentMs", "vadMode", "uploadConcurrency")
        private fun duration(value: Int, label: String, low: Int, high: Int) {
            require(value in low..high && value % 20 == 0) { "$label 需为 $low–$high 毫秒，且为 20 毫秒的整数倍" }
        }
        fun fromMap(value: Any?): AudioCaptureSettings {
            require(value is Map<*, *> && value.keys == keys) { "请提供完整的录音参数，不能包含其他字段" }
            fun integer(key: String): Int {
                val input = value[key]
                require(input is Int || input is Long || input is Short || input is Byte) { "录音参数 $key 必须为整数" }
                val number = (input as Number).toLong()
                require(number in Int.MIN_VALUE.toLong()..Int.MAX_VALUE.toLong()) { "录音参数 $key 超出允许范围" }
                return number.toInt()
            }
            return AudioCaptureSettings(integer("minSpeechMs"), integer("silenceMs"), integer("preRollMs"),
                integer("maxSegmentMs"), integer("vadMode"), integer("uploadConcurrency"))
        }
    }
}

/** Persistence is injected so commit failures and round trips are testable on the JVM. */
class AudioCaptureSettingsStore(
    private val read: () -> Map<String, *>,
    private val write: (Map<String, Int>) -> Boolean,
) {
    fun load(): AudioCaptureSettings = AudioCaptureSettings.fromMap(AudioCaptureSettings().map() + read())
    fun save(settings: AudioCaptureSettings): AudioCaptureSettings {
        check(write(settings.map())) { "未能保存录音参数，请检查设备可用空间后重试" }
        return settings
    }
    companion object {
        fun forContext(context: Context): AudioCaptureSettingsStore {
            val preferences = context.getSharedPreferences("orbit_audio_settings", Context.MODE_PRIVATE)
            return AudioCaptureSettingsStore({ preferences.all }, { values ->
                val editor = preferences.edit().clear()
                values.forEach { (key, value) -> editor.putInt(key, value) }
                editor.commit()
            })
        }
    }
}
