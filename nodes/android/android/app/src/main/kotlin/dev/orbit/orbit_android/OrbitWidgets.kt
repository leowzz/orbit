package dev.orbit.orbit_android

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.view.View
import android.widget.RemoteViews
import com.google.protobuf.Timestamp
import orbit.v1.View.Freshness

class UsageWidget : AppWidgetProvider() {
    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) { OrbitWidgets.updateAll(context) }
    override fun onAppWidgetOptionsChanged(context: Context, manager: AppWidgetManager, id: Int, options: Bundle) { OrbitWidgets.updateAll(context) }
}
class SessionWidget : AppWidgetProvider() {
    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) { OrbitWidgets.updateAll(context) }
    override fun onAppWidgetOptionsChanged(context: Context, manager: AppWidgetManager, id: Int, options: Bundle) { OrbitWidgets.updateAll(context) }
}
object OrbitWidgets {
    private fun hint(freshness: Freshness, until: Timestamp, now: Long): String = when {
        !OrbitService.active -> "已停止"
        freshness != Freshness.FRESHNESS_FRESH || ViewPolicy.millis(until) <= now -> "已过期"
        !OrbitService.connection.startsWith("已连接") -> "离线"
        else -> ""
    }
    fun updateAll(context: Context) {
        val manager = AppWidgetManager.getInstance(context)
        val now = System.currentTimeMillis()
        val data = ViewStore(context).load()?.takeIf { ViewPolicy.millis(it.retainUntil) > now }
        val open = PendingIntent.getActivity(context, 0, Intent(context, MainActivity::class.java), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
        for ((provider, usage) in listOf(UsageWidget::class.java to true, SessionWidget::class.java to false)) {
            for (id in manager.getAppWidgetIds(ComponentName(context, provider))) {
                val views = RemoteViews(context.packageName, if (usage) R.layout.usage_widget else R.layout.session_widget)
                if (usage) {
                    val value = data?.takeIf { it.hasUsage() }?.usage
                    views.setTextViewText(R.id.widget_amount, if (value?.hasActualCostMicros() == true)
                        WidgetPresentation.cost(value.actualCostMicros, value.currencyCode) else "—")
                    views.setTextViewText(R.id.widget_hint, if (value == null) "暂无数据" else hint(value.freshness, value.freshUntil, now))
                } else {
                    val value = data?.takeIf { it.hasCodex() }?.codex
                    val state = if (value == null) "暂无数据" else hint(value.freshness, value.freshUntil, now)
                    views.setTextViewText(R.id.widget_hint, state.ifBlank { "${value?.runningCount ?: 0} 运行中" })
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
                        row.setTextViewText(R.id.session_name, session.displayName.ifBlank { session.projectName }
                            .ifBlank { session.model }.ifBlank { session.sessionId.take(8) }.take(80))
                        views.addView(R.id.widget_sessions, row)
                    }
                }
                views.setOnClickPendingIntent(R.id.widget_root, open)
                manager.updateAppWidget(id, views)
            }
        }
    }
}
