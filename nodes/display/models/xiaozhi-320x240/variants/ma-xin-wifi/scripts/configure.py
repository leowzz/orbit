"""Use the same strict private configuration contract as the OLED node."""
import os
import sys
from pathlib import Path

Import("env")
project = Path(env.subst("$PROJECT_DIR"))
shared_scripts = project.parents[2] / "oled-128x32/variants/yd-esp32-s3/scripts"
sys.path.insert(0, str(shared_scripts))
from config_codegen import load_config, render_header

config_path = Path(os.environ.get("APP_CONFIG_FILE", "config.local.yaml"))
if not config_path.is_absolute():
    config_path = project / config_path
generated = Path(env.subst("$BUILD_DIR")) / "generated"
generated.mkdir(parents=True, exist_ok=True)
(generated / "generated_config.h").write_text(render_header(load_config(config_path)), encoding="utf-8")
env.Append(CPPPATH=[str(generated)])
