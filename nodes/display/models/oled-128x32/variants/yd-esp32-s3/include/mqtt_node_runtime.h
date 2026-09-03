#pragma once

#include <ArduinoMqttClient.h>

#include "generated_config.h"

#if ORBIT_MQTT_TLS
#include <WiFiClientSecure.h>
#else
#include <WiFiClient.h>
#endif

#include "orbit_display/protocol_codec.h"
#include "orbit_display/view_state.h"

class MqttNodeRuntime {
 public:
  MqttNodeRuntime(orbit_display::IProtocolCodec& codec,
                  orbit_display::ViewStore& view_store);

  void begin();
  void loop();
  orbit_display::DisplaySnapshot snapshot() const;

 private:
  static void messageThunk(int message_size);
  void handleMessage(int message_size);
  bool connectMqtt();
  bool publishNodeState();
  bool clockTrusted() const;
  void setError(orbit_display::RuntimeState state,
                orbit_display::DisplayError error);

#if ORBIT_MQTT_TLS
  WiFiClientSecure network_client_;
#else
  WiFiClient network_client_;
#endif
  MqttClient mqtt_client_;
  orbit_display::IProtocolCodec& codec_;
  orbit_display::ViewStore& view_store_;
  orbit_display::RuntimeState state_{orbit_display::RuntimeState::kBoot};
  orbit_display::DisplayError error_{orbit_display::DisplayError::kNone};
  orbit_display::EpochId node_epoch_{};
  uint64_t node_revision_{1};
  uint32_t next_wifi_attempt_ms_{0};
  uint32_t next_mqtt_attempt_ms_{0};

  static MqttNodeRuntime* active_instance_;
};
