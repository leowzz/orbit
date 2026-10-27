"""Build, package and explicitly flash the Ma Xin WiFi variant on any host."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

PROJECT = Path(__file__).resolve().parents[1]
OLED = PROJECT.parents[2] / "oled-128x32/variants/yd-esp32-s3"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["build", "package", "upload", "monitor"])
    parser.add_argument("--config", type=Path, default=PROJECT / "config.local.yaml")
    parser.add_argument("--port", help="Explicit USB port, e.g. COM11 or /dev/ttyACM0")
    args = parser.parse_args()
    if args.action in ("upload", "monitor") and not args.port:
        parser.error("--port is required; serial auto-selection is disabled")
    env = dict(os.environ, APP_CONFIG_FILE=str(args.config.resolve()))

    def run(*command):
        subprocess.run(command, cwd=PROJECT, env=env, check=True)

    if args.action == "monitor":
        run(sys.executable, "-m", "platformio", "device", "monitor", "-p", args.port, "-b", "115200", "--dtr", "0", "--rts", "0")
        return
    run(sys.executable, str(OLED / "scripts/generate_proto.py"), "--output", str(PROJECT / "src/generated-proto"))
    command = [sys.executable, "-m", "platformio", "run", "-e", "ma_xin_wifi"]
    if args.action == "upload":
        command += ["-t", "upload", "--upload-port", args.port]
    run(*command)
    if args.action != "package":
        return
    build = PROJECT / ".pio/build/ma_xin_wifi"
    packages = Path(os.environ.get("PLATFORMIO_CORE_DIR", Path.home() / ".platformio")) / "packages"
    output = PROJECT / "dist"
    output.mkdir(exist_ok=True)
    parts = [("0x0", build / "bootloader.bin"), ("0x8000", build / "partitions.bin"),
             ("0xe000", packages / "framework-arduinoespressif32/tools/partitions/boot_app0.bin"),
             ("0x10000", build / "firmware.bin")]
    run(sys.executable, str(packages / "tool-esptoolpy/esptool.py"), "--chip", "esp32s3", "merge_bin",
        "--flash_size", "16MB", "-o", str(output / "factory.bin"),
        *[value for offset, path in parts for value in (offset, str(path))])
    shutil.copy2(build / "firmware.bin", output / "firmware.bin")
    manifest = {"model": "xiaozhi-320x240", "variant": "ma-xin-wifi", "flash_size": "16MB",
                "factory_offset": "0x0", "application_offset": "0x10000",
                "contains_private_credentials": True,
                "sha256": {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in (output / "factory.bin", output / "firmware.bin")}}
    (output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print("Private firmware package:", output)


if __name__ == "__main__":
    main()
