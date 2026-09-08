package dev.orbit.orbit_android

import orbit.v1.View.CodexSessionView
import java.math.BigDecimal
import java.math.RoundingMode
import java.util.Currency
import java.util.Locale

object WidgetPresentation {
    fun cost(micros: Long, currencyCode: String): String {
        val symbol = if (currencyCode == "USD" || currencyCode.isBlank()) "$" else
            runCatching { Currency.getInstance(currencyCode).getSymbol(Locale.US) }.getOrDefault(currencyCode)
        if (micros in 1..9999) return "<$symbol" + "0.01"
        return symbol + BigDecimal.valueOf(micros, 6).setScale(2, RoundingMode.DOWN).stripTrailingZeros().toPlainString()
    }
    fun sessions(values: List<CodexSessionView>, limit: Int): List<CodexSessionView> = values.sortedWith(
        compareBy<CodexSessionView> { if (it.statusValue == 2) 0 else 1 }
            .thenByDescending { it.updatedAt.seconds }
            .thenByDescending { it.updatedAt.nanos }
    ).take(limit)

    fun status(value: Int): Pair<String, Int> = when (value) {
        2 -> "运行中" to 0xFF33785B.toInt()
        3 -> "完成" to 0xFF788379.toInt()
        4 -> "失败" to 0xFFB25049.toInt()
        5 -> "中断" to 0xFFA17C3D.toInt()
        6 -> "取消" to 0xFF92988F.toInt()
        else -> "未知" to 0xFF92988F.toInt()
    }
}
