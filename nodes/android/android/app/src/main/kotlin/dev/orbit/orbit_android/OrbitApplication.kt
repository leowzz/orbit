package dev.orbit.orbit_android

import android.app.Application
import android.content.res.Configuration

/** Refresh legacy RemoteViews even when the activity and sync service are stopped. */
class OrbitApplication : Application() {
    private var nightMode = Configuration.UI_MODE_NIGHT_UNDEFINED

    override fun onCreate() {
        super.onCreate()
        nightMode = resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK
    }

    override fun onConfigurationChanged(newConfig: Configuration) {
        super.onConfigurationChanged(newConfig)
        val next = newConfig.uiMode and Configuration.UI_MODE_NIGHT_MASK
        if (next != nightMode) {
            nightMode = next
            OrbitWidgets.updateAll(this, force = true)
        }
    }
}
