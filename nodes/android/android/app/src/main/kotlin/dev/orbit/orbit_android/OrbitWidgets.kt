package dev.orbit.orbit_android

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.widget.RemoteViews

class UsageWidget : AppWidgetProvider() {
    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) { OrbitWidgets.updateAll(context) }
}
class SessionWidget : AppWidgetProvider() {
    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) { OrbitWidgets.updateAll(context) }
}
object OrbitWidgets {
    fun updateAll(context: Context) {
        val manager = AppWidgetManager.getInstance(context)
        val data = ViewStore(context).snapshot()
        for ((provider, section) in listOf(UsageWidget::class.java to "usage", SessionWidget::class.java to "session")) {
            val ids = manager.getAppWidgetIds(ComponentName(context, provider))
            if (ids.isEmpty()) continue
            val views = RemoteViews(context.packageName, R.layout.orbit_widget)
            views.setTextViewText(R.id.widget_label, if (section == "usage") "ORBIT  /  用量" else "ORBIT  /  SESSION 状态")
            views.setTextViewText(R.id.widget_title, data[section + "Title"].toString())
            views.setTextViewText(R.id.widget_body, data[section + "Body"].toString())
            views.setTextViewText(R.id.widget_state, data[section + "State"].toString() + " · " + data["connection"])
            views.setTextViewText(R.id.widget_updated, data["updated"].toString())
            val open = PendingIntent.getActivity(context, 0, Intent(context, MainActivity::class.java), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
            views.setOnClickPendingIntent(R.id.widget_root, open)
            manager.updateAppWidget(ids, views)
        }
    }
}
