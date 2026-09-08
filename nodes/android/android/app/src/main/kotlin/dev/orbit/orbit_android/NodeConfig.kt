package dev.orbit.orbit_android

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import org.json.JSONObject
import java.net.URI
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

data class NodeConfig(val uri: String, val nodeId: String, val username: String, val password: String) {
    fun validate() {
        val parsed = URI(uri)
        require(parsed.scheme in listOf("ssl", "tcp") && !parsed.host.isNullOrBlank() &&
            parsed.port in 1..65535 && parsed.userInfo == null && parsed.query == null &&
            parsed.fragment == null && parsed.path.orEmpty().isEmpty()) {
            "地址格式为 ssl://主机:8883 或 tcp://主机:1883"
        }
        require(Regex("[A-Za-z0-9_-]{1,64}").matches(nodeId)) { "Node ID 仅支持 1–64 位字母、数字、下划线和短横线" }
        require(password.isEmpty() || username.isNotBlank()) { "使用密码时需要用户名" }
    }
    fun map(): Map<String, String> = mapOf("uri" to uri, "nodeId" to nodeId, "username" to username, "password" to password)
    companion object {
        fun from(map: Map<*, *>): NodeConfig = NodeConfig(
            map["uri"]?.toString()?.trim().orEmpty().replaceFirst(Regex("^mqtts://"), "ssl://").replaceFirst(Regex("^mqtt://"), "tcp://"), map["nodeId"]?.toString()?.trim().orEmpty(),
            map["username"]?.toString().orEmpty(), map["password"]?.toString().orEmpty())
    }
}

// Encrypt the entire configuration with a non-exportable Android Keystore key.
class ConfigVault(private val context: Context) {
    private val prefs get() = context.getSharedPreferences("orbit_config", Context.MODE_PRIVATE)
    private fun key(): SecretKey {
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (store.getKey("orbit_mqtt", null) as? SecretKey)?.let { return it }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply {
            init(KeyGenParameterSpec.Builder("orbit_mqtt", KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
        }.generateKey()
    }
    fun save(config: NodeConfig) {
        config.validate()
        val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.ENCRYPT_MODE, key()) }
        val encrypted = cipher.doFinal(JSONObject(config.map()).toString().toByteArray(Charsets.UTF_8))
        check(prefs.edit().putString("data", Base64.encodeToString(cipher.iv + encrypted, Base64.NO_WRAP)).commit())
    }
    fun load(): NodeConfig? {
        val encoded = prefs.getString("data", null) ?: return null
        val bytes = Base64.decode(encoded, Base64.NO_WRAP)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply {
            init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, bytes.copyOfRange(0, 12)))
        }
        val json = JSONObject(String(cipher.doFinal(bytes.copyOfRange(12, bytes.size)), Charsets.UTF_8))
        return NodeConfig.from(json.keys().asSequence().associateWith { json.getString(it) })
    }
}
