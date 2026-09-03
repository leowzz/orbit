# Orbit

Orbit connects trusted host capabilities to personal display and software nodes.
The repository is currently at the project-scaffold stage; the system contract
and implementation boundaries are documented in [docs/design.md](docs/design.md).

## Development

Requirements:

- Go 1.27 or newer
- Buf, when working on Protocol Buffers
- uv and PlatformIO, when working on display firmware

Common commands:

```shell
make dev          # run the host Agent entrypoint
make dev-core     # run the Core entrypoint
make test         # run all Go tests
make build        # compile all Go packages
make proto-lint   # lint Protocol Buffer sources
make generate     # generate protocol bindings
```

The two executables currently contain lifecycle placeholders only. Domain logic
belongs under `internal/`; `cmd/` is limited to process assembly.

## Layout

```text
cmd/                    Go executable entrypoints
internal/               Agent, Core, transport, source, and capability modules
proto/orbit/v1/         Versioned wire protocol sources
gen/go/                 Generated Go protocol bindings
nodes/display/          Shared display firmware and model-specific code
configs/                Non-sensitive example configuration
docs/                   Architecture, protocol, security, and ADR documentation
```

