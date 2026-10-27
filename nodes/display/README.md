# Orbit Display

This directory contains the shared C++17 firmware for the `display` Device
Series. Platform-independent protocol interfaces and view state live in
`common/`; the shared Arduino MQTT/TLS runtime and nanopb codec live in
`arduino/`. Model-specific display behavior belongs in `models/`.

- [OLED 128x32](models/oled-128x32/README.md), YD-ESP32-S3 / SSD1306.
- [Xiaozhi 320x240](models/xiaozhi-320x240/README.md), Ma Xin WiFi / ST7789.

Hardware variants provide board and pin configuration only. They must not copy
the complete application implementation.

