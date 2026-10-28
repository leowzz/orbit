# Ma Xin WiFi ORBIT firmware

Hardware: ESP32-S3, 16 MB flash, 8 MB OPI PSRAM, ST7789 320x240 landscape.
Board pins and LCD initialization derive from `leowzz/xiaozhi-vibe` commit
`3165eceea009ec427b74e8fbc1d92944d6a0e6f5` (`include/board.h`,
`include/battery_algorithm.h`, `src/dashboard_display.cpp`). The ORBIT protocol,
MQTT transport and freshness logic are shared with the OLED node.

## Build

Run from this directory with uv installed:

```sh
uv sync --frozen
cp config.example.yaml config.local.yaml
# Edit node.id, wifi, mqtt, mqtt.tls.ca_file and optional brightness.
uv run python scripts/firmware.py package
```

`config.local.yaml`, `certs/`, `.pio/`, generated bindings, and `dist/` are ignored.
Use the broker's trusted root PEM; TLS verification is enabled by default.
Real credentials are embedded in both images; keep personalized packages private.
`dist/firmware.bin` is the application at `0x10000`; `dist/factory.bin` is the
merged bootloader/partitions/application image at `0x0`. `manifest.json` records
their SHA-256 hashes. Public placeholder builds use
`uv run python scripts/firmware.py package --config config.example.yaml`, also
available from the repository root as `make build-xiaozhi` and checked in CI.

## USB deployment

Explicitly identify the serial port and back up flash before first installation:

```sh
uv run python -m platformio device list
uv run --with esptool python -m esptool --port COM11 read-flash 0 ALL backup.bin
uv run python scripts/firmware.py upload --port COM11
uv run python scripts/firmware.py monitor --port COM11
```

Replace COM11 with the actual port. Upload/monitor refuse automatic port selection.
Store backups privately outside Git. To restore a full backup:
`uv run --with esptool python -m esptool --port COM11 write-flash 0 backup.bin`.
Upload changes bootloader, partition table and application; retain a full backup
before switching from the original firmware. This is a metrics display firmware,
not the original voice assistant or microphone loopback application.

To change Wi-Fi later, edit `wifi` in `config.local.yaml` and repeat package/upload.
The OLED and Xiaozhi use the same strict configuration schema and validation.

## Core management

1. Run a Core version that recognizes this model.
2. In **MQTT 账号**, create a Node account scoped to the configured `node.id`.
3. Put the resulting account and broker address in the local firmware config.
4. After Wi-Fi, NTP and TLS connect, the firmware publishes retained NodeState.
5. In **设备 → 小智彩屏 → 内容与显示**, choose source Agent(s), API usage,
   weekly quota or rotation. Renaming and archiving use the normal Core controls.

Example route (managed through Core's route API/console, not firmware):

```yaml
profile: metrics-xiaozhi-320x240
display_mode: rotating
rotation_seconds: 5
sessions_enabled: false
inputs:
  - agent_id: your-agent
    observation_type: usage
  - agent_id: your-agent
    observation_type: codex
```

Usage shows USD cost rounded to one decimal by Core (for example `$20.2`), token
count and TPM. Larger amounts retain all integer digits. Weekly view shows remaining percent,
colored red at 10% or below, yellow above 10% through 30%, white above 30% through
90%, and green above 90%. Reset date/time (UTC+8) is grouped at the bottom left
and remaining days/hours at the bottom right (for example `5d 14h`). Missing data displays `--`;
expired observations are marked `STALE` and retained values disappear after their
retention deadline. Wi-Fi/time/MQTT failures have distinct connection messages.
Battery percent uses the upstream empirical ADC algorithm, not a calibrated gauge.

## Typography

The selected typeface is JetBrains Mono: weight 500 for the main percentage/cost,
weight 400 for supporting text, with centered layout 01 from the layout comparison.
API prices use compact optical spacing around the currency symbol, digits and
decimal point. Quota and supporting text retain monospace advances. Glyphs are
cropped to their actual coverage before measuring and fitting the text.
Glyphs use 16 coverage levels, RGB565 blending, and bilinear downscaling to fit
long values. Checked-in glyph data means normal firmware builds need no font
download or rasterizer. The original variable font, provenance and SIL Open Font
License are in `../../assets/fonts/`.
Local display libraries use `symlink://` references so changing generated glyph
data rebuilds the renderer instead of reusing PlatformIO's copied library cache.

To regenerate glyph data from the repository root:

```sh
uv run --with pillow==12.0.0 python nodes/display/models/xiaozhi-320x240/scripts/generate_fonts.py
```

## Validation

Core tests cover discovery, incompatible products/routes, page switching,
rotation, missing quota, and bounded session-free payloads. Shared native firmware
tests cover codec, node identity, revision and freshness handling. Hardware logs
should contain `framebuffer=ok`, Wi-Fi/clock/TLS connection, NodeState publication,
and accepted DeviceViews for both `usage` and `codex` pages.
