#include <Arduino.h>
#include <Adafruit_GFX.h>
#include <SPI.h>
#include <WiFi.h>
#include <memory>
#include "board.h"
#include "battery_algorithm.h"
#include "generated_config.h"
#include "mqtt_node_runtime.h"
#include "nanopb_protocol.h"
#include "orbit_display/smooth_font.h"
#include "orbit_display/weekly_details.h"

namespace {
using orbit_display::drawSmoothText;
using orbit_display::FontFace;
using orbit_display::TextAlign;
using orbit_display::TextSpacing;
constexpr uint16_t kBackground = 0x0861, kMuted = 0x94B2, kAccent = 0x57EA, kWarning = 0xFD20;
SPIClass spi(FSPI);
GFXcanvas16 canvas(320, 240);
orbit_display::ViewStore store;
std::unique_ptr<orbit_display::IProtocolCodec> codec;
std::unique_ptr<MqttNodeRuntime> runtime;
uint16_t battery[10]{};
size_t batteryCount = 0, batteryIndex = 0;
uint32_t lastFrame = 0;

void command(uint8_t value, const uint8_t* data = nullptr, size_t size = 0) {
  digitalWrite(board::kLcdCs, LOW);
  digitalWrite(board::kLcdDc, LOW);
  spi.transfer(value);
  digitalWrite(board::kLcdDc, HIGH);
  if (size) spi.writeBytes(data, size);
  digitalWrite(board::kLcdCs, HIGH);
}

void beginDisplay() {
  pinMode(board::kLcdCs, OUTPUT);
  pinMode(board::kLcdDc, OUTPUT);
  pinMode(board::kLcdReset, OUTPUT);
  digitalWrite(board::kLcdCs, HIGH);
  spi.begin(board::kLcdSclk, -1, board::kLcdMosi, board::kLcdCs);
  digitalWrite(board::kLcdReset, LOW); delay(20);
  digitalWrite(board::kLcdReset, HIGH); delay(120);
  spi.beginTransaction(SPISettings(40000000, MSBFIRST, SPI_MODE0));
  command(0x11); delay(120);
  const uint8_t pixel = 0x55, rotation = 0x60;
  command(0x3A, &pixel, 1); command(0x36, &rotation, 1);
  command(0x21); command(0x13); command(0x29);
  spi.endTransaction();
  ledcSetup(0, 25000, 10);
  ledcAttachPin(board::kLcdBacklight, 0);
  ledcWrite(0, generated_config::kDisplayContrast * 1023U / 255U);
  canvas.setTextWrap(false);
}

void push() {
  const uint8_t columns[] = {0, 0, 1, 63}, rows[] = {0, 0, 0, 239};
  spi.beginTransaction(SPISettings(40000000, MSBFIRST, SPI_MODE0));
  command(0x2A, columns, 4); command(0x2B, rows, 4); command(0x2C);
  digitalWrite(board::kLcdCs, LOW);
  static uint8_t line[640];
  for (int y = 0; y < 240; ++y) {
    for (int x = 0; x < 320; ++x) {
      const uint16_t pixel = canvas.getBuffer()[y * 320 + x];
      line[x*2] = pixel >> 8; line[x*2+1] = pixel;
    }
    spi.writeBytes(line, sizeof(line));
  }
  digitalWrite(board::kLcdCs, HIGH);
  spi.endTransaction();
}

void render() {
  if (!canvas.getBuffer()) return;
  auto snapshot = runtime->snapshot();
  battery[batteryIndex++ % 10] = analogRead(board::kBatteryAdc);
  if (batteryCount < 10) ++batteryCount;
  uint32_t sum = 0;
  for (size_t i=0; i<batteryCount; ++i) sum += battery[i];
  char power[24];
  snprintf(power, sizeof(power), "%s%u%%", digitalRead(board::kChargeStatus) == LOW ? "+" : "", batteryLevelFromAdc(sum/batteryCount));
  canvas.fillScreen(kBackground);
  drawSmoothText(canvas, FontFace::Small, power, 244, 8, 60, 14, kMuted, TextAlign::Right);
  const bool weekly = strcmp(snapshot.view.display_kind.value, "codex") == 0;
  const bool retained = snapshot.has_view && time(nullptr) < snapshot.view.retain_until;
  const char* primary = retained ? snapshot.view.primary.value : "--";
  const uint16_t primaryColor = weekly ? orbit_display::weeklyQuotaColor(primary)
                                       : snapshot.stale ? kWarning : 0xFFFF;
  drawSmoothText(canvas, FontFace::Primary, primary, 16, 32, 288, 112,
                 primaryColor, TextAlign::Center,
                 weekly ? TextSpacing::Monospace : TextSpacing::Compact);
  canvas.drawFastHLine(16, 160, 288, 0x2945);
  if (weekly) {
    const auto details = orbit_display::weeklyDetails(
        retained ? snapshot.view.secondary.value : nullptr,
        retained ? snapshot.view.footer.value : nullptr);
    drawSmoothText(canvas, FontFace::Small, "RESET", 16, 174, 176, 15, kMuted);
    drawSmoothText(canvas, FontFace::Secondary, details.reset, 16, 194, 176, 26, 0xFFFF);
    drawSmoothText(canvas, FontFace::Small, "REMAINING", 198, 174, 106, 15, kMuted, TextAlign::Right);
    drawSmoothText(canvas, FontFace::Secondary, details.remaining, 198, 194, 106, 26, 0xFFFF, TextAlign::Right);
  } else {
    drawSmoothText(canvas, FontFace::Small, "TOKENS", 16, 172, 130, 15, kMuted);
    drawSmoothText(canvas, FontFace::Secondary, retained ? snapshot.view.secondary.value : "--", 16, 190, 130, 26, 0xFFFF);
    drawSmoothText(canvas, FontFace::Small, "TPM", 174, 172, 130, 15, kMuted);
    drawSmoothText(canvas, FontFace::Secondary, retained ? snapshot.view.footer.value : "--", 174, 190, 130, 26, 0xFFFF);
  }
  const char* state = nullptr;
  if (snapshot.state == orbit_display::RuntimeState::kWifi) state = "CONNECTING WI-FI";
  else if (snapshot.state == orbit_display::RuntimeState::kTime) state = "SYNCING CLOCK";
  else if (snapshot.state == orbit_display::RuntimeState::kMqtt) state = "CONNECTING ORBIT";
  else if (snapshot.state == orbit_display::RuntimeState::kError) state = "CONNECTION / DATA ERROR";
  else if (!snapshot.has_view) state = "WAITING FOR CORE";
  else if (!retained || snapshot.stale) state = "STALE / WAITING FOR DATA";
  if (state) drawSmoothText(canvas, FontFace::Small, state,
                            weekly ? 32 : 16, weekly ? 7 : 221,
                            weekly ? 200 : 288, 16, kWarning);
  canvas.fillCircle(19, 15, 3, state ? kWarning : kAccent);
  push();
}
}  // namespace

void setup() {
  Serial.begin(115200);
  pinMode(board::kAmplifierEnable, OUTPUT); digitalWrite(board::kAmplifierEnable, LOW);
  pinMode(board::kChargeStatus, INPUT_PULLUP);
  analogReadResolution(12); analogSetPinAttenuation(board::kBatteryAdc, ADC_11db);
  beginDisplay();
  codec = makeNanopbProtocolCodec();
  runtime = std::make_unique<MqttNodeRuntime>(*codec, store);
  runtime->begin();
  Serial.printf("INFO orbit xiaozhi boot node_id=%s firmware=%s display=320x240 framebuffer=%s\n", generated_config::kNodeId, generated_config::kFirmwareVersion, canvas.getBuffer() ? "ok" : "failed");
  render();
}

void loop() {
  runtime->loop();
  if (millis() - lastFrame >= 500) { lastFrame = millis(); render(); }
  delay(1);
}
