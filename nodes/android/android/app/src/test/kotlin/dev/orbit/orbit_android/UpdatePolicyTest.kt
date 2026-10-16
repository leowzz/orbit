package dev.orbit.orbit_android

import org.junit.Assert.*
import org.junit.Test

class UpdatePolicyTest {
    @Test fun versionsAreNumericAndStableOnly() {
        assertTrue(UpdatePolicy.version("v0.3.10") > UpdatePolicy.version("0.3.9"))
        assertEquals(UpdatePolicy.version("0.3.1"), UpdatePolicy.version("0.3.1+24"))
        for (value in listOf("0.3.2-rc.1", "garbage", "0.03.2", "0.3")) {
            assertThrows(IllegalArgumentException::class.java) { UpdatePolicy.version(value) }
        }
    }
    @Test fun assetsMustBelongToTheReleaseAndHaveADigest() {
        val tag = "v0.3.2"
        val name = "orbit-android-$tag.apk"
        val url = "https://github.com/leowzz/orbit/releases/download/$tag/$name"
        val digest = "sha256:" + "a".repeat(64)
        UpdatePolicy.validateAsset(tag, name, url, digest, 42)
        assertThrows(IllegalArgumentException::class.java) { UpdatePolicy.validateAsset(tag, name, "https://example.com/app.apk", digest, 42) }
        assertThrows(IllegalArgumentException::class.java) { UpdatePolicy.validateAsset(tag, name, url, "", 42) }
        assertThrows(IllegalArgumentException::class.java) { UpdatePolicy.validateAsset(tag, name, url, digest, UpdatePolicy.MAX_PACKAGE + 1) }
    }
    @Test fun corruptOrIncompleteDownloadsAreRejected() {
        val hash = "a".repeat(64)
        UpdatePolicy.verifyDownload(42, "sha256:$hash", 42, hash)
        assertThrows(IllegalArgumentException::class.java) { UpdatePolicy.verifyDownload(42, "sha256:$hash", 41, hash) }
        assertThrows(IllegalArgumentException::class.java) { UpdatePolicy.verifyDownload(42, "sha256:$hash", 42, "b".repeat(64)) }
    }
    @Test fun selectsThePreferredDeviceAbiBeforeTheUniversalAsset() {
        val tag = "v0.3.4"
        val universal = "orbit-android-$tag.apk"
        val arm64 = "orbit-android-arm64-v8a-$tag.apk"
        val arm = "orbit-android-armeabi-v7a-$tag.apk"
        val x64 = "orbit-android-x86_64-$tag.apk"
        val assets = setOf(universal, arm, x64, arm64)
        assertEquals(arm64, UpdatePolicy.selectAssetName(tag, assets, listOf("arm64-v8a", "armeabi-v7a")))
        assertEquals(arm, UpdatePolicy.selectAssetName(tag, assets, listOf("armeabi-v7a")))
        assertEquals(x64, UpdatePolicy.selectAssetName(tag, assets, listOf("x86_64", "x86")))
        assertEquals(arm, UpdatePolicy.selectAssetName(tag, setOf(arm, universal), listOf("arm64-v8a", "armeabi-v7a")))
        assertEquals(universal, UpdatePolicy.selectAssetName(tag, setOf(universal), listOf("arm64-v8a")))
        assertNull(UpdatePolicy.selectAssetName(tag, setOf(x64), listOf("arm64-v8a")))
        assertNull(UpdatePolicy.selectAssetName(tag, setOf("orbit-android-mips-$tag.apk"), listOf("mips")))
    }
    @Test fun splitAssetsMustMatchADeviceAbiAndTheExactReleaseUrl() {
        val tag = "v0.3.4"
        val name = "orbit-android-arm64-v8a-$tag.apk"
        val url = "https://github.com/leowzz/orbit/releases/download/$tag/$name"
        val digest = "sha256:" + "a".repeat(64)
        UpdatePolicy.validateAsset(tag, name, url, digest, 42, listOf("arm64-v8a"))
        assertThrows(IllegalArgumentException::class.java) {
            UpdatePolicy.validateAsset(tag, name, url, digest, 42, listOf("x86_64"))
        }
        assertThrows(IllegalArgumentException::class.java) {
            UpdatePolicy.validateAsset(tag, name, "https://example.com/$name", digest, 42, listOf("arm64-v8a"))
        }
        assertThrows(IllegalArgumentException::class.java) {
            UpdatePolicy.validateAsset(tag, name, url, "", 42, listOf("arm64-v8a"))
        }
    }
}
