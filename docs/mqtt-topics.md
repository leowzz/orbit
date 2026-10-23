# MQTT Topics

Status and command payloads use `application/protobuf` and MQTT QoS 1. The status-display
chain uses these topics:

| Topic | Publisher | Consumer | Retained |
| --- | --- | --- | --- |
| `orbit/v1/agents/{agent_id}/state` | Agent | Core | yes |
| `orbit/v1/agents/{agent_id}/observations/usage` | Agent | Core | no |
| `orbit/v1/agents/{agent_id}/observations/codex` | Agent | Core -> Web projection | no |
| `orbit/v1/nodes/{node_id}/state` | OLED/Web/Android Node | Core | yes |
| `orbit/v1/nodes/{node_id}/view` | Core | OLED/Web/Android Node | yes |
| `orbit/v1/nodes/{node_id}/intents` | Web Node | Core | no |
| `orbit/v1/agents/{agent_id}/commands` | Core | Agent | no |
| `orbit/v1/agents/{agent_id}/results` | Agent | Core | no |
| `orbit/v1/agents/{agent_id}/shortcuts` | Agent | Core | yes |
| `orbit/v1/nodes/{node_id}/navigation` | Core | Node | yes |

Core rejects messages when the participant ID in the topic does not match the
protobuf payload. Nodes subscribe only to their own view and never receive the
upstream observation data or Sub2API credentials. Navigation snapshots identify
the configured Agent, but never include its local application paths or URL targets.

Shortcut definitions and local execution permission belong to their execution endpoint.
An Agent publishes a complete `AgentShortcuts` catalog (up to 24 entries, 256 KiB total)
with titles and optional PNG icons (32 KiB / 256 pixels each). Local targets remain local.
An execution-capable App Node publishes equivalent metadata through its authenticated HTTP API.
The legacy MQTT Android display client does not advertise execution capabilities.

Core owns independent per-receiver shortcut pairings, ordered across multiple Agents or
execution-capable Nodes. A pairing follows the full catalog or an explicitly ordered selection.
Data projection inputs do not create, modify or remove pairings. Core also owns per-device
title visibility, navigation position and NFCD padding. On first migration, the previous
navigation snapshot and explicit device padding are frozen into these settings; later Agent
catalog changes cannot override device appearance.

Core publishes a complete retained `Navigation` with a durable revision. Items identify
`executor_kind`, `executor_id`, `executor_label` and `shortcut_id`; the existing Agent fields
remain for compatibility. Removing a pair or catalog entry removes its projected entries.
App Nodes obtain the same snapshot through authenticated HTTP and the existing SSE refresh flow.

A click supplies only the displayed entry ID. Core rechecks the current pairing and catalog.
For an Agent it publishes a non-retained `OpenShortcut` with a 15-second expiry and exact
catalog revision. For an App Node it queues a short-lived HTTP command, claims it once and
rechecks pairing, receiver authorization and catalog revision at delivery. The foreground
Node checks its local permission, revision and expiry, persists a bounded deduplication receipt,
and resolves the target locally. Commands are not replayed after Core restart. The 202 response
means sent, not completed. Paths, URLs and shell commands are never accepted from the clicking Node.

The Codex observation is a non-retained `orbit.v1.Observation` with the Codex
oneof member. Its metadata carries the Agent epoch, an observation-type-local
revision, production time, and expiry. The payload contains only bounded
session ID/model/status/time/process-liveness fields plus total and running
counts. By default `display_name` and `project_name` are empty, so no title or
first-user-message fallback appears. There is no separate raw title or prompt
field. When its explicit privacy flag is enabled, `display_name` may carry a
Codex title or first-user-message fallback and must be treated as sensitive
opt-in data. Source JSON, full cwd, rollout paths, and PIDs are never part of
the wire contract. The Agent emits
one initial retained AgentState with every enabled source marked
`UNSPECIFIED`, then polls each source independently and publishes its own
health updates.

Core subscribes to and validates Codex observations, then projects the
sanitized fields into the retained Web DeviceView. The OLED usage route does
not consume or render Codex observations in this milestone.

The Web Node publishes a typed `OpenCodexSessionIntent` only for a session in
its fresh cached view. Core resolves the target Agent from its private
projection route and publishes a typed `OpenCodexSession` command. The Agent
validates the target, expiry, requester reference, and lowercase session UUID,
deduplicates by command ID in bounded process memory, opens the local Codex URI,
and publishes a final result. Intent, Command, and CommandResult are never
retained and contain no arbitrary URL or shell command string.

Android Nodes identify as `display/android/flutter` and use the `overview-android`
profile, with the same UsageView and CodexView fields as `overview-web`. They
use MQTT 3.1.1 QoS 1 (without the MQTT 5 Content Type property) and publish
their own Node State; this client does not emit intents. Android speech capture
adds an independent JSON audio connection under its own node-owned audio topics.
The unified App shares this native capture service and its Core-provisioned identity.

Audio segments, queued jobs, acknowledgements, ASR results, and worker state use
the dedicated JSON topics in [audio-pipeline.md](audio-pipeline.md). These messages
use QoS 1 and are non-retained except worker state. Android retains its local
outbox until a Core application acknowledgement; Core persists pending jobs until
a result is saved; workers persist their results until Core acknowledges them.
This recovery does not depend on MQTT session persistence.

The `usage-oled-128x32` profile contains only the three bounded DisplaySlot
fields. The `overview-web` profile may additionally contain sanitized CodexView
fields. Both profiles use the same node-owned topics and ACL shape; consumers
must not grant an OLED or node credential access to Agent observation topics.

TLS is enabled by default and never falls back to plaintext. Production broker
ACLs must restrict each identity to the rows above that it owns. Public broker
TLS, credentials, ACL rejection, Last Will, and reconnect behavior still require
deployment-level acceptance; the local memory-bus test does not prove them.

The broader presence contract remains specified by
[design.md](design.md#7-mqtt-设计) and is outside the current runnable scope.
