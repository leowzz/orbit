#include "orbit_display/smooth_font.h"
#include <algorithm>
#include <cmath>
#include <cstdint>

namespace orbit_display {
namespace {
struct Glyph {
  uint8_t code;
  uint32_t offset;
  uint16_t width, height;
  int16_t left, top;
  uint16_t advance64;
};
struct Font {
  const Glyph* glyphs;
  const uint8_t* coverage;
  size_t count;
};
#include "font_data.inc"

const Glyph* glyph(const Font& font, unsigned char code) {
  for (size_t i = 0; i < font.count; ++i)
    if (font.glyphs[i].code == code) return &font.glyphs[i];
  return nullptr;
}

// Compact prices by their visible glyph bounds, especially around '$' and '.'.
// Keep a positive optical gap so thin punctuation never touches adjacent digits.
float bearing(const Glyph& g, TextSpacing spacing) {
  return spacing == TextSpacing::Compact && g.width && g.height ? 0 : g.left;
}

float advance(const Glyph& g, TextSpacing spacing) {
  const float cell = g.advance64 / 64.0f;
  return spacing == TextSpacing::Compact && g.width && g.height
             ? g.width + std::max(1.0f, cell * 0.15f) : cell;
}

float coverage(const Font& font, const Glyph& g, int x, int y) {
  if (x < 0 || y < 0 || x >= g.width || y >= g.height) return 0;
  const size_t pixel = static_cast<size_t>(y) * g.width + x;
  const uint8_t packed = font.coverage[g.offset + pixel / 2];
  return static_cast<float>((pixel & 1) ? packed & 15 : packed >> 4);
}

uint16_t blend(uint16_t background, uint16_t color, int alpha) {
  const int r = (((background >> 11) & 31) * (255-alpha) + ((color >> 11) & 31) * alpha + 127) / 255;
  const int g = (((background >> 5) & 63) * (255-alpha) + ((color >> 5) & 63) * alpha + 127) / 255;
  const int b = ((background & 31) * (255-alpha) + (color & 31) * alpha + 127) / 255;
  return (r << 11) | (g << 5) | b;
}
}

void drawSmoothText(GFXcanvas16& canvas, FontFace face, const char* value,
                    int x, int y, int width, int height, uint16_t color,
                    TextAlign align, TextSpacing spacing) {
  if (!value || !*value || width <= 0 || height <= 0 || !canvas.getBuffer()) return;
  const Font& font = face == FontFace::Primary ? kPrimary : face == FontFace::Secondary ? kSecondary : kSmall;
  float pen = 0, left = 0, right = 0, top = 0, bottom = 0;
  bool first = true;
  for (const unsigned char* p = reinterpret_cast<const unsigned char*>(value); *p; ++p) {
    const Glyph* g = glyph(font, *p);
    if (!g) continue;
    if (g->width && g->height) {
      const float start = pen + bearing(*g, spacing);
      left = first ? start : std::min(left, start);
      right = first ? start + g->width : std::max(right, start + g->width);
      top = first ? g->top : std::min(top, static_cast<float>(g->top));
      bottom = first ? g->top + g->height : std::max(bottom, static_cast<float>(g->top + g->height));
      first = false;
    }
    pen += advance(*g, spacing);
  }
  if (first || right <= left || bottom <= top) return;
  if (face == FontFace::Secondary) {
    // Keep both detail columns and pages on the same baseline, even with '/'.
    for (size_t i = 0; i < font.count; ++i) {
      const Glyph& g = font.glyphs[i];
      if (!g.width || !g.height) continue;
      top = std::min(top, static_cast<float>(g.top));
      bottom = std::max(bottom, static_cast<float>(g.top + g.height));
    }
  }
  const float scale = std::min(1.0f, std::min(width / (right-left), height / (bottom-top)));
  const float remaining = width - (right-left) * scale;
  const float originX = x - left * scale + (align == TextAlign::Center ? remaining / 2 : align == TextAlign::Right ? remaining : 0);
  const float originY = y - top * scale + (height - (bottom-top) * scale) / 2;
  pen = 0;
  for (const unsigned char* p = reinterpret_cast<const unsigned char*>(value); *p; ++p) {
    const Glyph* g = glyph(font, *p);
    if (!g) continue;
    const float gx = originX + (pen + bearing(*g, spacing)) * scale, gy = originY + g->top * scale;
    const int x0 = std::max(x, static_cast<int>(std::floor(gx)));
    const int y0 = std::max(y, static_cast<int>(std::floor(gy)));
    const int x1 = std::min(x+width, static_cast<int>(std::ceil(gx + g->width*scale)));
    const int y1 = std::min(y+height, static_cast<int>(std::ceil(gy + g->height*scale)));
    for (int dy = y0; dy < y1; ++dy) {
      for (int dx = x0; dx < x1; ++dx) {
        if (dx < 0 || dy < 0 || dx >= canvas.width() || dy >= canvas.height()) continue;
        const float sx = (dx + 0.5f - gx) / scale - 0.5f, sy = (dy + 0.5f - gy) / scale - 0.5f;
        const int ix = std::floor(sx), iy = std::floor(sy);
        const float fx = sx-ix, fy = sy-iy;
        const float a = coverage(font, *g, ix, iy)*(1-fx) + coverage(font, *g, ix+1, iy)*fx;
        const float b = coverage(font, *g, ix, iy+1)*(1-fx) + coverage(font, *g, ix+1, iy+1)*fx;
        const int alpha = std::lround((a*(1-fy) + b*fy)*17);
        if (alpha) {
          uint16_t& pixel = canvas.getBuffer()[dy*canvas.width()+dx];
          pixel = blend(pixel, color, alpha);
        }
      }
    }
    pen += advance(*g, spacing);
  }
}
}
