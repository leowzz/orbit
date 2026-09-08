package dev.orbit.orbit_android

import android.content.Context
import android.util.Base64
import orbit.v1.View.DeviceView
import orbit.v1.View.Freshness
import com.google.protobuf.Timestamp
import java.text.NumberFormat
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

object ViewPolicy {
    fun millis(time: Timestamp): Long = time.seconds * 1000 + time.nanos / 1000000
    fun valid(next: DeviceView, previous: DeviceView?, nodeId: String, now: Long): Boolean {
        if (next.nodeId != nodeId || next.coreEpoch.isBlank() || !next.hasMetadata() ||
            next.metadata.revision <= 0 || next.metadata.producerId.isBlank() ||
            !next.metadata.hasProducedAt() || !next.hasRetainUntil() ||
            millis(next.retainUntil) <= now || millis(next.metadata.producedAt) > now + 60000) return false
        if (previous != null && previous.nodeId == nodeId) {
            if (previous.coreEpoch == next.coreEpoch && next.metadata.revision <= previous.metadata.revision) return false
            if (previous.coreEpoch != next.coreEpoch && millis(next.metadata.producedAt) <= millis(previous.metadata.producedAt)) return false
        }
        return true
    }
    fun freshness(value: Freshness, deadline: Timestamp, now: Long): String = when {
        value == Freshness.FRESHNESS_OFFLINE -> "离线"
        value != Freshness.FRESHNESS_FRESH || millis(deadline) <= now -> "数据已过期"
        else -> "数据正常"
    }
}

class ViewStore(context: Context) {
    private val prefs = context.getSharedPreferences("orbit_view", Context.MODE_PRIVATE)
    fun load(): DeviceView? = runCatching {
        prefs.getString("view", null)?.let { DeviceView.parseFrom(Base64.decode(it, Base64.NO_WRAP)) }
    }.getOrNull()
    fun clear() { prefs.edit().clear().commit() }
    fun accept(payload: ByteArray, nodeId: String, now: Long): Boolean {
        if (payload.size !in 1..32768) return false
        val next = runCatching { DeviceView.parseFrom(payload) }.getOrNull() ?: return false
        if (!ViewPolicy.valid(next, load(), nodeId, now)) return false
        prefs.edit().putString("view", Base64.encodeToString(payload, Base64.NO_WRAP)).apply()
        return true
    }
    fun snapshot(now: Long = System.currentTimeMillis()): Map<String, Any> {
        val view = load()
        val connection = if (OrbitService.active) OrbitService.connection else "已停止 · 打开应用连接"
        val base = mutableMapOf<String, Any>("connection" to connection, "active" to OrbitService.active)
        if (view == null || ViewPolicy.millis(view.retainUntil) <= now) {
            base.putAll(mapOf("usageTitle" to "—", "usageBody" to "等待用量数据", "usageState" to "暂无有效数据",
                "sessionTitle" to "—", "sessionBody" to "等待 Session 数据", "sessionState" to "暂无有效数据", "updated" to "尚未接收数据"))
            return base
        }
        base["updated"] = "更新于 " + DateTimeFormatter.ofPattern("MM-dd HH:mm:ss").withZone(ZoneId.systemDefault())
            .format(Instant.ofEpochMilli(ViewPolicy.millis(view.metadata.producedAt)))
        val number = NumberFormat.getIntegerInstance(Locale.US)
        if (view.hasUsage()) {
            val u = view.usage
            base["usageTitle"] = if (u.hasActualCostMicros()) "${u.currencyCode} ${String.format(Locale.US, "%.4f", u.actualCostMicros / 1000000.0)}" else "—"
            base["usageBody"] = "Tokens  ${if (u.hasTokenCount()) number.format(u.tokenCount) else "—"}\nTPM      ${if (u.hasTpm()) number.format(u.tpm) else "—"}"
            base["usageState"] = ViewPolicy.freshness(u.freshness, u.freshUntil, now)
        } else {
            base.putAll(mapOf("usageTitle" to "—", "usageBody" to "等待用量数据", "usageState" to "暂无数据"))
        }
        if (view.hasCodex()) {
            val c = view.codex
            base["sessionTitle"] = "${c.runningCount} 运行中 / ${c.totalCount} 总计"
            base["sessionBody"] = c.sessionsList.sortedBy { if (it.statusValue == 2) 0 else 1 }.take(4).joinToString("\n") {
                val label = it.displayName.ifBlank { it.projectName }.ifBlank { it.model }.ifBlank { it.sessionId.take(8) }
                val status = when (it.statusValue) { 2 -> "运行中"; 3 -> "已完成"; 4 -> "失败"; 5 -> "已中断"; 6 -> "已取消"; else -> "未知" }
                "$status · ${label.take(60)}"
            }.ifBlank { "暂无 Session" }
            base["sessionState"] = ViewPolicy.freshness(c.freshness, c.freshUntil, now)
        } else {
            base.putAll(mapOf("sessionTitle" to "—", "sessionBody" to "等待 Session 数据", "sessionState" to "暂无数据"))
        }
        return base
    }
}
