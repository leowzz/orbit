#include "nanopb_protocol.h"

#include <pb_decode.h>
#include <pb_encode.h>

#include <cstdio>
#include <cstring>
#include <ctime>
#include <memory>

#include "orbit/v1/state.pb.h"
#include "orbit/v1/view.pb.h"

namespace {

template <size_t Size>
bool copyText(char (&destination)[Size], const char* source) {
  if (source == nullptr || std::strlen(source) >= Size) return false;
  std::strcpy(destination, source);
  return true;
}

class NanopbProtocolCodec final : public orbit_display::IProtocolCodec {
 public:
  bool decodeDeviceView(const uint8_t* payload, size_t payload_size,
                        orbit_display::DisplayView& output,
                        orbit_display::CodecError& error) override {
    error = orbit_display::CodecError::kNone;
    if (payload == nullptr || payload_size == 0U ||
        payload_size > orbit_display::kMaxDeviceViewPayloadBytes) {
      error = orbit_display::CodecError::kOversized;
      return false;
    }

    orbit_v1_DeviceView message = orbit_v1_DeviceView_init_zero;
    pb_istream_t stream = pb_istream_from_buffer(payload, payload_size);
    if (!pb_decode(&stream, orbit_v1_DeviceView_fields, &message)) {
      error = orbit_display::CodecError::kMalformed;
      return false;
    }
    if (!message.has_metadata || message.metadata.revision == 0U ||
        !message.has_fresh_until || !message.has_retain_until ||
        !message.has_primary || !message.has_secondary || !message.has_footer) {
      error = orbit_display::CodecError::kMalformed;
      return false;
    }

    orbit_display::DisplayView decoded;
    decoded.revision = message.metadata.revision;
    decoded.fresh_until = message.fresh_until.seconds;
    decoded.retain_until = message.retain_until.seconds;
    if (!orbit_display::setText(decoded.node_id, message.node_id) ||
        !orbit_display::setText(decoded.core_epoch, message.core_epoch) ||
        !orbit_display::setText(decoded.primary, message.primary.text) ||
        !orbit_display::setText(decoded.secondary, message.secondary.text) ||
        !orbit_display::setText(decoded.footer, message.footer.text)) {
      error = orbit_display::CodecError::kOversized;
      return false;
    }
    output = decoded;
    return true;
  }

  bool encodeNodeState(const orbit_display::NodeState& state, uint8_t* output,
                       size_t output_capacity, size_t& output_size,
                       orbit_display::CodecError& error) override {
    error = orbit_display::CodecError::kNone;
    output_size = 0U;
    if (output == nullptr || output_capacity == 0U) {
      error = orbit_display::CodecError::kOversized;
      return false;
    }

    orbit_v1_NodeState message = orbit_v1_NodeState_init_zero;
    message.has_metadata = true;
    message.metadata.revision = state.revision;
    message.metadata.has_produced_at = true;
    message.metadata.produced_at.seconds = static_cast<int64_t>(time(nullptr));
    char message_id[65];
    std::snprintf(message_id, sizeof(message_id), "%s-%llu",
                  state.node_epoch.value,
                  static_cast<unsigned long long>(state.revision));
    if (!copyText(message.metadata.message_id, message_id) ||
        !copyText(message.metadata.producer_id, state.node_id.value) ||
        !copyText(message.node_id, state.node_id.value) ||
        !copyText(message.node_epoch, state.node_epoch.value) ||
        !copyText(message.series_id, state.series_id.value) ||
        !copyText(message.model_id, state.model_id.value) ||
        !copyText(message.variant_id, state.variant_id.value) ||
        !copyText(message.firmware_version, state.firmware_version.value)) {
      error = orbit_display::CodecError::kOversized;
      return false;
    }

    pb_ostream_t stream = pb_ostream_from_buffer(output, output_capacity);
    if (!pb_encode(&stream, orbit_v1_NodeState_fields, &message)) {
      error = PB_GET_ERROR(&stream) != nullptr
                  ? orbit_display::CodecError::kOversized
                  : orbit_display::CodecError::kMalformed;
      return false;
    }
    output_size = stream.bytes_written;
    return true;
  }
};

}  // namespace

std::unique_ptr<orbit_display::IProtocolCodec> makeNanopbProtocolCodec() {
  return std::make_unique<NanopbProtocolCodec>();
}
