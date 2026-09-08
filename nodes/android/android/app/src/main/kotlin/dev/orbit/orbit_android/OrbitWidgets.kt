package dev.orbit.orbit_android

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.os.PowerManager
import android.os.SystemClock
import android.view.View
import android.widget.RemoteViews
import com.google.protobuf.Timestamp
import orbit.v1.View.Freshness

class UsageWidget : AppWidgetProvider() {
    override fun onDisabled(context: Context) { OrbitService.reconcile() }
    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) { OrbitWidgets.updateAll(context, force = true) }
    override fun onAppWidgetOptionsChanged(context: Context, manager: AppWidgetManager, id: Int, options: Bundle) { OrbitWidgets.updateAll(context, force = true) }
}
class SessionWidget : AppWidgetProvider() {
    override fun onDisabled(context: Context) { OrbitService.reconcile() }
    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) { OrbitWidgets.updateAll(context, force = true) }
    override fun onAppWidgetOptionsChanged(context: Context, manager: AppWidgetManager, id: Int, options: Bundle) { OrbitWidgets.updateAll(context, force = true) }
}
object OrbitWidgets {
    private data class Rendered(val key: List<Any>, val hint: String, val at: Long)
    private val rendered = mutableMapOf<Int, Rendered>()
    fun hasWidgets(context: Context): Boolean {
        val manager = AppWidgetManager.getInstance(context)
        return listOf(UsageWidget::class.java, SessionWidget::class.java).any {
            manager.getAppWidgetIds(ComponentName(context, it)).isNotEmpty()
        }
    }
    private fun hint(freshness: Freshness, until: Timestamp, now: Long): String = when {
        !OrbitService.active -> "已停止"
        freshness != Freshness.FRESHNESS_FRESH || ViewPolicy.millis(until) <= now -> "已过期"
        !OrbitService.connection.startsWith("已连接") -> "离线"
        else -> ""
    }
    @Synchronized
    fun updateAll(context: Context, force: Boolean = false) {
        if (!context.getSystemService(PowerManager::class.java).isInteractive) return
        val manager = AppWidgetManager.getInstance(context)
        val now = System.currentTimeMillis()
        val data = ViewStore(context).load()?.takeIf { ViewPolicy.millis(it.retainUntil) > now }
        val open = PendingIntent.getActivity(context, 0, Intent(context, MainActivity::class.java), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
        for ((provider, usage) in listOf(UsageWidget::class.java to true, SessionWidget::class.java to false)) {
            for (id in manager.getAppWidgetIds(ComponentName(context, provider))) {
                val views = RemoteViews(context.packageName, if (usage) R.layout.usage_widget else R.layout.session_widget)
                val key = mutableListOf<Any>()
                var widgetHint: String
                if (usage) {
                    val value = data?.takeIf { it.hasUsage() }?.usage
                    val amount = if (value?.hasActualCostMicros() == true)
                        WidgetPresentation.cost(value.actualCostMicros, value.currencyCode) else "—"
                    widgetHint = if (value == null) "暂无数据" else hint(value.freshness, value.freshUntil, now)
                    views.setTextViewText(R.id.widget_amount, amount)
                    views.setTextViewText(R.id.widget_hint, widgetHint)
                    key.add(amount)
                    key.add(widgetHint)
                } else {
                    val value = data?.takeIf { it.hasCodex() }?.codex
                    val state = if (value == null) "暂无数据" else hint(value.freshness, value.freshUntil, now)
                    widgetHint = state.ifBlank { "${value?.runningCount ?: 0} 运行中" }
                    views.setTextViewText(R.id.widget_hint, widgetHint)
                    key.add(widgetHint)
                    views.removeAllViews(R.id.widget_sessions)
                    // Use the smaller offered height so all rows also fit after rotation.
                    val height = manager.getAppWidgetOptions(id).getInt(AppWidgetManager.OPTION_APPWIDGET_MIN_HEIGHT, 150)
                    val limit = ((height - 44) / 28).coerceIn(1, 6)
                    val sessions = WidgetPresentation.sessions(value?.sessionsList.orEmpty(), limit)
                    views.setViewVisibility(R.id.widget_empty, if (sessions.isEmpty()) View.VISIBLE else View.GONE)
                    for (session in sessions) {
                        val row = RemoteViews(context.packageName, R.layout.session_widget_row)
                        val (label, color) = WidgetPresentation.status(session.statusValue)
                        row.setTextViewText(R.id.session_status, label)
                        row.setTextColor(R.id.session_status, if (state.isBlank()) color else 0xFF92988F.toInt())
                        val name = session.displayName.ifBlank { session.projectName }
                            .ifBlank { session.model }.ifBlank { session.sessionId.take(8) }.take(80)
                        row.setTextViewText(R.id.session_name, name)
                        key.add(listOf(label, color, name, state))
                        views.addView(R.id.widget_sessions, row)
                    }
                }
                val elapsed = SystemClock.elapsedRealtime()
                val previous = rendered[id]
                if (!force && previous != null) {
                    if (previous.key == key) continue
                    if (usage && previous.hint == widgetHint && elapsed - previous.at < 60000) continue
                }
                rendered[id] = Rendered(key, widgetHint, elapsed)
                views.setOnClickPendingIntent(R.id.widget_root, open)
                manager.updateAppWidget(id, views)
            }
        }
    }
}
