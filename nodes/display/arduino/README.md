# Shared Arduino display transport

OLED and Xiaozhi use this library for MQTT/TLS and bounded nanopb messages.
The product selects `ORBIT_MODEL_ID` and `ORBIT_VARIANT_ID` at compile time;
defaults preserve the existing OLED identity. Core owns all source selection.
Generated protobuf headers and private configuration are supplied by each build.
`mqtt_node_runtime.cpp` is excluded on native hosts with `ARDUINO`; the codec is
also exercised by the OLED native protocol tests.
