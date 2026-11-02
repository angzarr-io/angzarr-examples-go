> **⚠️ Out of Date:** This repository is currently out of date. Primary development focus is on the **Rust** and **Python** implementations. The author will get back to updating this, but if you need it sooner, please [open an issue](https://github.com/angzarr-io/angzarr/issues) or contact the author directly.

> **Build status (2026-10-28):** `main` has been red since the 2026-05-19 `angzarr-project` submodule bump, which the sources have not caught up with. The dev-container fix merged on 2026-10-28 (the `justfile` and `lefthook.yml` changes) fixes only the dev-container recursion and worktree mounts; it does not repair the build.

# angzarr-examples-go

Example implementations demonstrating Angzarr event sourcing patterns in Go.

> **Status: intentionally lagging.** These examples are temporarily descoped: the project grew too fast in scope, so work is focused on the Rust and Python clients. They are behind the current angzarr stack and may not build against it. They will be brought back up to date once Rust and Python are fully operational.

## Examples

- **player/**: Player aggregate (functional style)
- **table/**: Table aggregate (object-oriented style)
- **hand/**: Hand aggregate
- **pmg-hand-flow/**: Process manager coordinating hand workflow
- **prj-output/**: Projector for output events
- **prj-cloudevents/**: CloudEvents projector

## Prerequisites

- Go 1.21+
- Buf CLI for proto generation
- Kind (for Kubernetes deployment)

## Building

```bash
# Generate protos
buf generate

# Build all binaries
go build -o player/agg-player ./player/agg
go build -o table/agg-table ./table/agg
# ... etc
```

## Running

### Standalone Mode

```bash
# Run with standalone runtime
./player/agg-player --standalone
```

### Kubernetes Mode

Note: the Kubernetes deployment here uses angzarr core's distributed runtime, which is retired for now; it is preserved at the tag [`distributed-runtime-final`](https://github.com/angzarr-io/angzarr/tree/distributed-runtime-final) and will return.

```bash
# Deploy to Kind cluster
skaffold run
```

## License

BSD-3-Clause

