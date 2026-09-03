import os
import sys
from pathlib import Path

Import("env")  # type: ignore[name-defined]

project_dir = Path(env.subst("$PROJECT_DIR"))  # type: ignore[name-defined]
sys.path.insert(0, str(project_dir))

from scripts.config_codegen import ConfigError, load_config, render_header


config_name = os.environ.get("APP_CONFIG_FILE", "config.local.yaml")
config_path = Path(config_name)
if not config_path.is_absolute():
    config_path = project_dir / config_path
generated_dir = Path(env.subst("$BUILD_DIR")) / "generated"  # type: ignore[name-defined]
generated_file = generated_dir / "generated_config.h"

try:
    config = load_config(config_path)
    header = render_header(config)
except (ConfigError, OSError) as error:
    raise RuntimeError(f"configuration generation failed: {error}") from error

generated_dir.mkdir(parents=True, exist_ok=True)
generated_file.write_text(header, encoding="utf-8")
env.Append(CPPPATH=[str(generated_dir)])  # type: ignore[name-defined]
