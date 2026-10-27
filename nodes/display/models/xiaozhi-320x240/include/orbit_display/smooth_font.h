#pragma once
#include <Adafruit_GFX.h>

namespace orbit_display {
enum class FontFace { Small, Secondary, Primary };
enum class TextAlign { Left, Center, Right };
enum class TextSpacing { Monospace, Compact };

// Rasterized at the intended optical size; downscaling uses bilinear coverage.
// Bounds include glyph bearings and fit the complete string inside the box.
void drawSmoothText(GFXcanvas16& canvas, FontFace face, const char* value,
                    int x, int y, int width, int height, uint16_t color,
                    TextAlign align = TextAlign::Left,
                    TextSpacing spacing = TextSpacing::Monospace);
}
