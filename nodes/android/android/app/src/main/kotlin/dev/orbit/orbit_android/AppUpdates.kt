package dev.orbit.orbit_android

import android.content.Context
import android.content.pm.PackageInfo
import android.content.pm.PackageManager
import android.os.Build
import org.json.JSONObject
import java.io.File
import java.io.ByteArrayOutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import java.util.concurrent.atomic.AtomicBoolean

/** All network and file work runs on MainActivity's worker, never on the UI thread. */
@Suppress("DEPRECATION")
class AppUpdates(private val context: Context) {
    private data class Asset(val version: String, val url: String, val digest: String, val size: Long)
    private val operation = AtomicBoolean(false)
    private val lock = Any()
    private val installed = context.packageManager.getPackageInfo(context.packageName, signatureFlags())
    private var selected: Asset? = null
    private var ready: Asset? = null
    private val values = mutableMapOf<String, Any?>(
        "currentVersion" to (installed.versionName ?: "未知"), "latestVersion" to null,
        "available" to false, "supported" to (context.packageName == "dev.orbit.orbit_android"),
        "phase" to "idle", "downloaded" to 0L, "total" to 0L, "error" to "",
    )
    private val apk get() = File(context.cacheDir, "updates/orbit-update.apk")
    fun status(): Map<String, Any?> = synchronized(lock) { values.toMap() }
    fun phase(phase: String) = synchronized(lock) { values["phase"] = phase }
    fun failed(message: String) = synchronized(lock) { values["phase"] = "idle"; values["error"] = message }
    private fun set(vararg pairs: Pair<String, Any?>) = synchronized(lock) { values.putAll(pairs) }

    private fun <T> exclusive(body: () -> T): T {
        check(operation.compareAndSet(false, true)) { "更新操作正在进行" }
        try { return body() }
        catch (error: Exception) {
            val message = if (error is IllegalArgumentException || error is IllegalStateException)
                error.message ?: "更新失败，请重试" else "更新失败，请检查网络和存储空间后重试"
            failed(message)
            throw IllegalStateException(message)
        } finally { operation.set(false) }
    }

    fun check(): Map<String, Any?> = exclusive {
        set("phase" to "checking", "error" to "")
        val connection = connect("https://api.github.com/repos/leowzz/orbit/releases/latest")
        val release = try {
            require(connection.responseCode == 200) { "更新服务暂不可用，请稍后重试" }
            val bytes = connection.inputStream.use { input ->
                val output = ByteArrayOutputStream()
                val buffer = ByteArray(8192)
                while (true) {
                    val count = input.read(buffer)
                    if (count < 0) break
                    require(output.size() + count <= 1024 * 1024) { "更新信息无效" }
                    output.write(buffer, 0, count)
                }
                output.toByteArray()
            }
            JSONObject(String(bytes, Charsets.UTF_8))
        } finally { connection.disconnect() }
        require(!release.getBoolean("draft") && !release.getBoolean("prerelease")) { "暂时没有可用的正式版本" }
        val tag = release.getString("tag_name")
        val version = UpdatePolicy.version(tag)
        selected = null
        set("available" to false, "latestVersion" to tag.removePrefix("v"))
        if (version > UpdatePolicy.version(installed.versionName ?: "")) {
            val assets = release.getJSONArray("assets")
            val asset = (0 until assets.length()).map { assets.getJSONObject(it) }
                .firstOrNull { it.optString("name") == "orbit-android-$tag.apk" }
                ?: throw IllegalArgumentException("此版本尚未提供 Android 更新包，请稍后重试")
            val url = asset.getString("browser_download_url")
            val digest = asset.optString("digest")
            val size = asset.getLong("size")
            UpdatePolicy.validateAsset(tag, asset.getString("name"), url, digest, size)
            selected = Asset(tag.removePrefix("v"), url, digest, size)
            set("available" to (context.packageName == "dev.orbit.orbit_android"))
        }
        set("phase" to "idle")
        status()
    }

    fun download() = exclusive {
        require(context.packageName == "dev.orbit.orbit_android") { "此测试应用不支持覆盖正式版" }
        val asset = selected ?: throw IllegalStateException("请先检查更新")
        // A permission prompt or cancelled installer can reuse the verified download.
        if (ready == asset && apk.isFile) {
            try {
                val digest = MessageDigest.getInstance("SHA-256")
                apk.inputStream().use { input ->
                    val buffer = ByteArray(64 * 1024)
                    while (true) {
                        val count = input.read(buffer)
                        if (count < 0) break
                        digest.update(buffer, 0, count)
                    }
                }
                UpdatePolicy.verifyDownload(asset.size, asset.digest, apk.length(), hex(digest.digest()))
                verifyApk(asset)
            } catch (error: Exception) {
                ready = null
                apk.delete()
                throw error
            }
            phase("ready")
            return@exclusive
        }
        ready = null
        set("phase" to "downloading", "downloaded" to 0L, "total" to asset.size, "error" to "")
        require(apk.parentFile!!.isDirectory || apk.parentFile!!.mkdirs()) { "无法创建更新目录" }
        val partial = File(apk.parentFile, "download.part")
        val connection = connect(asset.url)
        try {
            require(connection.responseCode == 200) { "更新包暂不可用，请稍后重试" }
            var size = 0L
            val digest = MessageDigest.getInstance("SHA-256")
            val started = android.os.SystemClock.elapsedRealtime()
            connection.inputStream.use { input ->
                partial.outputStream().use { output ->
                    val buffer = ByteArray(64 * 1024)
                    while (true) {
                        val count = input.read(buffer)
                        if (count < 0) break
                        size += count
                        require(size <= asset.size && size <= UpdatePolicy.MAX_PACKAGE) { "更新包大小不符" }
                        require(android.os.SystemClock.elapsedRealtime() - started < 15 * 60 * 1000) { "下载超时，请重试" }
                        digest.update(buffer, 0, count)
                        output.write(buffer, 0, count)
                        set("downloaded" to size)
                    }
                    output.fd.sync()
                }
            }
            UpdatePolicy.verifyDownload(asset.size, asset.digest, size, hex(digest.digest()))
            require(!apk.exists() || apk.delete()) { "无法替换更新缓存" }
            require(partial.renameTo(apk)) { "无法保存更新包" }
            phase("verifying")
            verifyApk(asset)
            ready = asset
            phase("ready")
        } catch (error: Exception) {
            apk.delete()
            throw error
        } finally {
            partial.delete()
            connection.disconnect()
        }
    }

    fun readyFile(): File {
        check(ready != null && apk.isFile) { "请重新下载更新包" }
        return apk
    }

    private fun verifyApk(asset: Asset) {
        val archive = context.packageManager.getPackageArchiveInfo(apk.path, signatureFlags())
            ?: throw IllegalArgumentException("安装包无效，请重新下载")
        require(archive.packageName == context.packageName && archive.versionName == asset.version &&
            code(archive) > code(installed)) { "安装包的应用或版本不符，无法覆盖当前应用" }
        val currentSigners = signatures(installed)
        val nextSigners = signatures(archive)
        require(currentSigners.isNotEmpty() && currentSigners == nextSigners) {
            "安装包签名与当前应用不同，无法直接更新。请使用相同签名的发布版本"
        }
    }

    private fun code(info: PackageInfo): Long = if (Build.VERSION.SDK_INT >= 28) info.longVersionCode else info.versionCode.toLong()
    private fun signatureFlags() = if (Build.VERSION.SDK_INT >= 28) PackageManager.GET_SIGNING_CERTIFICATES else PackageManager.GET_SIGNATURES
    private fun signatures(info: PackageInfo): Set<String> {
        val values = if (Build.VERSION.SDK_INT >= 28) info.signingInfo?.apkContentsSigners else info.signatures
        return values?.map { hex(MessageDigest.getInstance("SHA-256").digest(it.toByteArray())) }?.toSet() ?: emptySet()
    }
    private fun hex(bytes: ByteArray) = bytes.joinToString("") { "%02x".format(it) }

    private fun connect(address: String): HttpURLConnection {
        var url = URL(address)
        repeat(6) {
            require(url.protocol == "https" && (url.host == "api.github.com" || url.host == "github.com" ||
                url.host.endsWith(".githubusercontent.com"))) { "更新下载地址无效" }
            val connection = (url.openConnection() as HttpURLConnection).apply {
                connectTimeout = 15000
                readTimeout = 30000
                instanceFollowRedirects = false
                setRequestProperty("User-Agent", "Orbit-Android-Updater")
            }
            val code = try { connection.responseCode } catch (error: Exception) { connection.disconnect(); throw error }
            if (code !in listOf(301, 302, 303, 307, 308)) return connection
            val location = connection.getHeaderField("Location")
            connection.disconnect()
            require(!location.isNullOrBlank()) { "更新下载地址无效" }
            url = URL(url, location)
        }
        throw IllegalArgumentException("更新下载跳转过多，请稍后重试")
    }
}
