# Orbit Display

This directory contains the shared C++17 firmware for the `display` Device
Series. Shared MQTT, protocol, state, and rendering foundations belong in
`common/`; model-specific display behavior belongs in `models/`.

Hardware variants provide board and pin configuration only. They must not copy
the complete application implementation.

