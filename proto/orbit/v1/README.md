# Orbit v1 protocol

Versioned Protobuf source files belong here. Define the message contracts and
their size limits before adding generated bindings under `gen/go`.

Generated code must not be edited manually.

`ObservationType` keeps `usage = 1` and `codex = 2` stable and adds
`system = 3`. A Codex observation contains only bounded, sanitized session
summaries: session ID, display/project labels, model, normalized status, last
update time, and process liveness, together with total/running counts and the
observation timestamp. A System observation contains only host-level resource
telemetry: CPU usage permille, logical CPU count, memory and swap byte totals,
optional 1-minute load average, uptime, and observation timestamp. The
`Observation` oneof keeps `usage = 3` and `codex = 4` and adds `system = 5`;
consumers that do not understand newer members continue to decode and ignore
them.

`DeviceView` keeps its OLED slots wire-compatible and adds typed `UsageView`,
`CodexView`, and `SystemView` sections for rich projections (`overview-web`,
`overview-android`, `overview-app`). Core never includes those rich sections in
physical display views (`*-oled-128x32`, `metrics-xiaozhi-320x240`).
