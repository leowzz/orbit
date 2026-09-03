#include <unity.h>

#include <pb_decode.h>
#include <pb_encode.h>

#include <array>
#include <cstring>

#include "nanopb_protocol.h"
#include "orbit/v1/state.pb.h"
#include "orbit/v1/view.pb.h"

namespace {

template <size_t Size>
void setGeneratedText(char (&destination)[Size], const char* source) {
  std::strncpy(destination, source, Size - 1U);
  destination[Size - 1U] = '\0';
}

void test_decodes_device_view() {
  const uint8_t payload[] = {
      0x0a, 0x02, 0x18, 0x07, 0x12, 0x06, 0x6e, 0x6f, 0x64, 0x65,
      0x2d, 0x61, 0x1a, 0x0c, 0x63, 0x6f, 0x72, 0x65, 0x2d, 0x65,
      0x70, 0x6f, 0x63, 0x68, 0x2d, 0x61, 0x20, 0x01, 0x2a, 0x03,
      0x08, 0xd0, 0x0f, 0x32, 0x03, 0x08, 0xb8, 0x17, 0x3a, 0x08,
      0x0a, 0x06, 0x24, 0x31, 0x32, 0x2e, 0x33, 0x35, 0x42, 0x0a,
      0x0a, 0x08, 0x31, 0x2e, 0x32, 0x4d, 0x20, 0x54, 0x4f, 0x4b,
      0x4a, 0x0a, 0x0a, 0x08, 0x34, 0x2e, 0x35, 0x4b, 0x20, 0x54,
      0x50, 0x4d,
  };

  auto codec = makeNanopbProtocolCodec();
  orbit_display::DisplayView decoded;
  orbit_display::CodecError error = orbit_display::CodecError::kNone;
  TEST_ASSERT_TRUE(
      codec->decodeDeviceView(payload, sizeof(payload), decoded, error));
  TEST_ASSERT_EQUAL_STRING("node-a", decoded.node_id.value);
  TEST_ASSERT_EQUAL_STRING("core-epoch-a", decoded.core_epoch.value);
  TEST_ASSERT_EQUAL_UINT64(7U, decoded.revision);
  TEST_ASSERT_EQUAL_STRING("$12.35", decoded.primary.value);
}

void test_encodes_node_state_with_metadata() {
  orbit_display::NodeState state;
  state.revision = 3U;
  TEST_ASSERT_TRUE(orbit_display::setText(state.node_id, "node-a"));
  TEST_ASSERT_TRUE(orbit_display::setText(state.node_epoch, "node-epoch-a"));
  TEST_ASSERT_TRUE(orbit_display::setText(state.series_id, "display"));
  TEST_ASSERT_TRUE(orbit_display::setText(state.model_id, "oled-128x32"));
  TEST_ASSERT_TRUE(orbit_display::setText(state.variant_id, "yd-esp32-s3"));
  TEST_ASSERT_TRUE(orbit_display::setText(state.firmware_version, "0.1.0"));

  auto codec = makeNanopbProtocolCodec();
  std::array<uint8_t, orbit_display::kMaxNodeStatePayloadBytes> payload{};
  size_t payload_size = 0U;
  orbit_display::CodecError error = orbit_display::CodecError::kNone;
  TEST_ASSERT_TRUE(codec->encodeNodeState(state, payload.data(), payload.size(),
                                          payload_size, error));

  orbit_v1_NodeState decoded = orbit_v1_NodeState_init_zero;
  pb_istream_t stream = pb_istream_from_buffer(payload.data(), payload_size);
  TEST_ASSERT_TRUE(pb_decode(&stream, orbit_v1_NodeState_fields, &decoded));
  TEST_ASSERT_TRUE(decoded.has_metadata);
  TEST_ASSERT_EQUAL_UINT64(3U, decoded.metadata.revision);
  TEST_ASSERT_EQUAL_STRING("node-a", decoded.metadata.producer_id);
  TEST_ASSERT_EQUAL_STRING("node-epoch-a", decoded.node_epoch);
  TEST_ASSERT_EQUAL_STRING("yd-esp32-s3", decoded.variant_id);
}

}  // namespace

int main(int, char**) {
  UNITY_BEGIN();
  RUN_TEST(test_decodes_device_view);
  RUN_TEST(test_encodes_node_state_with_metadata);
  return UNITY_END();
}
