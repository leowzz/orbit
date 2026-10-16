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
}
