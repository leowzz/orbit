---
status: accepted
---

# Integrate Codex collection in the Agent

## Context

Codex session state is local host data. Orbit needs a bounded, read-only
summary without making Codex a separate MQTT participant or exposing prompts,
paths, process metadata, or raw storage details.

## Decision

- The Agent owns an optional Codex Source alongside the optional Sub2API Source.
  Each source polls immediately and on its own interval, with independent
  observation revision and health state. The Agent publishes one retained
  initial AgentState containing every enabled source before either source runs.
- Successful Codex snapshots use the non-retained
  `orbit/v1/agents/{agent_id}/observations/codex` topic and carry the Agent
  epoch, type-local revision, production time, and expiry.
- The wire summary is limited to session ID, model, normalized status, update
  time, process liveness, and total/running counts. Strings are bounded by
  UTF-8 byte limits. `include_display_name` and `include_project_name` default
  to false. `include_display_name` is an explicit opt-in and may expose the
  Codex title or first-user-message fallback used to form the display name;
  project name is likewise opt-in. There is no separate raw prompt/title field;
  by default neither title nor fallback appears, while the explicit display
  opt-in may carry user input through that fallback. Source JSON, full cwd,
  rollout path, and PID are permanently excluded from the protocol and logs.
- Existing consumers remain compatible because Codex uses a new observation
  type and oneof member; consuming or rendering it is a separate concern.

## Alternatives

Treating Codex as a standalone MQTT publisher would duplicate Agent identity,
health, and ACL handling. Publishing raw Codex records would be more flexible
but would violate the trusted-host privacy boundary. Both are rejected.

## Consequences

Codex-only and dual-source deployments work without Sub2API credentials.
Existing consumers ignore the new oneof member and topic until they explicitly
adopt them. Explicit privacy configuration remains a deployment decision and
must be reviewed before enabling it on a shared broker.
