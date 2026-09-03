#pragma once

#include <U8g2lib.h>

#include "orbit_display/view_state.h"

namespace orbit_display {

inline constexpr uint8_t kOledWidth = 128U;
inline constexpr uint8_t kOledHeight = 32U;
inline constexpr uint8_t kOledTileWidth = kOledWidth / 8U;
inline constexpr uint8_t kOledTileHeight = kOledHeight / 8U;
inline constexpr uint8_t kOledStaleMarkerX = 124U;

void renderUi(U8G2& display, const DisplaySnapshot& snapshot,
              bool force_full_refresh = false);

}  // namespace orbit_display
