#pragma once
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstdlib>

namespace orbit_display {
inline uint16_t weeklyQuotaColor(const char* primary) {
  if (!primary) return 0xFFFF;
  char* end = nullptr;
  const float percent = std::strtof(primary, &end);
  if (end == primary || *end != '%' || end[1] != '\0' || !std::isfinite(percent)) {
    return 0xFFFF;
  }
  if (percent <= 10) return 0xF800;
  if (percent <= 30) return 0xFFE0;
  if (percent <= 90) return 0xFFFF;
  return 0x07E0;
}

struct WeeklyDetails {
  char reset[24] = "--/-- --:--";
  char remaining[32] = "--d --h";
};

// Core's weekly slots contain "days MM/DD" and "hours HH:MM".
// Regroup these display fields without deriving quota data on the device.
inline WeeklyDetails weeklyDetails(const char* secondary, const char* footer) {
  WeeklyDetails result;
  if (!secondary || !footer) return result;
  char days[12], hours[12], date[8], time[8], extra;
  if (std::sscanf(secondary, "%11s %7s %c", days, date, &extra) != 2 ||
      std::sscanf(footer, "%11s %7s %c", hours, time, &extra) != 2) return result;
  std::snprintf(result.reset, sizeof(result.reset), "%s %s", date, time);
  std::snprintf(result.remaining, sizeof(result.remaining), "%sd %sh", days, hours);
  return result;
}
}
