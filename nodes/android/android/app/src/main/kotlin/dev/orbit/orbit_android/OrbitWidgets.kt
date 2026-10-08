package dev.orbit.orbit_android

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.graphics.Paint
import android.os.Bundle
import android.os.Build
import android.util.SizeF
import android.util.TypedValue
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
                val options = manager.getAppWidgetOptions(id)
                val portrait = SizeF(options.getInt(AppWidgetManager.OPTION_APPWIDGET_MIN_WIDTH, 180).toFloat(),
                    options.getInt(AppWidgetManager.OPTION_APPWIDGET_MAX_HEIGHT, 150).toFloat())
                val landscape = SizeF(options.getInt(AppWidgetManager.OPTION_APPWIDGET_MAX_WIDTH, 280).toFloat(),
                    options.getInt(AppWidgetManager.OPTION_APPWIDGET_MIN_HEIGHT, 110).toFloat())
                @Suppress("DEPRECATION")
                val sizes = if (Build.VERSION.SDK_INT >= 31)
                    options.getParcelableArrayList<SizeF>(AppWidgetManager.OPTION_APPWIDGET_SIZES)
                        ?.filter { it.width > 0 && it.height > 0 }?.distinct()?.take(16).orEmpty() else emptyList()
                val key = mutableListOf<Any>(sizes.ifEmpty { listOf(portrait, landscape) }, context.resources.configuration.fontScale)
                var widgetHint = ""
                fun render(size: SizeF): RemoteViews {
                    val layout = WidgetSizing.session(size.width, size.height, context.resources.configuration.fontScale)
                    val sideMetrics = usage && size.width >= 180 && size.height <= 110
                    val views = RemoteViews(context.packageName, when {
                        sideMetrics -> R.layout.usage_widget_wide
                        usage -> R.layout.usage_widget
                        else -> R.layout.session_widget
                    })
                    fun pixels(dp: Int) = (dp * context.resources.displayMetrics.density).toInt()
                    val vertical = if (usage) (size.height / 12).toInt().coerceIn(4, 18) else layout.padding
                    views.setViewPadding(R.id.widget_root, pixels(layout.padding), pixels(vertical), pixels(layout.padding), pixels(vertical))
                    views.setTextViewText(R.id.widget_label, if (usage) "用量" else if (size.width < 180) "会话" else "Sessions")
                    views.setTextViewTextSize(R.id.widget_label, TypedValue.COMPLEX_UNIT_SP, layout.textSize)
                    views.setTextViewTextSize(R.id.widget_hint, TypedValue.COMPLEX_UNIT_SP, (layout.textSize - 2).coerceAtLeast(9f))
                    if (usage) {
                        val value = data?.takeIf { it.hasUsage() }?.usage
                        val amount = if (value?.hasActualCostMicros() == true)
                            WidgetPresentation.cost(value.actualCostMicros, value.currencyCode) else "—"
                        widgetHint = if (value == null) "暂无数据" else hint(value.freshness, value.freshUntil, now)
                        views.setTextViewText(R.id.widget_amount, amount)
                        views.setTextViewText(R.id.widget_hint, widgetHint)
                        val tok = "TOK  " + if (value?.hasTokenCount() == true) WidgetPresentation.metric(value.tokenCount) else "—"
                        val tpm = "TPM  " + if (value?.hasTpm() == true) WidgetPresentation.metric(value.tpm) else "—"
                        val inline = "$tok    $tpm"
                        val metrics = context.resources.displayMetrics
                        val paint = Paint().apply { textSize = TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_SP, 11f, metrics) }
                        val width = (size.width - layout.padding * 2) * metrics.density
                        val lines = when {
                            paint.measureText(inline) <= width -> 1
                            maxOf(paint.measureText(tok), paint.measureText(tpm)) <= width -> 2
                            else -> 0
                        }
                        // Reserve readable amount/header space before adding optional secondary metrics.
                        val scale = context.resources.configuration.fontScale.coerceAtLeast(1f)
                        val requiredHeight = vertical * 2 + (layout.textSize * 1.5f + 42f) * scale +
                            (paint.fontSpacing / metrics.density) * lines + 4
                        val showMetrics = value != null && (sideMetrics || (lines > 0 && size.height >= requiredHeight))
                        val detail = if (showMetrics) { if (!sideMetrics && lines == 1) inline else "$tok\n$tpm" } else ""
                        views.setViewVisibility(R.id.widget_metrics, if (showMetrics) View.VISIBLE else View.GONE)
                        views.setTextViewText(R.id.widget_metrics, detail)
                        key.add(detail)
                        key.add(amount)
                        key.add(widgetHint)
                    } else {
                        val value = data?.takeIf { it.hasCodex() }?.codex
                        val state = if (value == null) "暂无数据" else hint(value.freshness, value.freshUntil, now)
                        widgetHint = state.ifBlank { "${value?.runningCount ?: 0} 运行中" }
                        views.setTextViewText(R.id.widget_hint, widgetHint)
                        key.add(widgetHint)
                        views.removeAllViews(R.id.widget_sessions)
                        val sessions = WidgetPresentation.sessions(value?.sessionsList.orEmpty(), layout.rows)
                        views.setViewVisibility(R.id.widget_empty, if (sessions.isEmpty()) View.VISIBLE else View.GONE)
                        for (session in sessions) {
                            val row = RemoteViews(context.packageName, R.layout.session_widget_row)
                            row.setViewPadding(R.id.session_row, 0, pixels(4), 0, pixels(4))
                            row.setTextViewTextSize(R.id.session_status, TypedValue.COMPLEX_UNIT_SP, layout.textSize - 1)
                            row.setTextViewTextSize(R.id.session_name, TypedValue.COMPLEX_UNIT_SP, layout.textSize)
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
                    views.setOnClickPendingIntent(R.id.widget_root, open)
                    return views
                }
                val views = if (Build.VERSION.SDK_INT >= 31 && sizes.isNotEmpty()) {
                    RemoteViews(sizes.associateWith { render(it) })
                } else {
                    RemoteViews(render(landscape), render(portrait))
                }
                val elapsed = SystemClock.elapsedRealtime()
                val previous = rendered[id]
                if (!force && previous != null) {
                    if (previous.key == key) continue
                    if (usage && previous.hint == widgetHint && elapsed - previous.at < 60000) continue
                }
                rendered[id] = Rendered(key, widgetHint, elapsed)
                manager.updateAppWidget(id, views)
            }
        }
    }
}
