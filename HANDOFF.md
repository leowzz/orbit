# Orbit Project Handoff

## 1. Project Positioning

Orbit is a personal device and capability network.

It connects host computers, cloud messaging, and multiple physical or software
nodes. A host can publish observed state and execute explicitly supported
commands. OLED, TFT, web, and future nodes can display state, emit intents, and
receive command results.

Orbit is not limited to status display. Expected future capabilities include:

- Sub2API usage and other account metrics.
- Codex task/session status.
- Opening a selected Codex session on a host.
- Host health, notifications, reminders, and personal automations.
- Input from buttons, touchscreens, web clients, or other trusted devices.

## 2. Confirmed Decisions

- Project name: `Orbit`.
- Repository location: `/Users/leo/work/orbit`.
- Backend and host-side implementation language: Go.
- Structured data protocol: Protocol Buffers.
- Cloud message transport: MQTT.
- Host-side process is an agent, not a collector. It both observes local state
  and executes commands.
- Initial consumers include small OLED displays and TFT displays.
- The protocol carries semantic state and commands, not pre-rendered pixels.

## 3. System Shape

```text
                         +-----------------------+
                         |      Orbit Core       |
                         | projection / policy   |
                         +-----------+-----------+
                                     |
                              MQTT + Protobuf
                                     |
             +-----------------------+-----------------------+
             |                                               |
     +-------+--------+                              +-------+-------+
     |  Orbit Agent   |                              |  Orbit Nodes  |
     |   Mac / PC     |                              | OLED/TFT/Web  |
     +-------+--------+                              +-------+-------+
             |                                               |
     observations + commands                         view + intents
             |
     Codex / Sub2API / OS
```

The first implementation may combine Orbit Core and the MQTT-facing producer
logic in one Go process. Their message contracts must remain distinct so they
can be deployed separately later without changing device firmware.

## 4. Responsibilities

### Orbit Agent

Runs on a trusted host computer.

- Collects observations from registered sources.
- Publishes host state and source health.
- Subscribes to commands addressed to its host identity.
- Validates command type, arguments, target, expiry, and authorization context.
- Deduplicates commands before causing local side effects.
- Executes commands through registered capabilities.
- Publishes command acknowledgements and final results.
- Never exposes a generic remote shell capability.

Example sources:

- `codex`: tasks, attention requirements, completion, and failure state.
- `sub2api`: cost, token usage, and TPM.
- `system`: host online state and selected health metrics.

Example capabilities:

- `codex.session.open`
- `url.open`
- `automation.run`

### Orbit Core

- Consumes observations from agents and external producers.
- Maintains the latest canonical state.
- Applies expiry, priority, privacy, and device-specific projection rules.
- Publishes retained views for consumers.
- Routes commands without interpreting arbitrary command payloads.
- May persist history later; MQTT retained state is not the history store.

### Orbit Node

- Connects with a unique device identity and scoped credentials.
- Subscribes to its projected view.
- Renders locally for its display geometry.
- Marks expired data stale while retaining the last usable frame.
- Optionally publishes input intents from buttons or touch controls.
- Does not store Sub2API, Codex, or host login credentials.

## 5. Proposed Repository Layout

```text
orbit/
├── cmd/
│   ├── orbit-agent/          # Host agent executable
│   └── orbit-core/           # Cloud/core executable
├── internal/
│   ├── agent/                # Scheduling, command lifecycle, deduplication
│   ├── core/                 # State projection and routing
│   ├── mqtt/                 # MQTT adapters
│   ├── sources/
│   │   ├── codex/
│   │   ├── sub2api/
│   │   └── system/
│   └── capabilities/
│       ├── codex/
│       └── automation/
├── proto/orbit/v1/
│   ├── common.proto
│   ├── observation.proto
│   ├── command.proto
│   ├── result.proto
│   └── view.proto
├── gen/go/                   # Generated Go Protobuf code
├── nodes/
│   ├── oled/                 # ESP32 OLED firmware
│   └── tft/                  # ESP32 TFT firmware
├── configs/
│   └── config.example.yaml
├── docs/
│   ├── architecture.md
│   ├── mqtt-topics.md
│   └── security.md
├── Makefile
├── buf.yaml
├── buf.gen.yaml
└── go.mod
```

Generated code should never be edited manually. Use Buf to lint Protocol
Buffers and generate Go plus the C/C++ representation selected for firmware.

## 6. MQTT Topic Contract

Proposed root namespace:

```text
orbit/v1/hosts/{host_id}/observations
orbit/v1/hosts/{host_id}/state
orbit/v1/hosts/{host_id}/commands
orbit/v1/hosts/{host_id}/results
orbit/v1/devices/{device_id}/view
orbit/v1/devices/{device_id}/intents
orbit/v1/clients/{client_id}/presence
```

Rules:

- Payload content type is `application/protobuf`.
- State, view, and presence messages use MQTT QoS 1 and retained delivery.
- Commands and results use QoS 1 and are not retained.
- Commands carry application-level expiry even when MQTT 5 Message Expiry is
  enabled.
- Consumers reject unsupported schema versions and oversized payloads.
- Topic version and Protobuf package version advance independently only when
  wire compatibility is preserved.

## 7. Protocol Model

All top-level messages should include a small common envelope rather than
duplicating transport-specific details throughout the domain payloads.

```proto
syntax = "proto3";

package orbit.v1;

import "google/protobuf/timestamp.proto";

message Metadata {
  string message_id = 1;
  string producer_id = 2;
  uint64 revision = 3;
  google.protobuf.Timestamp produced_at = 4;
  google.protobuf.Timestamp expires_at = 5;
}

message Command {
  Metadata metadata = 1;
  string command_id = 2;
  string target_host_id = 3;

  oneof action {
    OpenCodexSession open_codex_session = 10;
    OpenUrl open_url = 11;
    RunAutomation run_automation = 12;
  }
}

message OpenCodexSession {
  string session_id = 1;
}

message CommandResult {
  Metadata metadata = 1;
  string command_id = 2;
  CommandStatus status = 3;
  string error_code = 4;
  string safe_message = 5;
}

enum CommandStatus {
  COMMAND_STATUS_UNSPECIFIED = 0;
  COMMAND_STATUS_ACCEPTED = 1;
  COMMAND_STATUS_RUNNING = 2;
  COMMAND_STATUS_SUCCEEDED = 3;
  COMMAND_STATUS_FAILED = 4;
  COMMAND_STATUS_REJECTED = 5;
  COMMAND_STATUS_EXPIRED = 6;
}
```

Do not use `google.protobuf.Any` for commands. A typed `oneof` provides an
allowlist, makes firmware compatibility visible, and prevents arbitrary remote
execution from becoming part of the protocol by accident.

The exact observation and view schemas remain an implementation task. Keep raw
source observations separate from device views: sources describe facts, while
views contain the small semantic model selected for a consumer.

## 8. Command Delivery Semantics

MQTT QoS does not provide exactly-once execution of local side effects. The
agent must implement application-level semantics:

1. Decode and validate the Protobuf message.
2. Reject unknown action types, wrong target hosts, and expired commands.
3. Look up `command_id` in a durable deduplication store.
4. Persist acceptance before performing a side effect.
5. Execute the registered capability.
6. Persist and publish the final result.
7. Return the stored result for duplicate deliveries.

Commands initiated by interactive devices should normally expire within 30
seconds. An offline host must not open an old session hours after the original
button press.

## 9. Security Baseline

- MQTT over TLS only.
- Unique credentials and ACLs for every agent, core process, and device.
- Nodes may publish intents only within their own namespace.
- Only Orbit Core may publish device views or route approved commands.
- Agents subscribe only to commands for their own `host_id`.
- Capabilities are compiled or configured allowlists; no arbitrary command
  strings are accepted.
- Protobuf fields exposed to nodes must not contain prompts, response bodies,
  terminal output, tokens, credentials, or unrestricted local paths.
- Sensitive configuration remains outside the repository.
- Destructive or privacy-sensitive capabilities will require an explicit local
  confirmation policy before implementation.

## 10. Initial Delivery Plan

### Milestone 1: Protocol and Local Loop

- Initialize Go module and Buf configuration.
- Define metadata, observation, view, command, and result schemas.
- Add compatibility and size tests for representative firmware payloads.
- Run a local MQTT broker and round-trip one retained view.

### Milestone 2: Host Agent

- Implement MQTT lifecycle and presence.
- Add source and capability registries.
- Add durable command deduplication.
- Implement a synthetic source and harmless test capability first.

### Milestone 3: OLED Consumer

- Port the existing SSD1306 rendering and dirty-region refresh behavior.
- Replace direct Sub2API authentication and HTTP polling with an Orbit view
  subscriber.
- Verify stale-state and reconnect behavior on physical hardware.

### Milestone 4: Codex Integration

- Implement a version-isolated Codex source adapter.
- Implement `codex.session.open` as a typed capability.
- Publish only sanitized task summaries.
- Verify missing, archived, active, and attention-required session behavior.

### Milestone 5: TFT Consumer

- Add multi-page state display and touch/button intents.
- Show command acknowledgement, progress, success, and failure states.
- Keep display-specific navigation outside the shared protocol.

## 11. Decisions Still Required

- MQTT broker product and deployment location.
- Whether Orbit Core is required in the first executable version or initially
  embedded into the agent process.
- Firmware Protobuf implementation, likely nanopb or an equivalent bounded
  C/C++ generator.
- Durable command deduplication store for the Go agent.
- Device provisioning and credential rotation process.
- First TFT hardware target and display geometry.
- Exact privacy rules for Codex titles and repository names.

## 12. Definition of the First Useful Release

The first release is complete when:

- A Go Orbit Agent publishes a retained Protobuf view through MQTT.
- An ESP32 OLED node reconnects and immediately renders the latest view.
- A trusted test client publishes a typed command.
- The agent executes the command at most once and publishes a correlated result.
- Expired, duplicated, malformed, unauthorized, and unsupported commands are
  rejected deterministically.
- No Sub2API or Codex credential is present in device firmware.

