# angzarr-examples-go

Example implementations demonstrating Angzarr event sourcing patterns in Go. See the [Angzarr documentation](https://angzarr.io/) for more information.

> The poker example has been retired. A blackjack example is coming; its spec lives in [angzarr-project](https://github.com/angzarr-io/angzarr-project) under `proto/io/angzarr/examples/v1` and `features/example/blackjack*`.

## Development

Install git hooks (requires [lefthook](https://github.com/evilmartians/lefthook)):

```bash
lefthook install
```

```bash
just -l              # List all available recipes
just build           # Build (runs in the devcontainer image)
just test            # Run tests
just lint            # Lint
just fmt             # Format
```

## License

BSD-3-Clause
