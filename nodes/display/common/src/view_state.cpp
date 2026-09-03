#include "orbit_display/view_state.h"

#include <cstring>

namespace orbit_display {

ViewApplyResult ViewStore::apply(const DisplayView& candidate,
                                 const char* expected_node_id) {
  if (candidate.core_epoch.value[0] == '\0' || candidate.revision == 0U ||
      candidate.node_id.value[0] == '\0') {
    return ViewApplyResult::kInvalid;
  }
  if (expected_node_id != nullptr &&
      std::strcmp(candidate.node_id.value, expected_node_id) != 0) {
    return ViewApplyResult::kWrongNode;
  }
  if (has_view_ && std::strcmp(candidate.core_epoch.value,
                               view_.core_epoch.value) == 0 &&
      candidate.revision <= view_.revision) {
    return ViewApplyResult::kOlderRevision;
  }

  view_ = candidate;
  has_view_ = true;
  stale_ = true;
  return ViewApplyResult::kAccepted;
}

void ViewStore::updateFreshness(int64_t now_epoch, bool clock_trusted) {
  if (!has_view_) {
    stale_ = true;
    return;
  }
  stale_ = !clock_trusted || now_epoch <= 0 ||
           view_.fresh_until <= now_epoch;
}

DisplaySnapshot ViewStore::snapshot(RuntimeState state,
                                    DisplayError error) const {
  DisplaySnapshot snapshot;
  snapshot.state = state;
  snapshot.view = view_;
  snapshot.error = error;
  snapshot.has_view = has_view_;
  snapshot.stale = stale_;
  return snapshot;
}

}  // namespace orbit_display
