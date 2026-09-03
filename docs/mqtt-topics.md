# MQTT Topics

V1 payloads use `application/protobuf` and MQTT QoS 1. The first status-display
chain uses these topics:

| Topic | Publisher | Consumer | Retained |
| --- | --- | --- | --- |
| `orbit/v1/agents/{agent_id}/state` | Agent | Core | yes |
| `orbit/v1/agents/{agent_id}/observations/usage` | Agent | Core | no |
| `orbit/v1/agents/{agent_id}/observations/codex` | Agent | Core -> Web projection | no |
| `orbit/v1/nodes/{node_id}/state` | OLED/Web Node | Core | yes |
| `orbit/v1/nodes/{node_id}/view` | Core | OLED/Web Node | yes |
| `orbit/v1/nodes/{node_id}/intents` | Web Node | Core | no |
| `orbit/v1/agents/{agent_id}/commands` | Core | Agent | no |
| `orbit/v1/agents/{agent_id}/results` | Agent | Core | no |

Core rejects messages when the participant ID in the topic does not match the
protobuf payload. Nodes subscribe only to their own view and never receive the
upstream `agent_id` or Sub2API credentials.

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
