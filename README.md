# Orbit

Orbit connects trusted host state to small display nodes. The first working
chain reads Sub2API usage in the host Agent, transports protobuf observations
over MQTT, projects them in Core, and publishes a retained three-slot view for
the OLED node.

```text
Sub2API -> orbit-agent -> MQTT -> orbit-core -> retained DeviceView -> OLED node
```

Sub2API credentials stay on the Agent. The node receives only formatted cost,
token, TPM, and freshness text.

## Requirements

- Go 1.27 or newer
- uv for node tooling
- A C++ toolchain supported by PlatformIO

The root Makefile installs pinned Buf and `protoc-gen-go` binaries under the
ignored `.tools/` directory when protocol targets are first used. The OLED node
has its own Makefile and pinned Python/PlatformIO environment.

## Configuration

Agent, Core, and each independently built node use separate YAML contracts:

- `configs/agent.example.yaml`: Agent identity, MQTT credentials, Sub2API
  endpoints/credential files, timezone, currency, polling, and TTL.
- `configs/core.example.yaml`: Core identity, MQTT credentials, observation
  limits, and explicit `node_id -> agent_id` projection routes.
- `nodes/display/models/oled-128x32/variants/yd-esp32-s3/config.example.yaml`:
  node identity, Wi-Fi, MQTT, TLS, and display settings.

Use ignored local files for deploy-time values:

```shell
cp configs/agent.example.yaml configs/agent.local.yaml
cp configs/core.example.yaml configs/core.local.yaml
cp nodes/display/models/oled-128x32/variants/yd-esp32-s3/config.example.yaml \
  nodes/display/models/oled-128x32/variants/yd-esp32-s3/config.local.yaml
```

The host examples reference separate secret files. Do not commit local YAML,
secret files, generated headers, or firmware build output.

## Development

```shell
make dev              # run Agent and Core with their local YAML files
make test-go          # unit tests plus the Sub2API-to-DeviceView chain test
make proto-lint       # install pinned Buf if needed, then lint schemas
make generate         # regenerate Go protobuf bindings
make test-node        # YAML tests and native firmware tests
make build-node       # generate nanopb bindings and build YD-ESP32-S3 firmware
make verify           # all static checks, tests, protocol lint, and builds
```

The node can also be operated independently:

```shell
make -C nodes/display/models/oled-128x32/variants/yd-esp32-s3 check
make -C nodes/display/models/oled-128x32/variants/yd-esp32-s3 build \
  CONFIG=config.local.yaml
```

Automated checks do not establish live Sub2API credentials, public broker
TLS/ACL behavior, Wi-Fi connectivity, or physical OLED appearance. Those are
separate deployment and hardware acceptance steps.

## Layout

```text
cmd/                    Agent and Core process assembly
internal/               config, source, transport, Agent, and Core logic
proto/orbit/v1/         versioned wire schemas
gen/go/                 generated Go protobuf bindings
nodes/display/          shared display firmware and model/variant delivery units
configs/                non-sensitive host configuration examples
docs/                   architecture, security, MQTT, and ADR documentation
```
