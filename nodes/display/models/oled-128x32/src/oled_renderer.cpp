#include "orbit_display/oled_renderer.h"

#include <cstring>

#include "orbit_display/display_dirty_region.h"

namespace orbit_display {
namespace {

inline constexpr size_t kDisplayBufferSize =
    static_cast<size_t>(kOledTileWidth) * kOledTileHeight * kDisplayTileBytes;
inline constexpr uint8_t kSecondaryBaseline = 11U;
inline constexpr uint8_t kFooterBaseline = 27U;
inline constexpr uint16_t kUsableOledWidth =
    kOledWidth - 2U * kOledShiftMarginX;

PixelShift clampShift(PixelShift shift) {
  if (shift.dx < -kOledMaxShiftX) shift.dx = -kOledMaxShiftX;
  if (shift.dx > kOledMaxShiftX) shift.dx = kOledMaxShiftX;
  if (shift.dy < -kOledMaxShiftY) shift.dy = -kOledMaxShiftY;
  if (shift.dy > kOledMaxShiftY) shift.dy = kOledMaxShiftY;
  return shift;
}

struct FontMetrics {
  int16_t top;
  int16_t baseline;
};

FontMetrics centeredMetrics(U8G2& display, int8_t dy) {
  const int16_t ascent = display.getAscent();
  const int16_t descent = display.getDescent();
  const int16_t height = ascent - descent;
  const int16_t top = (static_cast<int16_t>(kOledHeight) - height) / 2;
  return {static_cast<int16_t>(top + dy),
          static_cast<int16_t>(top + ascent + dy)};
}

void copyFitted(U8G2& display, const char* source, char* output,
                size_t output_capacity, uint16_t max_width) {
  if (output_capacity == 0U) return;
  std::strncpy(output, source == nullptr ? "" : source, output_capacity - 1U);
  output[output_capacity - 1U] = '\0';
  size_t length = std::strlen(output);
  while (length > 0U && display.getStrWidth(output) > max_width) {
    output[--length] = '\0';
  }
}

const char* stateText(RuntimeState state) {
  switch (state) {
    case RuntimeState::kBoot:
      return "BOOT";
    case RuntimeState::kWifi:
      return "WIFI";
    case RuntimeState::kTime:
      return "TIME";
    case RuntimeState::kMqtt:
      return "MQTT";
    case RuntimeState::kReady:
      return "VIEW";
    case RuntimeState::kError:
      return "ERROR";
  }
  return "ERROR";
}

const char* errorText(DisplayError error) {
  switch (error) {
    case DisplayError::kNone:
      return nullptr;
    case DisplayError::kWifi:
      return "WIFI ERR";
    case DisplayError::kTime:
      return "TIME ERR";
    case DisplayError::kMqtt:
      return "MQTT ERR";
    case DisplayError::kProtocol:
      return "PROTO ERR";
    case DisplayError::kOversized:
      return "SIZE ERR";
    case DisplayError::kRejected:
      return "VIEW ERR";
  }
  return "ERROR";
}

void drawCentered(U8G2& display, const char* text, PixelShift shift) {
  char fitted[32];
  copyFitted(display, text, fitted, sizeof(fitted), kUsableOledWidth);
  const uint16_t width = display.getStrWidth(fitted);
  const uint8_t origin_x = static_cast<uint8_t>(kOledShiftMarginX + shift.dx);
  const uint8_t x =
      width < kUsableOledWidth
          ? static_cast<uint8_t>(origin_x + (kUsableOledWidth - width) / 2U)
          : origin_x;
  display.drawStr(x, static_cast<int16_t>(20 + shift.dy), fitted);
}

void drawRight(U8G2& display, const char* text, uint8_t baseline,
               uint8_t origin_x) {
  char fitted[65];
  copyFitted(display, text, fitted, sizeof(fitted), kUsableOledWidth);
  const uint16_t width = display.getStrWidth(fitted);
  const uint8_t x =
      width < kUsableOledWidth
          ? static_cast<uint8_t>(origin_x + (kUsableOledWidth - width))
          : origin_x;
  display.drawStr(x, baseline, fitted);
}

bool splitCountdown(const char* text, char separator, char (&count)[3],
                    const char*& reset) {
  // Core formats weekly rows as "%-2d MM/DD" and "%-2d HH:mm".
  if (std::strlen(text) != 8U || text[2] != ' ' || text[5] != separator) {
    return false;
  }
  for (size_t i = 0; i < 8U; ++i) {
    if (i == 2U || i == 5U) continue;
    if ((text[i] >= '0' && text[i] <= '9') || text[i] == '-') continue;
    if (i == 1U && text[i] == ' ') continue;
    return false;
  }
  count[0] = text[0];
  count[1] = text[1] == ' ' ? '\0' : text[1];
  count[2] = '\0';
  reset = text + 3;
  return true;
}

void drawView(U8G2& display, const DisplaySnapshot& snapshot,
              PixelShift shift) {
  const uint8_t origin_x = static_cast<uint8_t>(kOledShiftMarginX + shift.dx);
  const uint8_t secondary_baseline =
      static_cast<uint8_t>(kSecondaryBaseline + shift.dy);
  const uint8_t footer_baseline =
      static_cast<uint8_t>(kFooterBaseline + shift.dy);

  char days[3], hours[3];
  const char* secondary = snapshot.view.secondary.value;
  const char* footer = snapshot.view.footer.value;
  const size_t primary_length = std::strlen(snapshot.view.primary.value);
  const bool countdown =
      primary_length > 0U &&
      snapshot.view.primary.value[primary_length - 1U] == '%' &&
      splitCountdown(secondary, '/', days, secondary) &&
      splitCountdown(footer, ':', hours, footer);
  // Failed recognition must leave both original rows intact.
  if (!countdown) {
    secondary = snapshot.view.secondary.value;
    footer = snapshot.view.footer.value;
  }
  display.setFont(u8g2_font_6x10_tf);
  const uint16_t secondary_width = display.getStrWidth(secondary);
  const uint16_t footer_width = display.getStrWidth(footer);
  const uint16_t right_width =
      secondary_width > footer_width ? secondary_width : footer_width;
  // Reserve two characters so the percentage stays still at hour/day changes.
  const uint16_t left_width = countdown ? display.getStrWidth("00") : 0U;
  const uint16_t primary_area_width =
      left_width + right_width < kUsableOledWidth
          ? kUsableOledWidth - left_width - right_width
          : 0U;

  char primary[65];
  display.setFont(u8g2_font_helvB18_tf);
  const FontMetrics metrics = centeredMetrics(display, shift.dy);
  copyFitted(display, snapshot.view.primary.value, primary, sizeof(primary),
             countdown ? primary_area_width : kUsableOledWidth);
  const uint16_t width = display.getStrWidth(primary);
  const uint8_t x =
      width < primary_area_width
          ? static_cast<uint8_t>(origin_x + left_width +
                                 (primary_area_width - width) / 2U)
          : static_cast<uint8_t>(origin_x + left_width);
  display.drawStr(x, metrics.baseline, primary);

  display.setFont(u8g2_font_6x10_tf);
  if (countdown) {
    display.drawStr(origin_x, secondary_baseline, days);
    display.drawStr(origin_x, footer_baseline, hours);
  }
  drawRight(display, secondary, secondary_baseline, origin_x);
  drawRight(display, footer, footer_baseline, origin_x);

  if (snapshot.stale) {
    display.drawBox(static_cast<uint8_t>(kOledStaleMarkerX + shift.dx),
                    static_cast<uint8_t>(2 + shift.dy), 2, 2);
  }
}

}  // namespace

void renderUi(U8G2& display, const DisplaySnapshot& snapshot,
              bool force_full_refresh, PixelShift shift) {
  const PixelShift clamped = clampShift(shift);
  uint8_t previous_buffer[kDisplayBufferSize];
  std::memcpy(previous_buffer, display.getBufferPtr(), kDisplayBufferSize);
  display.clearBuffer();
  display.setFont(u8g2_font_6x10_tf);

  if (snapshot.has_view) {
    drawView(display, snapshot, clamped);
  } else {
    const char* text = errorText(snapshot.error);
    drawCentered(display, text == nullptr ? stateText(snapshot.state) : text,
                 clamped);
  }

  if (force_full_refresh) {
    display.sendBuffer();
    return;
  }

  const DisplayDirtyRegion dirty = findDisplayDirtyRegion(
      previous_buffer, display.getBufferPtr(), kOledTileWidth, kOledTileHeight);
  if (dirty.changed) {
    display.updateDisplayArea(dirty.x, dirty.y, dirty.width, dirty.height);
  }
}

}  // namespace orbit_display
