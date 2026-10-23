package dev.orbit.orbit_android

import android.Manifest
import android.app.*
import android.content.*
import android.content.pm.PackageManager
import android.media.*
import android.os.*
import com.konovalov.vad.webrtc.VadWebRTC
import com.konovalov.vad.webrtc.config.*
import io.github.jaredmdobson.concentus.OpusApplication
import io.github.jaredmdobson.concentus.OpusEncoder
import org.eclipse.paho.client.mqttv3.*
import org.eclipse.paho.client.mqttv3.persist.MemoryPersistence
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.security.MessageDigest
import java.time.Instant
import java.util.Base64
import java.util.UUID
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

/** A separate microphone service: screen/widget lifecycle never suspends capture. */
class AudioCaptureService : Service() {
    companion object {
        private val startPolicy = AudioStartPolicy()
        @Volatile private var draining = false
        @Volatile var active = false
            private set
        @Volatile var state = "尚未开始录音"
            private set
        @Volatile var transfer = ""
            private set
        @Volatile var pending = 0
            private set
        fun preferences(context: Context) = context.getSharedPreferences("orbit_audio", Context.MODE_PRIVATE)
        fun enabled(context: Context) = preferences(context).getBoolean("enabled", true)
        @Suppress("DEPRECATION")
        private fun unifiedApp(context: Context): Boolean = preferences(context).getBoolean("unified_app", false) ||
            context.packageManager.getApplicationInfo(context.packageName, PackageManager.GET_META_DATA)
                .metaData?.getBoolean("dev.orbit.UNIFIED_APP", false) == true
        fun configuration(context: Context): NodeConfig? {
            val hasOwner = ConfigVault(context, "orbit_app_connection").load() != null
            val unified = unifiedApp(context)
            return if (AudioConfigurationPolicy.legacyAllowed(unified, hasOwner)) ConfigVault(context).load()
                else if (hasOwner) ConfigVault(context, "orbit_app_mqtt").load() else null
        }
        fun start(context: Context) {
            try { startChecked(context) }
            catch (_: Exception) { state = "无法读取录音连接配置，请重新连接 Orbit 后重试" }
        }
        private fun startChecked(context: Context) {
            if (!startPolicy.request(active, draining, OrbitService.appVisible, enabled(context))) {
                if (!active && draining && enabled(context)) state = "正在保存最后一段录音，完成后自动开始"
                return
            }
            if (context.checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) return
            if (configuration(context) == null) {
                state = if (unifiedApp(context) || ConfigVault(context, "orbit_app_connection").load() != null)
                    "等待设备连接配置，连接 Orbit 后自动开始录音" else "请先保存 MQTT 配置"
                return
            }
            try { context.startForegroundService(Intent(context, AudioCaptureService::class.java)) }
            catch (_: Exception) { state = "无法启动后台录音，请回到应用后重试" }
        }
        fun snapshot(context: Context): Map<String, Any> = mapOf(
            "active" to active, "state" to state, "transfer" to transfer, "pending" to pending,
            "enabled" to enabled(context),
            "notifications" to context.getSystemService(NotificationManager::class.java).areNotificationsEnabled(),
            "permission" to (context.checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED),
        )
    }
    private val capture = Executors.newSingleThreadExecutor()
    private val processing = Executors.newSingleThreadExecutor()
    private val network = Executors.newSingleThreadScheduledExecutor()
    private val uploadQueued = AtomicBoolean(false)
    private val frames = ArrayBlockingQueue<Pair<ShortArray, Long>>(250)
    @Volatile private var running = true
    @Volatile private var capturing = true
    @Volatile private var recorder: AudioRecord? = null
    private var mqtt: MqttClient? = null // Network worker owns all client operations.
    private var wakeLock: PowerManager.WakeLock? = null
    private lateinit var config: NodeConfig
    private lateinit var outbox: AudioOutbox
    private val session = UUID.randomUUID().toString()
    private var sequence = 0L
    private var otherPending = 0
    private fun backlogNotice(): String = if (otherPending > 0) "；另有 $otherPending 段属于其他连接，请切回原连接同步" else ""
    override fun onBind(intent: Intent?): IBinder? = null
    override fun onCreate() {
        super.onCreate()
        active = true
        state = "正在准备录音"
        transfer = ""
        try {
            showNotification("正在准备录音")
            config = requireNotNull(configuration(this)) { "请先保存 MQTT 配置" }.also { it.validate() }
            require(Regex("[a-z0-9][a-z0-9_-]{0,63}").matches(config.nodeId)) { "录音设备 ID 需使用小写字母、数字、下划线或短横线，首位为字母或数字" }
            // A connection change cannot send an old device/broker backlog to a new destination.
            val owner = MessageDigest.getInstance("SHA-256").digest("${config.uri}|${config.nodeId}|${config.username}".toByteArray())
                .joinToString("") { "%02x".format(it) }
            val root = File(filesDir, "audio-outbox")
            val folder = File(root, owner)
            outbox = AudioOutbox(folder, budgetDirectory = root)
            otherPending = root.walkTopDown().count { it.isFile && it.name.endsWith(".json") && it.parentFile != folder }
            pending = outbox.count()
            wakeLock = getSystemService(PowerManager::class.java).newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "Orbit:Audio").apply { acquire() }
            draining = true
            processing.execute { process() }
            capture.execute { record() }
            network.scheduleWithFixedDelay({ upload() }, 0, 5, TimeUnit.SECONDS)
        } catch (error: Exception) {
            capturing = false
            if (!draining) wakeLock?.let { if (it.isHeld) it.release() }
            fail(if (error is IllegalArgumentException) error.message ?: "录音连接配置无效"
                else "无法准备录音，已停止；请检查麦克风权限和可用空间后重试")
        }
    }
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == "stop") {
            preferences(this).edit().putBoolean("enabled", false).apply()
            state = "已停止录音"
            stopSelf()
        }
        // Microphone foreground services must start while an activity is visible.
        return START_NOT_STICKY
    }
    private fun showNotification(text: String) {
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel("orbit_audio", "Orbit 录音", NotificationManager.IMPORTANCE_LOW))
        val open = PendingIntent.getActivity(this, 30, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val stop = PendingIntent.getService(this, 31, Intent(this, AudioCaptureService::class.java).setAction("stop"), PendingIntent.FLAG_IMMUTABLE)
        val notification = Notification.Builder(this, "orbit_audio").setSmallIcon(R.drawable.ic_orbit)
            .setContentTitle("Orbit 录音").setContentText(text).setContentIntent(open).setOngoing(true)
            .addAction(Notification.Action.Builder(null, "停止录音", stop).build()).build()
        startForeground(30, notification)
    }
    private fun fail(message: String) {
        state = message
        running = false
        Handler(mainLooper).post { stopSelf() }
    }
    private fun record() {
        Process.setThreadPriority(Process.THREAD_PRIORITY_AUDIO)
        var audio: AudioRecord? = null
        try {
            val minimum = AudioRecord.getMinBufferSize(16000, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
            check(minimum > 0)
            audio = AudioRecord(MediaRecorder.AudioSource.MIC, 16000, AudioFormat.CHANNEL_IN_MONO,
                AudioFormat.ENCODING_PCM_16BIT, maxOf(minimum * 2, 6400))
            recorder = audio
            check(audio.state == AudioRecord.STATE_INITIALIZED)
            if (!running) return
            audio.startRecording()
            check(audio.recordingState == AudioRecord.RECORDSTATE_RECORDING)
            state = "正在后台录音"
            Handler(mainLooper).post { if (running) showNotification("正在后台录音 · 语音片段上传到 Core") }
            val origin = System.currentTimeMillis()
            var frameIndex = 0L
            while (running) {
                val samples = ShortArray(320)
                var offset = 0
                while (running && offset < samples.size) {
                    val read = audio.read(samples, offset, samples.size - offset, AudioRecord.READ_BLOCKING)
                    check(read > 0 || !running) { "麦克风读取失败，请重新开始录音" }
                    if (read > 0) offset += read
                }
                if (!running) break
                if (!frames.offer(samples to (origin + frameIndex * 20))) {
                    fail("音频处理跟不上采集，已停止录音；部分声音未保存，请重新开始")
                    break
                }
                frameIndex++
            }
        } catch (_: SecurityException) { fail("麦克风权限已关闭，请在设置中允许后重新开始") }
        catch (_: Exception) { if (running) fail("无法读取麦克风，已停止录音，请检查设备后重试") }
        finally {
            runCatching { audio?.stop() }
            audio?.release()
            recorder = null
            capturing = false
        }
    }
    private fun process() {
        try {
            VadWebRTC(SampleRate.SAMPLE_RATE_16K, FrameSize.FRAME_SIZE_320, Mode.AGGRESSIVE, 0, 0).use { vad ->
                val segmenter = AudioSegmenter({ persist(it) })
                while (running || capturing || frames.isNotEmpty()) {
                    val frame = frames.poll(100, TimeUnit.MILLISECONDS) ?: continue
                    segmenter.accept(AudioFrame(frame.first, frame.second, vad.isSpeech(frame.first)))
                }
                segmenter.flush()
            }
        } catch (_: Exception) { if (running) fail("音频处理或保存失败，已停止录音；当前片段未保存，请检查可用空间后重试") }
        catch (_: LinkageError) { fail("当前设备无法加载语音检测组件，录音已停止") }
        finally {
            wakeLock?.let { if (it.isHeld) it.release() }
            Handler(mainLooper).post {
                draining = false
                if (startPolicy.drained(OrbitService.appVisible, enabled(this))) start(applicationContext)
            }
        }
    }
    private fun persist(frames: List<AudioFrame>) {
        val encoder = OpusEncoder(16000, 1, OpusApplication.OPUS_APPLICATION_VOIP).apply {
            bitrate = 24000
            complexity = 3
            useVBR = false // 60-byte packets keep even a 10 s JSON well below 128 KiB.
        }
        val packets = JSONArray()
        frames.forEach { frame ->
            val buffer = ByteArray(1275)
            val size = encoder.encode(frame.samples, 0, 320, buffer, 0, buffer.size)
            check(size in 1..1275)
            packets.put(Base64.getEncoder().encodeToString(buffer.copyOf(size)))
        }
        val id = UUID.randomUUID().toString()
        val payload = JSONObject().put("schema_version", 1).put("segment_id", id).put("session_id", session)
            .put("node_id", config.nodeId).put("sequence", sequence++)
            .put("started_at", Instant.ofEpochMilli(frames.first().startedAt).toString())
            .put("duration_ms", frames.size * 20).put("speech_ms", frames.count { it.speech } * 20)
            .put("sample_rate", 16000).put("channels", 1).put("codec", "opus").put("frame_ms", 20)
            .put("packets", packets).toString().toByteArray(Charsets.UTF_8)
        try { outbox.put(id, payload) }
        catch (error: IllegalStateException) {
            fail("本地录音队列已满或无法保存，已停止录音；当前片段未保存，请连接 Core 后重试")
            throw error
        }
        pending = outbox.count()
        wakeUploader()
    }
    private fun wakeUploader() {
        if (!running || !uploadQueued.compareAndSet(false, true)) return
        try {
            network.execute {
                uploadQueued.set(false)
                if (running) upload()
            }
        } catch (_: java.util.concurrent.RejectedExecutionException) {
            uploadQueued.set(false) // Service stopped while its last segment was being saved.
        }
    }
    private fun upload() {
        if (!running) return
        try {
            var client = mqtt
            if (client?.isConnected != true) {
                OrbitMqtt.close(client)
                client = MqttClient(config.uri, "orbit-audio-${config.nodeId}-${UUID.randomUUID().toString().take(8)}", MemoryPersistence())
                client.setTimeToWait(12000)
                mqtt = client
                val expected = "orbit/v1/nodes/${config.nodeId}/audio/acks"
                client.setCallback(object : MqttCallback {
                    override fun connectionLost(cause: Throwable?) { transfer = "连接断开 · 录音保存在本机，等待重传" }
                    override fun deliveryComplete(token: IMqttDeliveryToken?) {}
                    override fun messageArrived(topic: String?, message: MqttMessage?) {
                        if (topic != expected || message == null || message.isRetained || message.payload.size > 4096) return
                        val ack = runCatching { JSONObject(String(message.payload, Charsets.UTF_8)) }.getOrNull() ?: return
                        if (ack.optInt("schema_version") != 1 || ack.optString("status") != "stored") return
                        val id = ack.optString("segment_id")
                        if (runCatching { UUID.fromString(id).toString() == id }.getOrDefault(false)) {
                            runCatching { network.execute {
                                try { outbox.acknowledge(id); pending = outbox.count() }
                                catch (_: Exception) { transfer = "未能释放已确认的录音，将重新发送" }
                            } }
                        }
                    }
                })
                client.connect(OrbitMqtt.options(config).apply { keepAliveInterval = 30 })
                val granted = client.subscribeWithResponse(expected, 1).grantedQos
                check(granted.size == 1 && granted[0] == 1) { "audio subscription rejected" }
            }
            outbox.pending().forEach { (_, payload) ->
                client.publish("orbit/v1/nodes/${config.nodeId}/audio/segments", payload, 1, false)
            }
            transfer = (if (pending == 0) "录音队列已同步" else "等待 Core 确认 · 本机保留 $pending 段录音") + backlogNotice()
        } catch (_: Exception) {
            transfer = "无法上传 · 录音保存在本机，将自动重试；请检查网络和录音设备账号权限" + backlogNotice()
            OrbitMqtt.close(mqtt); mqtt = null
        }
    }
    override fun onDestroy() {
        running = false
        active = false
        runCatching { recorder?.stop() }
        capture.shutdown()
        processing.shutdown() // Drain captured frames and persist the last speech segment.
        network.execute { OrbitMqtt.close(mqtt); mqtt = null }
        network.shutdown()
        if (state == "正在后台录音") state = "已停止录音"
        super.onDestroy()
    }
}
