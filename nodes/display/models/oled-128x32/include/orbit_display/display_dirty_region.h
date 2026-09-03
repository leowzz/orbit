#pragma once

#include <stddef.h>
#include <stdint.h>

namespace orbit_display {

inline constexpr size_t kDisplayTileBytes = 8U;

struct DisplayDirtyRegion {
  bool changed;
  uint8_t x;
  uint8_t y;
  uint8_t width;
  uint8_t height;
};

inline DisplayDirtyRegion findDisplayDirtyRegion(
    const uint8_t* before, const uint8_t* after, uint8_t tile_width,
    uint8_t tile_height) {
  uint8_t min_x = tile_width;
  uint8_t min_y = tile_height;
  uint8_t max_x = 0U;
  uint8_t max_y = 0U;

  for (uint8_t y = 0U; y < tile_height; ++y) {
    for (uint8_t x = 0U; x < tile_width; ++x) {
      const size_t offset =
          (static_cast<size_t>(y) * tile_width + x) * kDisplayTileBytes;
      bool tile_changed = false;
      for (size_t byte = 0U; byte < kDisplayTileBytes; ++byte) {
        if (before[offset + byte] != after[offset + byte]) {
          tile_changed = true;
          break;
        }
      }
      if (!tile_changed) {
        continue;
      }
      if (x < min_x) min_x = x;
      if (y < min_y) min_y = y;
      if (x > max_x) max_x = x;
      if (y > max_y) max_y = y;
    }
  }

  if (min_x == tile_width) {
    return {false, 0U, 0U, 0U, 0U};
  }
  return {true, min_x, min_y, static_cast<uint8_t>(max_x - min_x + 1U),
          static_cast<uint8_t>(max_y - min_y + 1U)};
}

}  // namespace orbit_display
