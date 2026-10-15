package dev.orbit.orbit_android

import android.Manifest
import android.appwidget.AppWidgetManager
import android.content.Context
import android.content.ComponentName
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ActivityInfo
import android.view.View
import android.view.WindowManager
import android.os.Build
import android.net.Uri
import android.provider.Settings
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.Executors

class MainActivity : FlutterActivity() {
    override fun attachBaseContext(newBase: Context) {
        super.attachBaseContext(OrbitAppearance.themedContext(newBase))
    }

    private val io = Executors.newSingleThreadExecutor()
    private var matrixFullscreen = false
    private var previousOrientation = ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
    private var previousSystemUi = 0
    private var previousKeepScreenOn = false

    @Suppress("DEPRECATION")
    private fun setMatrixFullscreen(enabled: Boolean) {
        if (enabled == matrixFullscreen) return
        if (enabled) {
            previousOrientation = requestedOrientation
            previousSystemUi = window.decorView.systemUiVisibility
            previousKeepScreenOn = window.attributes.flags and WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON != 0
            matrixFullscreen = true
            requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
            window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
            applyMatrixSystemUi()
        } else {
            matrixFullscreen = false
            requestedOrientation = previousOrientation
            window.decorView.systemUiVisibility = previousSystemUi
            if (!previousKeepScreenOn) window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        }
    }

    @Suppress("DEPRECATION")
    private fun applyMatrixSystemUi() {
        window.decorView.systemUiVisibility = View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY or
            View.SYSTEM_UI_FLAG_FULLSCREEN or View.SYSTEM_UI_FLAG_HIDE_NAVIGATION or
            View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN or View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION or
            View.SYSTEM_UI_FLAG_LAYOUT_STABLE
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (hasFocus && matrixFullscreen) applyMatrixSystemUi()
    }
    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "dev.orbit/node").setMethodCallHandler { call, result ->
            try {
                when (call.method) {
                    "getDarkMode" -> result.success(OrbitAppearance.isDark(this))
                    "setDarkMode" -> {
                        OrbitAppearance.save(this, call.arguments == true)
                        OrbitWidgets.updateAll(this, force = true)
                        result.success(null)
                    }
                    "deviceModel" -> result.success(Build.MODEL)
                    "appConnection" -> {
                        val args = call.arguments as? Map<*, *>
                        val vault = ConfigVault(this, "orbit_app_connection")
                        val next = args?.let { NodeConfig(it["server"].toString(), "", "", it["token"].toString()) }
                        if (next != null) {
                            val uri = java.net.URI(next.uri)
                            require(uri.scheme in listOf("https", "http") && !uri.host.isNullOrBlank() && uri.userInfo == null && uri.query == null && uri.fragment == null && next.password.isNotBlank())
                        }
                        val previous = vault.load()
                        if (previous != next) {
                            if (next == null) vault.clear() else vault.save(next)
                            ViewStore(this).clear()
                            if (next == null) stopService(Intent(this, OrbitService::class.java))
                            OrbitWidgets.updateAll(this, force = true)
                        }
                        if (next != null && !OrbitService.active) startForegroundService(Intent(this, OrbitService::class.java))
                        else if (next != null && previous != next) startForegroundService(Intent(this, OrbitService::class.java))
                        result.success(null)
                    }
                    "listLaunchableApps" -> {
                        val intent = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
                        val apps = packageManager.queryIntentActivities(intent, 0)
                            .map { mapOf("target" to it.activityInfo.packageName, "title" to it.loadLabel(packageManager).toString()) }
                            .distinctBy { it["target"] }.sortedBy { it["title"] }
                        result.success(apps)
                    }
                    "openLocalApp" -> {
                        val target = call.arguments as String
                        require(Regex("^[A-Za-z][A-Za-z0-9_]*(\\.[A-Za-z0-9_]+)+$").matches(target))
                        val intent = packageManager.getLaunchIntentForPackage(target)
                        if (intent == null) result.success(false) else {
                            startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                            result.success(true)
                        }
                    }
                    "matrixFullscreen" -> { setMatrixFullscreen(call.arguments == true); result.success(null) }
                    "showInputMethodPicker" -> {
                        startActivity(InputMethodTools.pickerIntent(this))
                        result.success(null)
                    }
                    "pinInputMethodShortcut" -> result.success(InputMethodTools.pin(this))
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
                            result.success(when {
                                !manager.isRequestPinAppWidgetSupported -> "unsupported"
                                manager.requestPinAppWidget(ComponentName(this, provider), null, null) -> "requested"
                                else -> "rejected"
                            })
                        } catch (_: SecurityException) {
                            result.success("rejected")
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
