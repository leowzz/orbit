package core

import (
	"fmt"
	"math"
	"strings"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
)

type renderedSlots struct {
	Primary   *orbitv1.DisplaySlot
	Secondary *orbitv1.DisplaySlot
	Footer    *orbitv1.DisplaySlot
}

func emptySlots() renderedSlots {
	return renderedSlots{
		Primary:   &orbitv1.DisplaySlot{Text: "--"},
		Secondary: &orbitv1.DisplaySlot{Text: "--"},
		Footer:    &orbitv1.DisplaySlot{Text: "--"},
	}
}

func renderUsageSlots(profile string, usage canonicalUsage, hasUsage bool) renderedSlots {
	if !hasUsage || usage.value == nil {
		return emptySlots()
	}
	cost := usage.value.GetActualCostMicros()
	tokens := usage.value.GetTokenCount()
	tpm := usage.value.GetTpm()
	primary := formatCost(cost)
	if profile == xiaozhiProfile {
		primary = formatXiaozhiCost(cost)
	}
	return renderedSlots{
		Primary:   &orbitv1.DisplaySlot{Text: primary, Emphasis: orbitv1.Emphasis_EMPHASIS_STRONG},
		Secondary: &orbitv1.DisplaySlot{Text: formatMetric(tokens), Emphasis: orbitv1.Emphasis_EMPHASIS_NORMAL},
		Footer:    &orbitv1.DisplaySlot{Text: formatMetric(tpm), Emphasis: orbitv1.Emphasis_EMPHASIS_DIM},
	}
}

// renderSystemSlots formats CPU, memory, and 1m load average (LAV) metrics to fit
// both 128x32 OLED screens and the Xiaozhi 320x240 primary font (" $%+,-.0123456789").
func renderSystemSlots(primaryMetric string, obs *orbitv1.SystemObservation, hasObs bool) renderedSlots {
	loadFirst := primaryMetric == "load" || primaryMetric == "lav"
	memoryFirst := primaryMetric == "memory"
	secondaryMissing := "MEM --%"
	primaryMissing := "--%"
	if memoryFirst {
		secondaryMissing = "CPU --%"
	} else if loadFirst {
		primaryMissing = "--.-"
		secondaryMissing = "C--% M--%"
	}
	if !hasObs || obs == nil {
		return renderedSlots{
			Primary:   &orbitv1.DisplaySlot{Text: primaryMissing, Emphasis: orbitv1.Emphasis_EMPHASIS_STRONG},
			Secondary: &orbitv1.DisplaySlot{Text: secondaryMissing, Emphasis: orbitv1.Emphasis_EMPHASIS_NORMAL},
			Footer:    &orbitv1.DisplaySlot{Text: "--/--G", Emphasis: orbitv1.Emphasis_EMPHASIS_DIM},
		}
	}
	cpuPct := systemCPUPercent(obs)
	memPct := systemMemoryPercent(obs)
	memRatio := formatMemoryRatio(obs.GetMemoryUsedBytes(), obs.GetMemoryTotalBytes())

	if loadFirst {
		primaryText := "--.-"
		if obs.Load_1MMilli != nil {
			primaryText = formatLoadValue(obs.GetLoad_1MMilli())
		}
		return renderedSlots{
			Primary:   &orbitv1.DisplaySlot{Text: primaryText, Emphasis: orbitv1.Emphasis_EMPHASIS_STRONG},
			Secondary: &orbitv1.DisplaySlot{Text: fmt.Sprintf("C%d%% M%d%%", cpuPct, memPct), Emphasis: orbitv1.Emphasis_EMPHASIS_NORMAL},
			Footer:    &orbitv1.DisplaySlot{Text: memRatio, Emphasis: orbitv1.Emphasis_EMPHASIS_DIM},
		}
	}

	primaryText := fmt.Sprintf("%d%%", cpuPct)
	if memoryFirst {
		primaryText = fmt.Sprintf("%d%%", memPct)
	}
	if obs.Load_1MMilli != nil {
		compactMem := formatCompactMemoryRatio(obs.GetMemoryUsedBytes(), obs.GetMemoryTotalBytes())
		secondaryText := fmt.Sprintf("M%d%% %s", memPct, compactMem)
		if memoryFirst {
			secondaryText = fmt.Sprintf("C%d%% %s", cpuPct, compactMem)
		}
		return renderedSlots{
			Primary:   &orbitv1.DisplaySlot{Text: primaryText, Emphasis: orbitv1.Emphasis_EMPHASIS_STRONG},
			Secondary: &orbitv1.DisplaySlot{Text: secondaryText, Emphasis: orbitv1.Emphasis_EMPHASIS_NORMAL},
			Footer:    &orbitv1.DisplaySlot{Text: formatLoadAvg(obs.GetLoad_1MMilli()), Emphasis: orbitv1.Emphasis_EMPHASIS_DIM},
		}
	}

	secondaryText := fmt.Sprintf("MEM %d%%", memPct)
	if memoryFirst {
		secondaryText = fmt.Sprintf("CPU %d%%", cpuPct)
	}
	return renderedSlots{
		Primary:   &orbitv1.DisplaySlot{Text: primaryText, Emphasis: orbitv1.Emphasis_EMPHASIS_STRONG},
		Secondary: &orbitv1.DisplaySlot{Text: secondaryText, Emphasis: orbitv1.Emphasis_EMPHASIS_NORMAL},
		Footer:    &orbitv1.DisplaySlot{Text: memRatio, Emphasis: orbitv1.Emphasis_EMPHASIS_DIM},
	}
}

func formatLoadValue(milli uint32) string {
	load := float64(milli) / 1000.0
	if load >= 100 {
		return fmt.Sprintf("%.0f", math.Round(load))
	}
	if load >= 10 {
		return fmt.Sprintf("%.1f", load)
	}
	return fmt.Sprintf("%.2f", load)
}

func formatLoadAvg(milli uint32) string {
	return "LAV " + formatLoadValue(milli)
}

func formatCompactMemoryRatio(usedBytes, totalBytes uint64) string {
	if totalBytes == 0 {
		return "--/--G"
	}
	if usedBytes > totalBytes {
		usedBytes = totalBytes
	}
	const mib = 1024 * 1024
	const gib = 1024 * mib
	if totalBytes < 1000*mib {
		return formatMemoryRatio(usedBytes, totalBytes)
	}
	usedG := float64(usedBytes) / float64(gib)
	totalG := float64(totalBytes) / float64(gib)
	if totalG >= 10 {
		return fmt.Sprintf("%.0f/%sG", math.Round(usedG), trimDotZero(totalG))
	}
	full := fmt.Sprintf("%.1f/%sG", usedG, trimDotZero(totalG))
	if len(full) <= 6 {
		return full
	}
	return fmt.Sprintf("%.0f/%sG", math.Round(usedG), trimDotZero(totalG))
}

func systemCPUPercent(obs *orbitv1.SystemObservation) uint32 {
	pct := (obs.GetCpuUsagePermille() + 5) / 10
	if pct > 100 {
		return 100
	}
	return pct
}

func systemMemoryPercent(obs *orbitv1.SystemObservation) uint32 {
	total := obs.GetMemoryTotalBytes()
	if total == 0 {
		return 0
	}
	used := obs.GetMemoryUsedBytes()
	if used >= total {
		return 100
	}
	return uint32(math.Round(float64(used) * 100 / float64(total)))
}

func formatMemoryRatio(usedBytes, totalBytes uint64) string {
	if totalBytes == 0 {
		return "--/--G"
	}
	if usedBytes > totalBytes {
		usedBytes = totalBytes
	}
	const mib = 1024 * 1024
	const gib = 1024 * mib
	if totalBytes < 1000*mib {
		usedM := uint64(math.Round(float64(usedBytes) / float64(mib)))
		totalM := uint64(math.Round(float64(totalBytes) / float64(mib)))
		if totalM == 0 {
			totalM = 1
		}
		if usedM > totalM {
			usedM = totalM
		}
		return fmt.Sprintf("%d/%dM", usedM, totalM)
	}
	usedG := float64(usedBytes) / float64(gib)
	totalG := float64(totalBytes) / float64(gib)
	if totalG >= 100 {
		return fmt.Sprintf("%.0f/%.0fG", math.Round(usedG), math.Round(totalG))
	}
	return fmt.Sprintf("%.1f/%sG", usedG, trimDotZero(totalG))
}

func trimDotZero(value float64) string {
	rounded := math.Round(value*10) / 10
	formatted := fmt.Sprintf("%.1f", rounded)
	return strings.TrimSuffix(formatted, ".0")
}

func applyRenderedSlots(view *orbitv1.DeviceView, slots renderedSlots) {
	view.Primary = slots.Primary
	view.Secondary = slots.Secondary
	view.Footer = slots.Footer
}

func freshnessLabel(f orbitv1.Freshness) string {
	switch f {
	case orbitv1.Freshness_FRESHNESS_FRESH:
		return "fresh"
	case orbitv1.Freshness_FRESHNESS_STALE:
		return "stale"
	default:
		return "unspecified"
	}
}

func consoleNodePreview(now time.Time, view *orbitv1.DeviceView, step, total int) map[string]any {
	if view == nil {
		return nil
	}
	preview := map[string]any{
		"display_kind": view.GetDisplayKind(),
		"primary":      view.GetPrimary().GetText(),
		"secondary":    view.GetSecondary().GetText(),
		"footer":       view.GetFooter().GetText(),
		"freshness":    freshnessLabel(view.GetFreshness()),
		"updated_at":   now,
	}
	if total > 1 {
		preview["rotation_step"] = step
		preview["rotation_total"] = total
	}
	if view.DisplayBrightnessPercent != nil {
		preview["brightness_percent"] = view.GetDisplayBrightnessPercent()
	}
	return preview
}
