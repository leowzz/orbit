#include <Arduino.h>
#include <U8g2lib.h>
#include <Wire.h>

#include <memory>

#include "generated_config.h"
#include "mqtt_node_runtime.h"
#include "nanopb_protocol.h"
#include "orbit_display/oled_renderer.h"

namespace {

constexpr uint8_t kSdaPin = 5U;
constexpr uint8_t kSclPin = 6U;
constexpr uint8_t kDisplayAddress = 0x3CU;
constexpr uint32_t kI2cClockHz = 100000U;
constexpr uint32_t kDisplayRetryMs = 2000U;
constexpr uint32_t kFrameIntervalMs = 100U;

U8G2_SSD1306_128X32_UNIVISION_F_SW_I2C display(
    U8G2_R0, kSclPin, kSdaPin, U8X8_PIN_NONE);
std::unique_ptr<orbit_display::IProtocolCodec> protocol;
orbit_display::ViewStore view_store;
MqttNodeRuntime* runtime = nullptr;
bool display_ready = false;
bool full_refresh_needed = true;
int16_t applied_brightness = -1;
uint32_t last_display_retry_ms = 0U;
uint32_t last_frame_ms = 0U;

void applyDisplayBrightness(uint8_t percent) {
  if (percent > 100U) percent = 100U;
  if (static_cast<int16_t>(percent) == applied_brightness) return;
  const bool waking = applied_brightness <= 0 && percent > 0U;
  applied_brightness = percent;
  if (percent == 0U) {
    display.setPowerSave(1);
    Serial.println("INFO oled brightness=0 power_save=1");
    return;
  }
  const uint8_t contrast = static_cast<uint8_t>(
      (static_cast<uint16_t>(percent) * 255U + 50U) / 100U);
  display.setPowerSave(0);
  display.setContrast(contrast);
  if (waking) full_refresh_needed = true;
  Serial.printf("INFO oled brightness=%u contrast=%u\n", percent, contrast);
}

void printI2cDevice(uint8_t address) {
  Serial.printf("INFO i2c device discovered address=0x%02X\n", address);
}

bool startDisplay() {
  bool found = false;
  for (uint8_t address = 0x08U; address <= 0x77U; ++address) {
    Wire.beginTransmission(address);
    if (Wire.endTransmission() != 0) continue;
    printI2cDevice(address);
    found = found || address == kDisplayAddress;
  }
  if (!found) {
    Serial.println("WARN i2c display missing address=0x3C");
    return false;
  }

  Wire.end();
  display.setI2CAddress(kDisplayAddress << 1U);
  display.setBusClock(kI2cClockHz);
  display.initDisplay();
  applied_brightness = -1;
  applyDisplayBrightness(generated_config::kDisplayBrightnessPercent);
  full_refresh_needed = true;
  Serial.println("INFO oled ready address=0x3C size=128x32");
  return true;
}

}  // namespace

void setup() {
  Serial.begin(115200);
  Serial.printf("INFO orbit display boot node_id=%s firmware_version=%s\n",
                generated_config::kNodeId,
                generated_config::kFirmwareVersion);
  Wire.begin(kSdaPin, kSclPin);
  Wire.setClock(kI2cClockHz);
  display_ready = startDisplay();
  last_display_retry_ms = millis();

  protocol = makeNanopbProtocolCodec();
  runtime = new MqttNodeRuntime(*protocol, view_store);
  runtime->begin();
}

void loop() {
  const uint32_t now = millis();
  if (!display_ready && now - last_display_retry_ms >= kDisplayRetryMs) {
    last_display_retry_ms = now;
    display_ready = startDisplay();
  }

  if (runtime != nullptr) {
    runtime->loop();
  }
  if (display_ready && now - last_frame_ms >= kFrameIntervalMs) {
    last_frame_ms = now;
    const orbit_display::DisplaySnapshot snapshot =
        runtime == nullptr
            ? view_store.snapshot(orbit_display::RuntimeState::kBoot,
                                  orbit_display::DisplayError::kNone)
            : runtime->snapshot();
    const uint8_t target_brightness =
        snapshot.has_view && snapshot.view.has_brightness_percent
            ? snapshot.view.brightness_percent
            : generated_config::kDisplayBrightnessPercent;
    applyDisplayBrightness(target_brightness);
    if (applied_brightness != 0) {
      orbit_display::renderUi(display, snapshot, full_refresh_needed,
                              orbit_display::computePixelShift(now));
      full_refresh_needed = false;
    }
  }
  delay(1);
}
