# MQTT Topics

V1 payloads use `application/protobuf` and MQTT QoS 1. The first status-display
chain uses these topics:

| Topic | Publisher | Consumer | Retained |
| --- | --- | --- | --- |
| `orbit/v1/agents/{agent_id}/state` | Agent | Core | yes |
| `orbit/v1/agents/{agent_id}/observations/usage` | Agent | Core | no |
| `orbit/v1/nodes/{node_id}/state` | Node | Core | yes |
| `orbit/v1/nodes/{node_id}/view` | Core | Node | yes |

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

TLS is enabled by default and never falls back to plaintext. Production broker
ACLs must restrict each identity to the rows above that it owns. Public broker
TLS, credentials, ACL rejection, Last Will, and reconnect behavior still require
deployment-level acceptance; the local memory-bus test does not prove them.

The broader presence, command, result, and intent contract remains specified by
[design.md](design.md#7-mqtt-设计) and is outside the V1 status-display scope.
