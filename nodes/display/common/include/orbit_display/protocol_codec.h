#pragma once

#include <stddef.h>
#include <stdint.h>

#include "orbit_display/view_state.h"

namespace orbit_display {

enum class CodecError : uint8_t {
  kNone,
  kMalformed,
  kUnsupportedVersion,
  kOversized,
};

class IProtocolCodec {
 public:
  virtual ~IProtocolCodec() = default;

  virtual bool decodeDeviceView(const uint8_t* payload, size_t payload_size,
                                DisplayView& output,
                                CodecError& error) = 0;
  virtual bool encodeNodeState(const NodeState& state, uint8_t* output,
                               size_t output_capacity, size_t& output_size,
                               CodecError& error) = 0;
};

}  // namespace orbit_display
