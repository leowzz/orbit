#include "mqtt_node_runtime.h"

#include <Arduino.h>
#include <WiFi.h>

#include <array>
#include <cstdio>
#include <ctime>

#include "generated_config.h"

namespace {

constexpr uint32_t kMinimumValidEpoch = 1700000000UL;
constexpr uint32_t kWifiRetryMs = 5000U;
constexpr uint32_t kMqttRetryMs = 5000U;

bool isDue(uint32_t now, uint32_t deadline) {
  return static_cast<int32_t>(now - deadline) >= 0;
}

}  // namespace

MqttNodeRuntime* MqttNodeRuntime::active_instance_ = nullptr;

MqttNodeRuntime::MqttNodeRuntime(orbit_display::IProtocolCodec& codec,
                                 orbit_display::ViewStore& view_store)
    : mqtt_client_(network_client_), codec_(codec), view_store_(view_store) {
  char epoch_text[38];
  std::snprintf(epoch_text, sizeof(epoch_text), "node-%08lx%08lx%08lx%08lx",
                static_cast<unsigned long>(esp_random()),
                static_cast<unsigned long>(esp_random()),
                static_cast<unsigned long>(esp_random()),
                static_cast<unsigned long>(esp_random()));
  orbit_display::setText(node_epoch_, epoch_text);
  active_instance_ = this;
}

void MqttNodeRuntime::begin() {
  WiFi.mode(WIFI_STA);
  WiFi.begin(generated_config::kWifiSsid, generated_config::kWifiPassword);
  configTime(0, 0, "pool.ntp.org", "time.nist.gov");
  mqtt_client_.onMessage(messageThunk);
#if ORBIT_MQTT_TLS
  network_client_.setCACert(generated_config::kMqttCaPem);
#endif
  state_ = orbit_display::RuntimeState::kWifi;
  error_ = orbit_display::DisplayError::kNone;
}

void MqttNodeRuntime::loop() {
  const uint32_t now = millis();
  const bool trusted_clock = clockTrusted();
  view_store_.updateFreshness(static_cast<int64_t>(time(nullptr)),
                              trusted_clock);

  if (WiFi.status() != WL_CONNECTED) {
    setError(orbit_display::RuntimeState::kWifi,
             orbit_display::DisplayError::kNone);
    if (isDue(now, next_wifi_attempt_ms_)) {
      WiFi.disconnect();
      WiFi.begin(generated_config::kWifiSsid, generated_config::kWifiPassword);
      next_wifi_attempt_ms_ = now + kWifiRetryMs;
    }
    return;
  }
  if (!trusted_clock) {
    setError(orbit_display::RuntimeState::kTime,
             orbit_display::DisplayError::kNone);
    return;
  }
  if (!mqtt_client_.connected()) {
    setError(orbit_display::RuntimeState::kMqtt,
             orbit_display::DisplayError::kNone);
    if (isDue(now, next_mqtt_attempt_ms_)) {
      connectMqtt();
      next_mqtt_attempt_ms_ = now + kMqttRetryMs;
    }
    return;
  }

  mqtt_client_.poll();
  state_ = orbit_display::RuntimeState::kReady;
  error_ = orbit_display::DisplayError::kNone;
}

orbit_display::DisplaySnapshot MqttNodeRuntime::snapshot() const {
  return view_store_.snapshot(state_, error_);
}

void MqttNodeRuntime::messageThunk(int message_size) {
  if (active_instance_ != nullptr) {
    active_instance_->handleMessage(message_size);
  }
}

void MqttNodeRuntime::handleMessage(int message_size) {
  std::array<uint8_t, orbit_display::kMaxDeviceViewPayloadBytes> payload{};
  size_t copied = 0U;
  while (mqtt_client_.available()) {
    const int value = mqtt_client_.read();
    if (value < 0) break;
    if (copied < payload.size()) {
      payload[copied++] = static_cast<uint8_t>(value);
    }
  }
  if (message_size < 0 ||
      static_cast<size_t>(message_size) > payload.size()) {
    setError(orbit_display::RuntimeState::kError,
             orbit_display::DisplayError::kOversized);
    return;
  }
  if (copied != static_cast<size_t>(message_size)) {
    setError(orbit_display::RuntimeState::kError,
             orbit_display::DisplayError::kProtocol);
    return;
  }

  orbit_display::DisplayView decoded;
  orbit_display::CodecError codec_error = orbit_display::CodecError::kNone;
  if (!codec_.decodeDeviceView(payload.data(), static_cast<size_t>(message_size),
                               decoded, codec_error)) {
    const auto error = codec_error == orbit_display::CodecError::kOversized
                           ? orbit_display::DisplayError::kOversized
                           : orbit_display::DisplayError::kProtocol;
    setError(orbit_display::RuntimeState::kError, error);
    return;
  }

  const orbit_display::ViewApplyResult result =
      view_store_.apply(decoded, generated_config::kNodeId);
  if (result != orbit_display::ViewApplyResult::kAccepted) {
    setError(orbit_display::RuntimeState::kError,
             result == orbit_display::ViewApplyResult::kWrongNode
                 ? orbit_display::DisplayError::kRejected
                 : orbit_display::DisplayError::kProtocol);
    return;
  }
  state_ = orbit_display::RuntimeState::kReady;
  error_ = orbit_display::DisplayError::kNone;
}

bool MqttNodeRuntime::connectMqtt() {
  mqtt_client_.setId(generated_config::kNodeId);
  mqtt_client_.setUsernamePassword(generated_config::kMqttUsername,
                                   generated_config::kMqttPassword);
  mqtt_client_.setKeepAliveInterval(
      static_cast<unsigned long>(generated_config::kMqttKeepAliveSeconds) *
      1000UL);
  if (!mqtt_client_.connect(generated_config::kMqttHost,
                           generated_config::kMqttPort)) {
    setError(orbit_display::RuntimeState::kError,
             orbit_display::DisplayError::kMqtt);
    return false;
  }

  mqtt_client_.subscribe(generated_config::kViewTopic, 1);
  if (!publishNodeState()) {
    setError(orbit_display::RuntimeState::kError,
             orbit_display::DisplayError::kProtocol);
    mqtt_client_.stop();
    return false;
  }
  return true;
}

bool MqttNodeRuntime::publishNodeState() {
  orbit_display::NodeState node_state;
  if (!orbit_display::setText(node_state.node_epoch, node_epoch_.value)) {
    return false;
  }
  node_state.revision = node_revision_++;
  if (!orbit_display::setText(node_state.node_id, generated_config::kNodeId) ||
      !orbit_display::setText(node_state.series_id, "display") ||
      !orbit_display::setText(node_state.model_id, "oled-128x32") ||
      !orbit_display::setText(node_state.variant_id, "yd-esp32-s3") ||
      !orbit_display::setText(node_state.firmware_version,
                              generated_config::kFirmwareVersion)) {
    return false;
  }

  std::array<uint8_t, orbit_display::kMaxNodeStatePayloadBytes> payload{};
  size_t payload_size = 0U;
  orbit_display::CodecError codec_error = orbit_display::CodecError::kNone;
  if (!codec_.encodeNodeState(node_state, payload.data(), payload.size(),
                              payload_size, codec_error) ||
      payload_size > payload.size()) {
    return false;
  }

  mqtt_client_.beginMessage(generated_config::kStateTopic, payload_size, true,
                            1);
  const size_t written = mqtt_client_.write(payload.data(), payload_size);
  mqtt_client_.endMessage();
  return written == payload_size;
}

bool MqttNodeRuntime::clockTrusted() const {
  return static_cast<int64_t>(time(nullptr)) >= kMinimumValidEpoch;
}

void MqttNodeRuntime::setError(orbit_display::RuntimeState state,
                               orbit_display::DisplayError error) {
  state_ = state;
  error_ = error;
}
