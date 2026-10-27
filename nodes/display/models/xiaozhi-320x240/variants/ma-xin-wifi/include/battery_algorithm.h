#pragma once

#include <stdint.h>

inline uint8_t batteryLevelFromAdc(uint32_t averageAdc) {
  if (averageAdc <= 1800) return 0;
  if (averageAdc >= 2450) return 100;
  return static_cast<uint8_t>((averageAdc - 1800) * 100.0f / 650.0f);
}
