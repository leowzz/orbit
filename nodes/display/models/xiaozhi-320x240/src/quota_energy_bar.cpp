#include "orbit_display/quota_energy_bar.h"
#include <algorithm>
#include <cmath>
#include <cstdlib>

namespace orbit_display {
namespace {
constexpr float kLeft = 16.0f, kWidth = 288.0f, kCenterY = 155.5f;
constexpr uint16_t kTrack = 0x2185;

float barDistance(float x, float y, float width) {
  const float radius = std::min(3.5f, width / 2);
  const float dx = std::abs(x - kLeft - width / 2) - (width / 2 - radius);
  const float dy = std::abs(y - kCenterY) - (3.5f - radius);
  const float ox = std::max(dx, 0.0f), oy = std::max(dy, 0.0f);
  return std::sqrt(ox * ox + oy * oy) + std::min(std::max(dx, dy), 0.0f) - radius;
}

float coverage(float distance) {
  return std::clamp(0.5f - distance, 0.0f, 1.0f);
}

float halo(float distance, float radius, float strength) {
  const float falloff = std::max(0.0f, 1.0f - std::max(distance, 0.0f) / radius);
  return strength * falloff * falloff;
}

uint16_t blend(uint16_t background, uint16_t color, float opacity) {
  const int alpha = std::lround(std::clamp(opacity, 0.0f, 1.0f) * 255);
  const int r = (((background >> 11) & 31) * (255 - alpha) + ((color >> 11) & 31) * alpha + 127) / 255;
  const int g = (((background >> 5) & 63) * (255 - alpha) + ((color >> 5) & 63) * alpha + 127) / 255;
  const int b = ((background & 31) * (255 - alpha) + (color & 31) * alpha + 127) / 255;
  return (r << 11) | (g << 5) | b;
}
}

void drawQuotaEnergyBar(GFXcanvas16& canvas, const char* primary, uint16_t color) {
  if (!canvas.getBuffer()) return;
  float percent = 0;
  if (primary) {
    char* end = nullptr;
    const float value = std::strtof(primary, &end);
    if (end != primary && *end == '%' && end[1] == '\0' && std::isfinite(value)) {
      percent = std::clamp(value, 0.0f, 100.0f);
    }
  }
  const float filled = kWidth * percent / 100;
  const float tipRadius = std::min(2.5f, filled / 2);
  const float tipX = kLeft + filled - tipRadius;
  // Limit blending to the bar and its halo; no extra framebuffer or animation.
  for (int y = 143; y < std::min(169, static_cast<int>(canvas.height())); ++y) {
    for (int x = 7; x < std::min(313, static_cast<int>(canvas.width())); ++x) {
      const float px = x + 0.5f, py = y + 0.5f;
      uint16_t& pixel = canvas.getBuffer()[y * canvas.width() + x];
      const float trackAlpha = coverage(barDistance(px, py, kWidth));
      if (filled > 0) {
        const float distance = barDistance(px, py, filled);
        const float gradient = 0.22f + 0.63f * std::clamp((px - kLeft) / filled, 0.0f, 1.0f);
        pixel = blend(pixel, color, halo(distance, 5, 0.22f) * gradient);
        pixel = blend(pixel, kTrack, trackAlpha);
        pixel = blend(pixel, color, coverage(distance) * gradient);
        const float dx = px - tipX, dy = py - kCenterY;
        const float tipDistance = std::sqrt(dx * dx + dy * dy) - tipRadius;
        const float tipStrength = std::min(1.0f, filled / 5);
        pixel = blend(pixel, color, halo(tipDistance, 8, 0.3f) * tipStrength);
        pixel = blend(pixel, color, coverage(tipDistance) * tipStrength);
      } else {
        pixel = blend(pixel, kTrack, trackAlpha);
      }
    }
  }
}
}
