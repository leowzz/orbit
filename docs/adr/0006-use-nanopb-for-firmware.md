---
status: accepted
---

# Use nanopb for firmware Protobuf

Orbit Display firmware uses nanopb with compile-time bounds for strings and repeated fields. Payload limits will be fixed only after measuring generated message sizes and peak memory on the YD ESP32-S3 target, rather than inheriting the unrelated legacy HTTP response limit.
