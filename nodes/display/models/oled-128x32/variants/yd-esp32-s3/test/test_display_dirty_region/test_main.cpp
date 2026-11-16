#include <unity.h>

#include <cstdlib>

#include "orbit_display/display_dirty_region.h"
#include "orbit_display/oled_pixel_shift.h"

using orbit_display::findDisplayDirtyRegion;

namespace {

constexpr uint8_t kTileWidth = 4U;
constexpr uint8_t kTileHeight = 3U;
constexpr size_t kBufferSize = kTileWidth * kTileHeight * 8U;

void changeTile(uint8_t* buffer, uint8_t x, uint8_t y) {
  buffer[(static_cast<size_t>(y) * kTileWidth + x) * 8U] = 1U;
}

}  // namespace

void test_identical_buffers_have_no_dirty_region() {
  uint8_t before[kBufferSize]{};
  uint8_t after[kBufferSize]{};

  const auto dirty =
      findDisplayDirtyRegion(before, after, kTileWidth, kTileHeight);

  TEST_ASSERT_FALSE(dirty.changed);
  TEST_ASSERT_EQUAL_UINT8(0, dirty.width);
  TEST_ASSERT_EQUAL_UINT8(0, dirty.height);
}

void test_changes_return_minimum_bounding_tile_region() {
  uint8_t before[kBufferSize]{};
  uint8_t after[kBufferSize]{};
  changeTile(after, 1U, 0U);
  changeTile(after, 3U, 2U);

  const auto dirty =
      findDisplayDirtyRegion(before, after, kTileWidth, kTileHeight);

  TEST_ASSERT_TRUE(dirty.changed);
  TEST_ASSERT_EQUAL_UINT8(1, dirty.x);
  TEST_ASSERT_EQUAL_UINT8(0, dirty.y);
  TEST_ASSERT_EQUAL_UINT8(3, dirty.width);
  TEST_ASSERT_EQUAL_UINT8(3, dirty.height);
}

void test_pixel_shift_cycle_covers_grid_one_pixel_per_step() {
  using namespace orbit_display;
  const PixelShift first = computePixelShift(0U);
  TEST_ASSERT_EQUAL_INT8(0, first.dx);
  TEST_ASSERT_EQUAL_INT8(0, first.dy);

  bool visited[5][5]{};
  for (uint32_t step = 0; step < kOledShiftCycleSteps; ++step) {
    const PixelShift cur = computePixelShift(step * kOledShiftIntervalMs);
    const PixelShift next =
        computePixelShift(((step + 1U) % kOledShiftCycleSteps) * kOledShiftIntervalMs);
    TEST_ASSERT_TRUE(cur.dx >= -kOledMaxShiftX && cur.dx <= kOledMaxShiftX);
    TEST_ASSERT_TRUE(cur.dy >= -kOledMaxShiftY && cur.dy <= kOledMaxShiftY);
    visited[cur.dy + kOledMaxShiftY][cur.dx + kOledMaxShiftX] = true;
    const int manhattan = std::abs(next.dx - cur.dx) + std::abs(next.dy - cur.dy);
    TEST_ASSERT_EQUAL_INT(1, manhattan);
  }
  for (int y = 0; y < 5; ++y) {
    for (int x = 0; x < 5; ++x) {
      TEST_ASSERT_TRUE(visited[y][x]);
    }
  }
}

void setUp() {}
void tearDown() {}

int main(int, char**) {
  UNITY_BEGIN();
  RUN_TEST(test_identical_buffers_have_no_dirty_region);
  RUN_TEST(test_changes_return_minimum_bounding_tile_region);
  RUN_TEST(test_pixel_shift_cycle_covers_grid_one_pixel_per_step);
  return UNITY_END();
}
