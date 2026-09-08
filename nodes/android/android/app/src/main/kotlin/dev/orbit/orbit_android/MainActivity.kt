package dev.orbit.orbit_android

import android.Manifest
import android.appwidget.AppWidgetManager
import android.content.ComponentName
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.net.Uri
import android.provider.Settings
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.Executors

class MainActivity : FlutterActivity() {
    private val io = Executors.newSingleThreadExecutor()
    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "dev.orbit/node").setMethodCallHandler { call, result ->
            try {
                when (call.method) {
                    "load" -> result.success(ConfigVault(this).load()?.map())
                    "snapshot" -> result.success(ViewStore(this).snapshot())
                    "save" -> {
                        val config = NodeConfig.from(call.arguments as Map<*, *>).also { it.validate() }
                        val old = ConfigVault(this).load()
                        ConfigVault(this).save(config)
                        if (old?.nodeId != config.nodeId || old.uri != config.uri) ViewStore(this).clear()
                        // A running service reloads on its serialized worker; no overlapping client IDs.
                        if (OrbitService.active) startService(Intent(this, OrbitService::class.java))
                        OrbitWidgets.updateAll(this)
                        result.success(null)
                    }
                    "start" -> {
                        requireNotNull(ConfigVault(this).load()) { "请先保存 MQTT 配置" }.validate()
                        if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
                            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 1)
                        }
                        startForegroundService(Intent(this, OrbitService::class.java))
                        result.success(null)
                    }
                    "stop" -> { stopService(Intent(this, OrbitService::class.java)); result.success(null) }
                    "pin" -> {
                        val provider = if (call.arguments == "usage") UsageWidget::class.java else SessionWidget::class.java
                        val manager = getSystemService(AppWidgetManager::class.java)
                        try {
                            result.success(manager.isRequestPinAppWidgetSupported && manager.requestPinAppWidget(ComponentName(this, provider), null, null))
                        } catch (_: SecurityException) {
                            result.success(false)
                        }
                    }
                    "appSettings" -> {
                        startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:$packageName")))
                        result.success(null)
                    }
                    "test" -> {
                        val config = NodeConfig.from(call.arguments as Map<*, *>).also { it.validate() }
                        io.execute {
                            var client: org.eclipse.paho.client.mqttv3.MqttClient? = null
                            val message = try {
                                client = OrbitMqtt.client(config, testing = true)
                                client.connect(OrbitMqtt.options(config))
                                val granted = client.subscribeWithResponse("orbit/v1/nodes/${config.nodeId}/view", 1).grantedQos
                                if (granted.size != 1 || granted[0] !in 0..1) "连接成功，但 view 订阅权限被拒绝"
                                else "连接、认证和 view 订阅成功。尚未验证 Core 路由和 state 发布权限；请保存并开始同步确认数据。"
                            } catch (error: Exception) { OrbitMqtt.safeError(error) }
                            finally { OrbitMqtt.close(client) }
                            runOnUiThread { result.success(message) }
                        }
                    }
                    else -> result.notImplemented()
                }
            } catch (error: IllegalArgumentException) {
                result.error("invalid_config", error.message ?: "配置无效", null)
            } catch (error: Exception) {
                result.error("operation_failed", "操作失败，请重新保存配置或检查设备设置", null)
            }
        }
    }
    override fun onStart() {
        super.onStart()
        OrbitService.appVisible = true
        OrbitService.reconcile()
    }
    override fun onStop() {
        OrbitService.appVisible = false
        OrbitService.reconcile()
        super.onStop()
    }
    override fun onDestroy() { io.shutdown(); super.onDestroy() }
}
