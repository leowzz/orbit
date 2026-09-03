#pragma once

#include <stddef.h>
#include <stdint.h>

#include <cstring>

namespace orbit_display {

inline constexpr size_t kMaxNodeIdBytes = 64U;
inline constexpr size_t kMaxDisplayTextBytes = 64U;
inline constexpr size_t kMaxDeviceViewPayloadBytes = 1024U;
inline constexpr size_t kMaxNodeStatePayloadBytes = 512U;

template <size_t Capacity>
struct BoundedText {
  char value[Capacity + 1U]{};
};

using NodeId = BoundedText<kMaxNodeIdBytes>;
using EpochId = BoundedText<kMaxNodeIdBytes>;
using DisplayText = BoundedText<kMaxDisplayTextBytes>;

template <size_t Capacity>
bool setText(BoundedText<Capacity>& destination, const char* source,
             size_t length) {
  if (source == nullptr || length > Capacity) {
    return false;
  }
  if (length > 0U) {
    std::memcpy(destination.value, source, length);
  }
  destination.value[length] = '\0';
  return true;
}

template <size_t Capacity>
bool setText(BoundedText<Capacity>& destination, const char* source) {
  if (source == nullptr) {
    return false;
  }
  return setText(destination, source, std::strlen(source));
}

struct DisplayView {
  EpochId core_epoch{};
  uint64_t revision{0};
  int64_t fresh_until{0};
  int64_t retain_until{0};
  NodeId node_id{};
  DisplayText primary{};
  DisplayText secondary{};
  DisplayText footer{};
};

struct NodeState {
  EpochId node_epoch{};
  uint64_t revision{0};
  NodeId node_id{};
  DisplayText series_id{};
  DisplayText model_id{};
  DisplayText variant_id{};
  DisplayText firmware_version{};
};

enum class RuntimeState : uint8_t {
  kBoot,
  kWifi,
  kTime,
  kMqtt,
  kReady,
  kError,
};

enum class DisplayError : uint8_t {
  kNone,
  kWifi,
  kTime,
  kMqtt,
  kProtocol,
  kOversized,
  kRejected,
};

struct DisplaySnapshot {
  RuntimeState state{RuntimeState::kBoot};
  DisplayView view{};
  DisplayError error{DisplayError::kNone};
  bool has_view{false};
  bool stale{true};
};

enum class ViewApplyResult : uint8_t {
  kAccepted,
  kInvalid,
  kWrongNode,
  kOlderRevision,
};

class ViewStore {
 public:
  ViewApplyResult apply(const DisplayView& view, const char* expected_node_id);
  void updateFreshness(int64_t now_epoch, bool clock_trusted);

  bool hasView() const { return has_view_; }
  bool stale() const { return stale_; }
  const DisplayView& view() const { return view_; }

  DisplaySnapshot snapshot(RuntimeState state, DisplayError error) const;

 private:
  DisplayView view_{};
  bool has_view_{false};
  bool stale_{true};
};

}  // namespace orbit_display
