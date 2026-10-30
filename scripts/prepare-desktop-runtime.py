from pathlib import Path
import shutil
import sys


root = Path(__file__).resolve().parents[1]
source = root / "runtime" / "speech-0.8.11" / "package"
target = root / "agents" / "desktop" / "src-tauri" / "resources" / "speech-runtime"

required = [source / "speech.dll", source / "opus.dll"]
missing = [path for path in required if not path.is_file()]
if missing:
    names = ", ".join(path.name for path in missing)
    print(
        f"desktop runtime is missing ({names}); prepare speech.cpp v0.8.11 under {source}",
        file=sys.stderr,
    )
    raise SystemExit(1)

target.mkdir(parents=True, exist_ok=True)
for path in source.iterdir():
    destination = target / path.name
    if path.is_file():
        shutil.copy2(path, destination)
print(f"prepared desktop runtime in {target}")
