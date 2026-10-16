package dev.orbit.orbit_android

/** Pure release policy, shared by metadata selection and pre-install validation. */
object UpdatePolicy {
    const val MAX_PACKAGE = 512L * 1024 * 1024
    data class Version(val major: Long, val minor: Long, val patch: Long) : Comparable<Version> {
        override fun compareTo(other: Version): Int = compareValuesBy(this, other, { it.major }, { it.minor }, { it.patch })
    }
    fun version(raw: String): Version {
        val match = Regex("^v?(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)(?:\\+[0-9A-Za-z.-]+)?$")
            .matchEntire(raw) ?: throw IllegalArgumentException("无法识别正式版本号")
        return Version(match.groupValues[1].toLong(), match.groupValues[2].toLong(), match.groupValues[3].toLong())
    }
    fun validateAsset(tag: String, name: String, url: String, digest: String, size: Long) {
        version(tag)
        require(name == "orbit-android-$tag.apk" &&
            url == "https://github.com/leowzz/orbit/releases/download/$tag/$name" &&
            Regex("^sha256:[0-9a-f]{64}$").matches(digest) && size in 1..MAX_PACKAGE) {
            "更新包信息无效，请稍后重试"
        }
    }
    fun verifyDownload(expectedSize: Long, expectedDigest: String, actualSize: Long, actualDigest: String) {
        require(actualSize == expectedSize && expectedDigest == "sha256:$actualDigest") {
            "更新包校验失败，未安装任何文件，请重试"
        }
    }
}
