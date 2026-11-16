#pragma once

#include <stdint.h>

namespace orbit_display {

struct PixelShift {
  int8_t dx{0};
  int8_t dy{0};
};

inline constexpr uint8_t kOledShiftMarginX = 2U;
inline constexpr int8_t kOledMaxShiftX = 2;
inline constexpr int8_t kOledMaxShiftY = 2;
inline constexpr uint32_t kOledShiftIntervalMs = 60000U;
inline constexpr uint8_t kOledShiftCycleSteps = 48U;

// Steps one pixel at a time across a closed 5x5 [-2..+2] cycle to spread
// OLED pixel wear evenly without sudden jumps or edge clipping.
inline PixelShift computePixelShift(uint32_t elapsed_ms) {
  static constexpr PixelShift kCycle[kOledShiftCycleSteps] = {
      {0, 0},   {1, 0},   {2, 0},   {2, 1},   {1, 1},   {0, 1},
      {-1, 1},  {-2, 1},  {-2, 2},  {-1, 2},  {0, 2},   {1, 2},
      {2, 2},   {1, 2},   {0, 2},   {-1, 2},  {-2, 2},  {-2, 1},
      {-1, 1},  {0, 1},   {1, 1},   {2, 1},   {2, 0},   {1, 0},
      {0, 0},   {-1, 0},  {-2, 0},  {-2, -1}, {-1, -1}, {0, -1},
      {1, -1},  {2, -1},  {2, -2},  {1, -2},  {0, -2},  {-1, -2},
      {-2, -2}, {-1, -2}, {0, -2},  {1, -2},  {2, -2},  {2, -1},
      {1, -1},  {0, -1},  {-1, -1}, {-2, -1}, {-2, 0},  {-1, 0},
  };
  return kCycle[(elapsed_ms / kOledShiftIntervalMs) % kOledShiftCycleSteps];
}

}  // namespace orbit_display
