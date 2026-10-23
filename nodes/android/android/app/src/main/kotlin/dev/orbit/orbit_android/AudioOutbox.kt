package dev.orbit.orbit_android

import java.io.File
import java.io.FileOutputStream
import java.util.UUID

/** Persist until the local QoS 0 send completes. Atomic rename hides partial writes. */
class AudioOutbox(
    private val directory: File,
    private val maxBytes: Long = 64L * 1024 * 1024,
    private val maxEntries: Int = 1000,
    private val budgetDirectory: File = directory,
) {
    companion object {
        // Capture and upload services open separate instances of the same queue.
        private val lock = Any()
        /** Clear persisted segments for every connection, serialized with capture writes. */
        fun clearAll(root: File): Int = synchronized(lock) {
            if (!root.exists()) return@synchronized 0
            var removed = 0
            root.walkTopDown().onFail { _, error -> throw error }.filter {
                it.isFile && (it.name.endsWith(".json") || it.name.endsWith(".tmp"))
            }.forEach {
                check(it.delete()) { "Cannot clear audio outbox" }
                if (it.name.endsWith(".json")) removed++
            }
            removed
        }
    }
    init {
        synchronized(lock) {
            check(directory.isDirectory || directory.mkdirs()) { "Cannot create audio outbox" }
            directory.listFiles { f -> f.name.endsWith(".tmp") }?.forEach { check(it.delete()) }
        }
    }
    private fun entries(): List<File> = directory.listFiles { f -> f.name.endsWith(".json") }
        ?.map { it to it.lastModified() }
        ?.sortedWith(compareByDescending<Pair<File, Long>> { it.second }.thenBy { it.first.name })
        ?.map { it.first } ?: error("Cannot read audio outbox")
    fun count(): Int = synchronized(lock) {
        directory.listFiles { f -> f.name.endsWith(".json") }?.size ?: error("Cannot read audio outbox")
    }
    fun put(id: String, payload: ByteArray): Unit = synchronized(lock) {
        require(UUID.fromString(id).toString() == id)
        require(payload.size in 1..131072)
        val target = File(directory, "$id.json")
        if (target.exists()) {
            check(target.readBytes().contentEquals(payload)) { "Segment ID conflict" }
            return@synchronized
        }
        val files = budgetDirectory.walkTopDown().filter { it.isFile && it.name.endsWith(".json") }.toList()
        check(files.size < maxEntries && files.sumOf { it.length() } + payload.size <= maxBytes) { "Audio outbox full" }
        val temp = File(directory, "$id.tmp")
        try {
            FileOutputStream(temp).use { it.write(payload); it.fd.sync() }
            check(temp.renameTo(target)) { "Cannot persist audio segment" }
        } finally { if (temp.exists()) temp.delete() }
    }
    fun pending(limit: Int = 16, exclude: Set<String> = emptySet()): List<Pair<String, ByteArray>> = synchronized(lock) {
        entries().asSequence().filter { it.name.removeSuffix(".json") !in exclude }.take(limit).map {
            check(it.length() in 1..131072) { "Invalid audio outbox entry" }
            it.name.removeSuffix(".json") to it.readBytes()
        }.toList()
    }
    fun sent(id: String): Unit = synchronized(lock) {
        require(UUID.fromString(id).toString() == id)
        val file = File(directory, "$id.json")
        if (file.exists()) check(file.delete()) { "Cannot remove sent audio segment" }
    }
}
