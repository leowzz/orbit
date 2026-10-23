"""Run after installing the worker's base dependencies (no model or GUI needed)."""
from orbit_asr.importer import main

if __name__ == "__main__":
    raise SystemExit(main())
