package dev.orbit.orbit_android

import android.content.Context
import android.content.res.Configuration

object OrbitAppearance {
    private fun preferences(context: Context) = context.getSharedPreferences("orbit_appearance", Context.MODE_PRIVATE)

    fun isDark(context: Context): Boolean = preferences(context).getBoolean("dark", false)

    fun save(context: Context, dark: Boolean) {
        check(preferences(context).edit().putBoolean("dark", dark).commit()) { "无法保存外观设置" }
    }

    fun themedContext(context: Context): Context {
        val config = Configuration(context.resources.configuration)
        config.uiMode = (config.uiMode and Configuration.UI_MODE_NIGHT_MASK.inv()) or
            if (isDark(context)) Configuration.UI_MODE_NIGHT_YES else Configuration.UI_MODE_NIGHT_NO
        return context.createConfigurationContext(config)
    }
}
