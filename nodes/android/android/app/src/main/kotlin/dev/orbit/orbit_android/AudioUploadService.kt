package dev.orbit.orbit_android

import android.app.*
import android.content.*
import android.os.*
import org.eclipse.paho.client.mqttv3.*
import org.eclipse.paho.client.mqttv3.persist.MemoryPersistence
import java.io.File
import java.security.MessageDigest
import java.util.UUID
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/** Sending survives microphone shutdown and never waits for a broker/Core receipt. */
class AudioUploadService : Service() {
    companion object {
        @Volatile var pending = 0
            private set
        @Volatile var transfer = ""
            private set
        @Volatile var transferState = "idle"
            private set
        @Volatile var inFlight = 0
            private set
        // Process-lifetime totals survive individual upload service restarts.
        @Volatile var sentPackets = 0L
            private set
        @Volatile var sentBytes = 0L
            private set
        @Volatile var lastSentAt = 0L
            private set
        fun start(context: Context) {
            try { context.startForegroundService(Intent(context, AudioUploadService::class.java)) }
            catch (_: Exception) {
                transferState = "paused"
                transfer = "录音保存在本机，请回到应用继续上传"
            }
        }
        fun folder(context: Context, config: NodeConfig): File {
            val owner = MessageDigest.getInstance("SHA-256")
                .digest("${config.uri}|${config.nodeId}|${config.username}".toByteArray())
                .joinToString("") { "%02x".format(it) }
            return File(File(context.filesDir, "audio-outbox"), owner)
        }
    }
    private val worker = Executors.newSingleThreadScheduledExecutor()
    private val main = Handler(Looper.getMainLooper())
    @Volatile private var alive = true
    private var config: NodeConfig? = null
    private var outbox: AudioOutbox? = null
    private var mqtt: MqttAsyncClient? = null
    private var pump: AudioUploadPump? = null
    private var retryAt = 0L
    private var idleSince = 0L
    private var lastStartId = 0
    private var otherPending = 0
    private var wakeLock: PowerManager.WakeLock? = null
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel("orbit_audio_upload", "Orbit 录音上传", NotificationManager.IMPORTANCE_LOW))
        val open = PendingIntent.getActivity(this, 32, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        startForeground(32, Notification.Builder(this, "orbit_audio_upload").setSmallIcon(R.drawable.ic_orbit)
            .setContentTitle("Orbit 录音上传").setContentText("正在发送本机录音，停止录音后仍会继续")
            .setContentIntent(open).setOngoing(true).build())
        wakeLock = getSystemService(PowerManager::class.java).newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "Orbit:AudioUpload")
            .apply { acquire() }
        worker.scheduleWithFixedDelay({ tick(refreshConfiguration = true) }, 0, 1, TimeUnit.SECONDS)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        post {
            lastStartId = startId
            idleSince = 0
            if (intent?.action == "clearQueue") {
                @Suppress("DEPRECATION")
                val receiver = intent.getParcelableExtra<ResultReceiver>("result")
                try {
                    // Close the pump before deleting files so late send callbacks are ignored.
                    disconnect()
                    val removed = AudioOutbox.clearAll(File(filesDir, "audio-outbox"))
                    otherPending = 0
                    retryAt = 0
                    pending = outbox?.count() ?: 0
                    transfer = "已清空缓存队列"
                    receiver?.send(0, Bundle().apply { putInt("removed", removed) })
                } catch (_: Exception) {
                    receiver?.send(1, Bundle.EMPTY)
                }
            }
            tick(refreshConfiguration = true)
        }
        return START_NOT_STICKY
    }

    private fun post(action: () -> Unit) {
        if (!alive) return
        runCatching { worker.execute { if (alive) action() } }
    }
    private fun disconnect() {
        pump?.close()
        pump = null
        inFlight = 0
        val client = mqtt
        mqtt = null
        runCatching { client?.disconnectForcibly(0, 0, false) }
        runCatching { client?.close(true) }
    }
    private fun updateStatus() {
        pending = outbox?.count() ?: 0
        inFlight = pump?.inFlight ?: 0
        transferState = when {
            pump?.retrying == true -> "retrying"
            pending > 0 -> "sending"
            otherPending > 0 -> "waiting"
            else -> "idle"
        }
        transfer = (if (pending == 0) "当前连接没有待发送的录音" else if (pump?.retrying == true)
            "部分片段发送失败，保存在本机，将自动重试" else "正在发送录音片段，停止录音后也会继续发送") +
            if (otherPending > 0) "；另有 $otherPending 段属于其他连接，请切回原连接发送" else ""
    }
    private fun tick(refreshConfiguration: Boolean = false) {
        if (!alive) return
        try {
            // Keystore reads belong to startup/config reconciliation, not every send completion.
            val next = if (refreshConfiguration) AudioCaptureService.configuration(this) else config
            if (next != config) {
                disconnect()
                // Never keep an old queue paired with a new destination if opening it fails.
                config = null
                outbox = null
                otherPending = 0
                val opened = next?.let {
                    it.validate()
                    val directory = folder(this, it)
                    AudioOutbox(directory, budgetDirectory = directory.parentFile!!).also {
                        otherPending = directory.parentFile!!.walkTopDown().count { f ->
                            f.isFile && f.name.endsWith(".json") && f.parentFile != directory
                        }
                    }
                }
                outbox = opened
                config = next
                retryAt = 0
            }
            val box = outbox
            if (next == null || box == null) {
                pending = 0
                transferState = "waiting"
                transfer = "等待设备连接配置，录音保存在本机"
                finishIdle()
                return
            }
            pending = box.count()
            if (pending == 0) {
                updateStatus()
                finishIdle()
                return
            }
            idleSince = 0
            if (SystemClock.elapsedRealtime() < retryAt) return
            if (pump?.timedOut == true) disconnect()
            if (mqtt?.isConnected != true) {
                disconnect()
                val client = MqttAsyncClient(next.uri, "orbit-audio-${next.nodeId}-${UUID.randomUUID().toString().take(8)}", MemoryPersistence())
                mqtt = client
                client.setCallback(object : MqttCallback {
                    override fun messageArrived(topic: String?, message: MqttMessage?) {}
                    override fun deliveryComplete(token: IMqttDeliveryToken?) {}
                    override fun connectionLost(cause: Throwable?) { post {
                        if (mqtt === client) {
                            disconnect()
                            retryAt = SystemClock.elapsedRealtime() + 2000
                            transferState = "retrying"
                            transfer = "连接断开 · 未发送的录音保存在本机，等待重试"
                        }
                    } }
                })
                transferState = "connecting"
                transfer = "正在连接发送服务，录音已保存在本机"
                client.connect(OrbitMqtt.options(next).apply { keepAliveInterval = 30; maxInflight = 64 })
                    .waitForCompletion(12000)
                val sender = AudioMqttSender(client, "orbit/v1/nodes/${next.nodeId}/audio/segments", ::post)
                pump = AudioUploadPump(box, { AudioCaptureService.settings(this).uploadConcurrency },
                    { _, payload, complete -> sender.send(payload, complete) },
                    { post { tick() } }, { SystemClock.elapsedRealtime() }, { bytes ->
                        sentPackets++
                        sentBytes += bytes
                        lastSentAt = System.currentTimeMillis()
                    })
            }
            pump?.pump()
            updateStatus()
        } catch (error: Exception) {
            disconnect()
            retryAt = SystemClock.elapsedRealtime() + 2000
            transferState = "retrying"
            val reason = OrbitMqtt.safeError(error)
            android.util.Log.w("OrbitAudio", "Upload failed: type=${error.javaClass.simpleName} reason=$reason")
            transfer = "无法发送 · $reason；未发送的录音保存在本机，将自动重试"
        }
    }
    private fun finishIdle() {
        val now = SystemClock.elapsedRealtime()
        if (idleSince == 0L) idleSince = now
        if (now - idleSince < 15000) return // Keep the connection warm for the next sentence/final flush.
        val id = lastStartId
        main.post { if (alive) stopSelfResult(id) }
    }
    // Android 15 limits background dataSync foreground-service time.
    override fun onTimeout(startId: Int, fgsType: Int) {
        transferState = "paused"
        transfer = "系统暂停了后台上传，请回到应用继续发送"
        stopSelf()
    }
    override fun onDestroy() {
        alive = false
        worker.execute {
            disconnect()
            wakeLock?.let { if (it.isHeld) it.release() }
        }
        worker.shutdown()
        super.onDestroy()
    }
}
