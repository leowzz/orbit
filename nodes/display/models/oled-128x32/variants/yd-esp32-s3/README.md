# Orbit Display OLED 128x32 on YD-ESP32-S3

This is the `display` / `oled-128x32` / `yd-esp32-s3` node variant. The
application receives only the Core-produced Protobuf `DeviceView`; it has no
Sub2API, Codex, or host login credentials.

## Hardware

- Board: YD-ESP32-S3 (`esp32-s3-devkitc-1`)
- OLED: SSD1306, 128x32, I2C address `0x3C`, 3.3 V
- SDA: GPIO5
- SCL: GPIO6

The model renderer keeps the last accepted view in RAM and marks it stale when
the absolute `fresh_until` is reached. A retained view received after startup
is allowed to render stale while the clock is untrusted. Same-epoch views with
an older or duplicate revision are rejected.

## MQTT contract

The node subscribes to the retained Protobuf topic
`orbit/v1/nodes/{node_id}/view` with QoS 1 and publishes its retained
Protobuf NodeState to `orbit/v1/nodes/{node_id}/state` with QoS 1. The node
epoch is generated once per boot and the NodeState revision increases on each
state publish. The codec boundary is
`common/include/orbit_display/protocol_codec.h`.

`make proto` generates bounded nanopb bindings from the root Orbit schemas into
the ignored `src/generated-proto` directory. The local codec decodes
`DeviceView` and encodes `NodeState` against those generated types; it does not
fall back to JSON.

The current ArduinoMqttClient transport negotiates MQTT 3.1.1. QoS 1 and
retained messages interoperate with the MQTT 5 broker, but MQTT 5-only CONNECT
and publish properties are not yet exercised by this firmware. Treat that as a
deployment acceptance gap rather than claiming full MQTT 5 client coverage.

## Configuration

Copy `config.example.yaml` to the ignored `config.local.yaml`. The schema is
in `config.schema.yaml`. Wi-Fi and MQTT credentials are build-time values and
must not be committed. TLS defaults to enabled; replace the public example CA
with the root certificate used by the selected broker.

## Commands

```bash
uv sync
APP_CONFIG_FILE=config.example.yaml uv run python -m pytest -q
uv run pio test -e native
make proto
make build CONFIG=config.example.yaml
make upload PORT=/dev/cu.usbmodemXXXX
make monitor PORT=/dev/cu.usbmodemXXXX
```

`make test` validates the node YAML contract. `make native` validates view
revision/freshness state and dirty-region calculation. `make build` regenerates
nanopb sources before compiling the YD-ESP32-S3 target.
