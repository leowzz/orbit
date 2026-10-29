#pragma once
#include <Adafruit_GFX.h>

namespace orbit_display {
// Static gradient with a soft halo and a bright tip, for the weekly quota page.
void drawQuotaEnergyBar(GFXcanvas16& canvas, const char* primary, uint16_t color);
}
