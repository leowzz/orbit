# Orbit v1 protocol

Versioned Protobuf source files belong here. Define the message contracts and
their size limits before adding generated bindings under `gen/go`.

Generated code must not be edited manually.

`ObservationType` keeps `usage = 1` stable and adds `codex = 2`. A Codex
observation contains only bounded, sanitized session summaries: session ID,
display/project labels, model, normalized status, last update time, and process
liveness, together with total/running counts and the observation timestamp.
The `Observation` oneof keeps the existing `usage = 3` field and adds
`codex = 4`; consumers that do not understand Codex continue to decode and
ignore the new oneof member.

`DeviceView` keeps its OLED slots wire-compatible and adds typed `UsageView`
and `CodexView` sections for the `overview-web` projection. Core never includes
those rich sections in `usage-oled-128x32` views.
