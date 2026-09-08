package dev.orbit.orbit_android

import android.app.*
import android.content.Intent
import android.os.IBinder
import com.google.protobuf.Timestamp
import orbit.v1.Common.Metadata
import orbit.v1.State.NodeState
import org.eclipse.paho.client.mqttv3.*
import org.eclipse.paho.client.mqttv3.persist.MemoryPersistence
import java.util.UUID
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

object OrbitMqtt {
    fun options(config: NodeConfig) = MqttConnectOptions().apply {
        mqttVersion = MqttConnectOptions.MQTT_VERSION_3_1_1
        isCleanSession = true
        connectionTimeout = 10
        keepAliveInterval = 30
        if (config.username.isNotEmpty()) userName = config.username
        if (config.password.isNotEmpty()) password = config.password.toCharArray()
        // Paho uses the platform trust store and validates TLS hostnames by default.
        isHttpsHostnameVerificationEnabled = true
    }
    fun client(config: NodeConfig, testing: Boolean = false) = MqttClient(config.uri,
        "orbit-node-${config.nodeId}" + if (testing) "-test-${UUID.randomUUID().toString().take(8)}" else "", MemoryPersistence()).apply {
        setTimeToWait(12000)
    }
    fun close(client: MqttClient?) {
        if (client == null) return
        runCatching { if (client.isConnected) client.disconnect(1000) }
        runCatching { client.close(true) }
    }
    fun safeError(error: Throwable): String = when (error) {
        is MqttException -> when (error.reasonCode) {
            4, 5 -> "认证失败或没有连接权限"
            32103 -> "无法连接，请检查地址、端口、网络或 TLS 证书"
            32000 -> "连接超时"
            else -> "MQTT 操作失败（${error.reasonCode}）"
        }
        else -> "连接失败，请检查配置、网络和证书"
    }
}

class OrbitService : Service() {
    private val worker = Executors.newSingleThreadScheduledExecutor()
    private var client: MqttClient? = null
    private var config: NodeConfig? = null
    private var epoch = UUID.randomUUID().toString()
    private var revision = 0L
    private var nextAttempt = 0L
    @Volatile private var alive = true
    companion object {
        @Volatile var active = false
        @Volatile var connection = "未连接"
    }
    override fun onBind(intent: Intent?): IBinder? = null
    override fun onCreate() {
        super.onCreate()
        active = true
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel("orbit", "Orbit 桌面组件同步", NotificationManager.IMPORTANCE_LOW))
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val stop = PendingIntent.getService(this, 1, Intent(this, OrbitService::class.java).setAction("stop"), PendingIntent.FLAG_IMMUTABLE)
        startForeground(1, Notification.Builder(this, "orbit").setSmallIcon(R.drawable.ic_orbit)
            .setContentTitle("Orbit 桌面组件同步").setContentText("正在接收 MQTT 数据 · 可在应用中停止")
            .setContentIntent(open).setOngoing(true).addAction(Notification.Action.Builder(null, "停止", stop).build()).build())
        worker.scheduleWithFixedDelay({
            if (alive) {
                if (config != null && client?.isConnected != true && System.currentTimeMillis() >= nextAttempt) connect()
                OrbitWidgets.updateAll(this)
            }
        }, 1, 5, TimeUnit.SECONDS)
    }
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == "stop") { stopSelf(); return START_NOT_STICKY }
        worker.execute {
            if (!alive) return@execute
            OrbitMqtt.close(client)
            client = null
            val nextConfig = runCatching { ConfigVault(this).load()?.also { it.validate() } }.getOrNull()
            if (config != null && (config?.nodeId != nextConfig?.nodeId || config?.uri != nextConfig?.uri)) ViewStore(this).clear()
            config = nextConfig
            epoch = UUID.randomUUID().toString()
            revision = 0
            nextAttempt = 0
            connection = if (config == null) "请先配置 MQTT" else "正在连接"
            if (config == null) stopSelf()
        }
        return START_STICKY
    }
    private fun connect() {
        val cfg = config ?: return
        nextAttempt = System.currentTimeMillis() + 15000
        connection = "正在连接"
        OrbitMqtt.close(client)
        val current = OrbitMqtt.client(cfg)
        client = current
        current.setCallback(object : MqttCallback {
            override fun connectionLost(cause: Throwable?) { if (alive && client === current) connection = "连接断开 · 正在重试" }
            override fun deliveryComplete(token: IMqttDeliveryToken?) {}
            override fun messageArrived(topic: String?, message: MqttMessage?) {
                if (alive && topic == "orbit/v1/nodes/${cfg.nodeId}/view" && message != null) {
                    val bytes = message.payload.copyOf()
                    runCatching { worker.execute {
                        if (alive && client === current && ViewStore(this@OrbitService).accept(bytes, cfg.nodeId, System.currentTimeMillis())) {
                            connection = "已连接 · 已接收数据"
                            OrbitWidgets.updateAll(this@OrbitService)
                        }
                    } }
                }
            }
        })
        try {
            current.connect(OrbitMqtt.options(cfg))
            if (!alive) { OrbitMqtt.close(current); return }
            val granted = current.subscribeWithResponse("orbit/v1/nodes/${cfg.nodeId}/view", 1).grantedQos
            check(granted.size == 1 && granted[0] in 0..1) { "subscription rejected" }
            val now = Timestamp.newBuilder().setSeconds(System.currentTimeMillis() / 1000).build()
            val state = NodeState.newBuilder().setNodeId(cfg.nodeId).setNodeEpoch(epoch)
                .setSeriesId("display").setModelId("android").setVariantId("flutter").setFirmwareVersion("0.1.0")
                .setMetadata(Metadata.newBuilder().setMessageId(UUID.randomUUID().toString()).setProducerId(cfg.nodeId)
                    .setRevision(++revision).setProducedAt(now)).build()
            current.publish("orbit/v1/nodes/${cfg.nodeId}/state", state.toByteArray(), 1, true)
            connection = "已连接 · 等待 Core 数据"
        } catch (error: Exception) {
            connection = OrbitMqtt.safeError(error) + " · 正在重试"
            OrbitMqtt.close(current)
            if (client === current) client = null
        }
    }
    override fun onDestroy() {
        alive = false
        active = false
        connection = "已停止"
        worker.execute { OrbitMqtt.close(client); client = null }
        worker.shutdown()
        OrbitWidgets.updateAll(this)
        super.onDestroy()
    }
}
