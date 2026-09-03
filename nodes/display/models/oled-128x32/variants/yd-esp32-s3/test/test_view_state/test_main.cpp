#include <unity.h>

#include <cstdio>

#include "orbit_display/view_state.h"

using namespace orbit_display;

namespace {

DisplayView makeView(uint64_t epoch, uint64_t revision, int64_t fresh_until,
                     const char* node_id = "desk-oled-01") {
  DisplayView view;
  char epoch_text[24];
  snprintf(epoch_text, sizeof(epoch_text), "core-%llu",
           static_cast<unsigned long long>(epoch));
  TEST_ASSERT_TRUE(setText(view.core_epoch, epoch_text));
  view.revision = revision;
  view.fresh_until = fresh_until;
  TEST_ASSERT_TRUE(setText(view.node_id, node_id));
  TEST_ASSERT_TRUE(setText(view.primary, "primary"));
  TEST_ASSERT_TRUE(setText(view.secondary, "secondary"));
  TEST_ASSERT_TRUE(setText(view.footer, "footer"));
  return view;
}

}  // namespace

void test_accepts_first_view_and_marks_it_stale_until_clock_is_trusted() {
  ViewStore store;
  const DisplayView view = makeView(7, 1, 2000);

  TEST_ASSERT_EQUAL(ViewApplyResult::kAccepted,
                    store.apply(view, "desk-oled-01"));
  TEST_ASSERT_TRUE(store.hasView());
  TEST_ASSERT_TRUE(store.stale());

  store.updateFreshness(1999, true);
  TEST_ASSERT_FALSE(store.stale());
  store.updateFreshness(2000, true);
  TEST_ASSERT_TRUE(store.stale());
  store.updateFreshness(1999, false);
  TEST_ASSERT_TRUE(store.stale());
}

void test_rejects_lower_or_duplicate_revision_in_same_core_epoch() {
  ViewStore store;
  TEST_ASSERT_EQUAL(ViewApplyResult::kAccepted,
                    store.apply(makeView(7, 3, 2000), "desk-oled-01"));
  TEST_ASSERT_EQUAL(ViewApplyResult::kOlderRevision,
                    store.apply(makeView(7, 2, 3000), "desk-oled-01"));
  TEST_ASSERT_EQUAL(ViewApplyResult::kOlderRevision,
                    store.apply(makeView(7, 3, 3000), "desk-oled-01"));
}

void test_accepts_first_view_from_a_new_core_epoch() {
  ViewStore store;
  TEST_ASSERT_EQUAL(ViewApplyResult::kAccepted,
                    store.apply(makeView(7, 100, 2000), "desk-oled-01"));
  TEST_ASSERT_EQUAL(ViewApplyResult::kAccepted,
                    store.apply(makeView(8, 1, 3000), "desk-oled-01"));
  TEST_ASSERT_EQUAL_STRING("core-8", store.view().core_epoch.value);
  TEST_ASSERT_EQUAL_UINT64(1, store.view().revision);
}

void test_rejects_wrong_node_and_invalid_identity() {
  ViewStore store;
  TEST_ASSERT_EQUAL(ViewApplyResult::kWrongNode,
                    store.apply(makeView(7, 1, 2000, "other-node"),
                                "desk-oled-01"));
  DisplayView invalid = makeView(7, 1, 2000);
  TEST_ASSERT_TRUE(setText(invalid.core_epoch, ""));
  TEST_ASSERT_EQUAL(ViewApplyResult::kInvalid,
                    store.apply(invalid, "desk-oled-01"));
}

void test_rejects_text_over_the_fixed_bound() {
  DisplayText text;
  char too_long[kMaxDisplayTextBytes + 2U]{};
  for (size_t index = 0U; index < kMaxDisplayTextBytes + 1U; ++index) {
    too_long[index] = 'x';
  }
  TEST_ASSERT_FALSE(setText(text, too_long));
}

int main(int, char**) {
  UNITY_BEGIN();
  RUN_TEST(test_accepts_first_view_and_marks_it_stale_until_clock_is_trusted);
  RUN_TEST(test_rejects_lower_or_duplicate_revision_in_same_core_epoch);
  RUN_TEST(test_accepts_first_view_from_a_new_core_epoch);
  RUN_TEST(test_rejects_wrong_node_and_invalid_identity);
  RUN_TEST(test_rejects_text_over_the_fixed_bound);
  return UNITY_END();
}
