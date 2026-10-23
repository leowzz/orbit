package dev.orbit.orbit_android

import java.io.File
import java.io.FileOutputStream
import java.util.UUID

/** Application ACK is the only removal path. Atomic rename hides partial writes. */
class AudioOutbox(
    private val directory: File,
    private val maxBytes: Long = 64L * 1024 * 1024,
    private val maxEntries: Int = 1000,
    private val budgetDirectory: File = directory,
) {
    init {
        check(directory.isDirectory || directory.mkdirs()) { "Cannot create audio outbox" }
        directory.listFiles { f -> f.name.endsWith(".tmp") }?.forEach { check(it.delete()) }
    }
    private fun entries(): List<File> = directory.listFiles { f -> f.name.endsWith(".json") }
        ?.sortedBy { it.name } ?: error("Cannot read audio outbox")
    @Synchronized fun count(): Int = entries().size
    @Synchronized fun put(id: String, payload: ByteArray) {
        require(UUID.fromString(id).toString() == id)
        require(payload.size in 1..131072)
        val target = File(directory, "$id.json")
        if (target.exists()) {
            check(target.readBytes().contentEquals(payload)) { "Segment ID conflict" }
            return
        }
        val files = budgetDirectory.walkTopDown().filter { it.isFile && it.name.endsWith(".json") }.toList()
        check(files.size < maxEntries && files.sumOf { it.length() } + payload.size <= maxBytes) { "Audio outbox full" }
        val temp = File(directory, "$id.tmp")
        try {
            FileOutputStream(temp).use { it.write(payload); it.fd.sync() }
            check(temp.renameTo(target)) { "Cannot persist audio segment" }
        } finally { if (temp.exists()) temp.delete() }
    }
    @Synchronized fun pending(limit: Int = 4): List<Pair<String, ByteArray>> = entries().take(limit).map {
        check(it.length() in 1..131072) { "Invalid audio outbox entry" }
        it.name.removeSuffix(".json") to it.readBytes()
    }
    @Synchronized fun acknowledge(id: String) {
        require(UUID.fromString(id).toString() == id)
        val file = File(directory, "$id.json")
        if (file.exists()) check(file.delete()) { "Cannot acknowledge audio segment" }
    }
}
