# Xiaozhi 320x240

ORBIT display model `display/xiaozhi-320x240`, hardware variant `ma-xin-wifi`.
This is the ESP32-S3 N16R8 / ST7789 hardware from
[leowzz/xiaozhi-vibe](https://github.com/leowzz/xiaozhi-vibe), with a native ORBIT
MQTT node firmware. See [build and provisioning](variants/ma-xin-wifi/README.md).

Core profile: `metrics-xiaozhi-320x240`. Explicit `display_mode` is required:
`usage`, `codex` (weekly remaining quota), or `rotating`. Source Agents and
rotation intervals are managed in Core's device page and stored in SQLite.
The device receives only formatted slots and page identity; it has no upstream
API keys or Codex database access. Session lists and shortcut execution are not
part of this metrics firmware. The microphone and amplifier are not enabled.
