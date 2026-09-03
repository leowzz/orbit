#include <unity.h>

#include "orbit_display/display_dirty_region.h"

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

int main(int, char**) {
  UNITY_BEGIN();
  RUN_TEST(test_identical_buffers_have_no_dirty_region);
  RUN_TEST(test_changes_return_minimum_bounding_tile_region);
  return UNITY_END();
}
