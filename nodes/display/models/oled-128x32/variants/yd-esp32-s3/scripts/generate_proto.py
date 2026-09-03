import argparse
from importlib import resources
import os
from pathlib import Path
import shutil
import subprocess
import sys


def _find_repo_root(start: Path) -> Path:
    for candidate in (start, *start.parents):
        if (candidate / "buf.yaml").is_file():
            return candidate
    raise SystemExit("nanopb generation unavailable: repository root with buf.yaml was not found")


def _generator_command(value: str) -> list[str]:
    resolved = shutil.which(value) or (value if Path(value).exists() else "")
    if not resolved:
        raise SystemExit(
            "nanopb generation unavailable: set NANOPB_GENERATOR to nanopb_generator or an executable"
        )
    if resolved.endswith(".py"):
        return [sys.executable, resolved]
    return [resolved]


def main() -> int:
    parser = argparse.ArgumentParser(description="Generate Orbit nanopb bindings into the local build tree")
    parser.add_argument("--proto-root", type=Path, default=None)
    parser.add_argument("--output", type=Path, default=Path("src/generated-proto"))
    args = parser.parse_args()

    project_dir = Path(__file__).resolve().parents[1]
    repo_root = _find_repo_root(project_dir)
    proto_root = args.proto_root or Path(os.environ.get("ORBIT_PROTO_ROOT", repo_root / "proto"))
    sources = sorted((proto_root / "orbit" / "v1").glob("*.proto"))
    if not sources:
        raise SystemExit(
            f"nanopb generation unavailable: no .proto sources under {proto_root / 'orbit' / 'v1'}"
        )

    output = args.output if args.output.is_absolute() else project_dir / args.output
    output.mkdir(parents=True, exist_ok=True)
    grpc_proto_root = Path(str(resources.files("grpc_tools") / "_proto"))
    timestamp_proto = grpc_proto_root / "google" / "protobuf" / "timestamp.proto"
    command = _generator_command(os.environ.get("NANOPB_GENERATOR", "nanopb_generator"))
    command.extend([
        "--quiet",
        "--proto-path", str(proto_root),
        "--proto-path", str(grpc_proto_root),
        "--options-file", str(project_dir / "scripts" / "orbit.options"),
        "--output-dir", str(output),
    ])
    command.extend(str(source.relative_to(proto_root)) for source in sources)
    command.append(str(timestamp_proto))
    subprocess.run(command, check=True, cwd=proto_root)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
