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
import androidx.core.content.FileProvider
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.Executors

class MainActivity : FlutterActivity() {
    override fun attachBaseContext(newBase: Context) {
        super.attachBaseContext(OrbitAppearance.themedContext(newBase))
    }

    private val io = Executors.newSingleThreadExecutor()
    private val updates by lazy { AppUpdates(applicationContext) }
    private var pendingUpdatePermission = false

    private fun openUpdateInstaller() {
        val apk = updates.readyFile()
        if (Build.VERSION.SDK_INT >= 26 && !packageManager.canRequestPackageInstalls()) {
            updates.phase("permission")
            pendingUpdatePermission = true
            startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:$packageName")))
            return
        }
        val uri = FileProvider.getUriForFile(this, "$packageName.updates", apk)
        startActivity(Intent(Intent.ACTION_VIEW).setDataAndType(uri, "application/vnd.android.package-archive")
            .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION))
        updates.phase("installer")
    }

    override fun onResume() {
        super.onResume()
        AudioUploadService.start(this)
        if (checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) {
            AudioCaptureService.start(this)
        } else if (!AudioCaptureService.preferences(this).getBoolean("asked", false)) {
            AudioCaptureService.preferences(this).edit().putBoolean("asked", true).apply()
            android.app.AlertDialog.Builder(this).setTitle("允许后台录音？")
                .setMessage("Orbit 会持续使用麦克风，将语音片段保存在本机并上传到你配置的 Core。你可以随时从常驻通知或应用中停止录音。")
                .setPositiveButton("申请麦克风权限") { _, _ -> requestAudioPermission() }
                .setNegativeButton("暂不允许", null).show()
        }
        if (pendingUpdatePermission && packageManager.canRequestPackageInstalls()) {
            pendingUpdatePermission = false
            try { openUpdateInstaller() }
            catch (_: Exception) { updates.failed("无法打开安装器，请重新点击更新") }
        }
    }
    private var matrixFullscreen = false
    private var previousOrientation = ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
    private var previousSystemUi = 0
    private var previousKeepScreenOn = false

    @Suppress("DEPRECATION")
    private fun setMatrixFullscreen(enabled: Boolean, landscape: Boolean = true) {
        if (enabled == matrixFullscreen) return
        if (enabled) {
            previousOrientation = requestedOrientation
            previousSystemUi = window.decorView.systemUiVisibility
            previousKeepScreenOn = window.attributes.flags and WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON != 0
            matrixFullscreen = true
            requestedOrientation = if (landscape) ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
                else ActivityInfo.SCREEN_ORIENTATION_FULL_SENSOR
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
                    "audioStatus" -> result.success(AudioCaptureService.snapshot(this))
                    "audioClearQueue" -> {
                        val receiver = object : android.os.ResultReceiver(android.os.Handler(mainLooper)) {
                            override fun onReceiveResult(code: Int, data: android.os.Bundle?) {
                                if (code == 0) result.success(data?.getInt("removed") ?: 0)
                                else result.error("clear_failed", "未能完全清空缓存队列，请重试", null)
                            }
                        }
                        startForegroundService(Intent(this, AudioUploadService::class.java)
                            .setAction("clearQueue").putExtra("result", receiver))
                    }
                    "audioSaveSettings" -> {
                        val settings = try { AudioCaptureSettings.fromMap(call.arguments) }
                        catch (error: IllegalArgumentException) {
                            result.error("invalid_audio_settings", error.message ?: "录音参数无效", null)
                            return@setMethodCallHandler
                        }
                        io.execute {
                            try {
                                val saved = AudioCaptureService.saveSettings(this, settings)
                                runOnUiThread {
                                    AudioUploadService.start(this)
                                    result.success(saved.map())
                                }
                            } catch (_: Exception) {
                                runOnUiThread { result.error("save_failed", "未能保存录音参数，请检查设备可用空间后重试", null) }
                            }
                        }
                    }
                    "audioStart" -> {
                        AudioCaptureService.preferences(this).edit().putBoolean("enabled", true).apply()
                        if (checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) requestAudioPermission()
                        else AudioCaptureService.start(this)
                        result.success(null)
                    }
                    "audioStop" -> {
                        AudioCaptureService.preferences(this).edit().putBoolean("enabled", false).apply()
                        AudioUploadService.start(this)
                        stopService(Intent(this, AudioCaptureService::class.java))
                        result.success(null)
                    }
                    "updateStatus" -> result.success(updates.status())
                    "checkUpdate" -> io.execute {
                        try {
                            val status = updates.check()
                            runOnUiThread { result.success(status) }
                        } catch (error: Exception) {
                            runOnUiThread { result.error("update_failed", error.message, null) }
                        }
                    }
                    "installUpdate" -> io.execute {
                        try {
                            updates.download()
                            runOnUiThread {
                                try {
                                    openUpdateInstaller()
                                    result.success(updates.status())
                                } catch (_: Exception) {
                                    updates.failed("无法打开安装器，请检查设备是否允许安装应用")
                                    result.error("install_failed", "无法打开安装器，请检查设备是否允许安装应用", null)
                                }
                            }
                        } catch (error: Exception) {
                            runOnUiThread { result.error("update_failed", error.message, null) }
                        }
                    }
                    "getDarkMode" -> result.success(OrbitAppearance.isDark(this))
                    "setDarkMode" -> {
                        OrbitAppearance.save(this, call.arguments == true)
                        OrbitWidgets.updateAll(this, force = true)
                        result.success(null)
                    }
                    "deviceModel" -> result.success(Build.MODEL)
                    "appConnection" -> {
                        AudioCaptureService.preferences(this).edit().putBoolean("unified_app", true).apply()
                        val args = call.arguments as? Map<*, *>
                        val vault = ConfigVault(this, "orbit_app_connection")
                        val next = args?.let { NodeConfig(it["server"].toString(), "", "", it["token"].toString()) }
                        if (next != null) {
                            val uri = java.net.URI(next.uri)
                            require(uri.scheme in listOf("https", "http") && !uri.host.isNullOrBlank() && uri.userInfo == null && uri.query == null && uri.fragment == null && next.password.isNotBlank())
                        }
                        val previous = vault.load()
                        if (next == null) stopService(Intent(this, AudioCaptureService::class.java))
                        if (previous != next) {
                            stopService(Intent(this, AudioUploadService::class.java))
                            ConfigVault(this, "orbit_app_mqtt").clear()
                            if (next == null) vault.clear() else vault.save(next)
                            ViewStore(this).clear()
                            if (next == null) stopService(Intent(this, OrbitService::class.java))
                            stopService(Intent(this, AudioCaptureService::class.java))
                            OrbitWidgets.updateAll(this, force = true)
                        }
                        if (next != null && !OrbitService.active) startForegroundService(Intent(this, OrbitService::class.java))
                        else if (next != null && previous != next) startForegroundService(Intent(this, OrbitService::class.java))
                        result.success(null)
                    }
                    "appMqttConnection" -> {
                        val args = call.arguments as Map<*, *>
                        val owner = NodeConfig(args["server"].toString(), "", "", args["token"].toString())
                        if (ConfigVault(this, "orbit_app_connection").load() != owner) {
                            result.success(false)
                        } else {
                            val mqtt = NodeConfig.from(args["mqtt"] as Map<*, *>).also { it.validate() }
                            require(mqtt.username.isNotBlank() && mqtt.password.isNotBlank())
                            val vault = ConfigVault(this, "orbit_app_mqtt")
                            if (vault.load() != mqtt) {
                                require(!AudioCaptureService.active) { "请先停止录音，再修改 MQTT 配置" }
                                vault.save(mqtt)
                            }
                            AudioCaptureService.start(this)
                            result.success(true)
                        }
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
                    "workbenchFullscreen" -> {
                        setMatrixFullscreen(call.arguments == true, landscape = Build.MODEL == "NFCD_AlipayBoxN5DEL")
                        result.success(null)
                    }
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
                        require(!AudioCaptureService.active || old == config) { "请先停止录音，再修改 MQTT 配置" }
                        ConfigVault(this).save(config)
                        if (old?.nodeId != config.nodeId || old.uri != config.uri) ViewStore(this).clear()
                        // A running service reloads on its serialized worker; no overlapping client IDs.
                        if (OrbitService.active) startService(Intent(this, OrbitService::class.java))
                        AudioCaptureService.start(this)
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
    private fun requestAudioPermission() {
        val permissions = mutableListOf(Manifest.permission.RECORD_AUDIO)
        if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            permissions.add(Manifest.permission.POST_NOTIFICATIONS)
        }
        requestPermissions(permissions.toTypedArray(), 42)
    }
    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode == 42 && grantResults.firstOrNull() == PackageManager.PERMISSION_GRANTED) {
            AudioCaptureService.start(this)
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
