#!/usr/bin/env bash
set -euo pipefail

TAG="${1:?usage: package-esp32s3.sh TAG OUTPUT_DIR}"
OUTPUT_DIR="${2:?usage: package-esp32s3.sh TAG OUTPUT_DIR}"
[[ "$TAG" =~ ^v[A-Za-z0-9_.-]+$ ]] || { echo 'Invalid release tag' >&2; exit 1; }
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NODE_DIR="$ROOT/nodes/display/models/oled-128x32/variants/yd-esp32-s3"
PIO_DIR="${PLATFORMIO_CORE_DIR:-$HOME/.platformio}"
BUILD_DIR="$NODE_DIR/.pio/build/yd_esp32s3"
mkdir -p "$OUTPUT_DIR"
OUTPUT_DIR="$(cd "$OUTPUT_DIR" && pwd)"
PACKAGE="orbit-esp32s3-yd-oled-128x32-$TAG"
STAGING="$(mktemp -d)"
trap 'rm -rf "$STAGING"' EXIT
mkdir "$STAGING/$PACKAGE"
cp "$BUILD_DIR/firmware.bin" "$BUILD_DIR/bootloader.bin" "$BUILD_DIR/partitions.bin" "$STAGING/$PACKAGE/"
cp "$PIO_DIR/packages/framework-arduinoespressif32/tools/partitions/boot_app0.bin" "$STAGING/$PACKAGE/"

# Offsets match the pinned espressif32 Arduino builder for ESP32-S3.
cd "$NODE_DIR"
uv run python "$PIO_DIR/packages/tool-esptoolpy/esptool.py" --chip esp32s3 merge_bin \
  -o "$STAGING/$PACKAGE/factory.bin" \
  0x0000 "$STAGING/$PACKAGE/bootloader.bin" \
  0x8000 "$STAGING/$PACKAGE/partitions.bin" \
  0xe000 "$STAGING/$PACKAGE/boot_app0.bin" \
  0x10000 "$STAGING/$PACKAGE/firmware.bin"
cat > "$STAGING/$PACKAGE/FLASH.md" <<'EOF'
# YD-ESP32-S3 / OLED 128x32

This public build embeds example Wi-Fi and MQTT placeholders. It boots but cannot
connect to your network. Build from source with config.local.yaml for actual use.
The release signature verifies the download; it does not enable ESP Secure Boot.

After verifying the archive with Cosign, extract it and flash the factory image:

```sh
python -m pip install esptool==4.11.0
python -m esptool --chip esp32s3 --port PORT write_flash 0x0 factory.bin
```

Replace PORT with your serial port. Alternatively flash the component images:

```sh
python -m esptool --chip esp32s3 --port PORT write_flash \
  0x0000 bootloader.bin 0x8000 partitions.bin 0xe000 boot_app0.bin 0x10000 firmware.bin
```
EOF
tar -czf "$OUTPUT_DIR/$PACKAGE.tar.gz" -C "$STAGING" "$PACKAGE"
echo "Packaged $OUTPUT_DIR/$PACKAGE.tar.gz"
