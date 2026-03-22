> **⚠️ Out of Date:** This repository is currently out of date. Primary development focus is on the **Rust** and **Python** implementations. The author will get back to updating this, but if you need it sooner, please [open an issue](https://github.com/angzarr-io/angzarr/issues) or contact the author directly.

# angzarr-examples-go

Example implementations demonstrating Angzarr event sourcing patterns in Go.

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

```bash
# Deploy to Kind cluster
skaffold run
```

## License

BSD-3-Clause
