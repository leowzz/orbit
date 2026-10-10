package dev.orbit.orbit_android

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.content.pm.ShortcutInfo
import android.content.pm.ShortcutManager
import android.graphics.drawable.Icon
import android.os.Bundle
import android.view.inputmethod.InputMethodManager
import android.widget.Toast

object InputMethodTools {
    const val ACTION_SHOW_PICKER = "dev.orbit.orbit_android.SHOW_INPUT_METHOD_PICKER"

    fun pickerIntent(context: Context) = Intent(context, InputMethodPickerActivity::class.java)
        .setAction(ACTION_SHOW_PICKER)

    fun pin(context: Context): String {
        val manager = context.getSystemService(ShortcutManager::class.java)
        if (manager == null || !manager.isRequestPinShortcutSupported) return "unsupported"
        val shortcut = ShortcutInfo.Builder(context, "input-method-picker")
            .setShortLabel("切换输入法")
            .setLongLabel("选择输入法")
            .setIcon(Icon.createWithResource(context, R.drawable.ic_input_method))
            .setIntent(pickerIntent(context))
            .build()
        return try {
            if (manager.requestPinShortcut(shortcut, null)) "requested" else "rejected"
        } catch (_: SecurityException) {
            "rejected"
        }
    }
}

/** A lightweight transparent entry so a desktop shortcut does not start Flutter. */
class InputMethodPickerActivity : Activity() {
    private var pickerRequested = false
    private var pickerTookFocus = false

    override fun onCreate(state: Bundle?) {
        super.onCreate(state)
        if (intent.action != InputMethodTools.ACTION_SHOW_PICKER) {
            finish()
            return
        }
        // Let the system register this window before opening the picker. Calling
        // directly in the first focus callback is too early for a launcher entry.
        window.decorView.postDelayed({ showPicker() }, 150)
    }

    private fun showPicker() {
        if (isFinishing || isDestroyed) return
        try {
            pickerRequested = true
            requireNotNull(getSystemService(InputMethodManager::class.java)).showInputMethodPicker()
        } catch (_: Exception) {
            Toast.makeText(this, "无法打开输入法列表，请重试", Toast.LENGTH_LONG).show()
            finish()
        }
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (pickerRequested && !hasFocus) {
            pickerTookFocus = true
        } else if (pickerTookFocus && hasFocus) {
            finish()
        }
    }
}
