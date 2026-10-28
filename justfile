# Go poker examples
#
# Container Overlay Pattern:
# --------------------------
# This justfile uses an overlay pattern for container execution:
#
# 1. `justfile` (this file) - runs on the host, delegates to container
# 2. `justfile.container` - mounted over this file inside the container
#
# When running outside a devcontainer:
#   - Builds/uses local devcontainer image with `just` pre-installed
#   - Podman mounts justfile.container as /workspace/justfile
#   - `just build` on host → docker runs → `just build` in container → `go build ./...`
#
# When running inside a devcontainer (DEVCONTAINER=true):
#   - Commands execute directly via `just --justfile justfile.container <target>`
#   - No container nesting

set shell := ["bash", "-c"]

# Reusable submodule-protection recipes (install-submodule-hooks,
# check-submodules-clean). Source of truth: angzarr-project/submodule.just.
import? 'angzarr-project/submodule.just'

ROOT := `git rev-parse --show-toplevel`
IMAGE := "angzarr-go-dev"

# Build the devcontainer image
[private]
_build-image:
    #!/usr/bin/env bash
    set -euo pipefail
    # Inside a devcontainer _container runs recipes directly, so no image is
    # needed (and there is no docker to build one with).
    if [ "${DEVCONTAINER:-}" = "true" ]; then
        exit 0
    fi
    docker build --network=host -t {{IMAGE}} -f "{{ROOT}}/.devcontainer/Containerfile" "{{ROOT}}/.devcontainer"

# Run just target in container (or directly if already in devcontainer)
[private]
_container +ARGS: _build-image
    #!/usr/bin/env bash
    if [ "${DEVCONTAINER:-}" = "true" ]; then
        # Name the container justfile explicitly. A bare `just {{ARGS}}`
        # resolves to THIS (host) justfile, whose recipes call `_container`
        # again: build -> _container build -> just build -> ... with no limit.
        just --justfile "{{ROOT}}/justfile.container" {{ARGS}}
    else
        # Mount the shared git dir at its host path so a linked worktree
        # (whose .git file points there) resolves inside the container;
        # safe.directory lets git read a checkout owned by another uid.
        git_common="$(git rev-parse --path-format=absolute --git-common-dir)"
        docker run --rm --network=host \
            -v "{{ROOT}}:/workspace:Z" \
            -v "${git_common}:${git_common}" \
            -e GIT_CONFIG_COUNT=1 \
            -e GIT_CONFIG_KEY_0=safe.directory \
            -e GIT_CONFIG_VALUE_0='*' \
            -v "{{ROOT}}/justfile.container:/workspace/justfile:ro" \
            -w /workspace \
            {{IMAGE}} just {{ARGS}}
    fi

default:
    @just --list

build:
    just _container build

test-unit:
    just _container test-unit

test-acceptance:
    just _container test-acceptance

test: test-unit test-acceptance

fmt:
    just _container fmt

lint:
    just _container lint

# Run poker in standalone mode (host - needs Rust)
run: build
    mkdir -p "{{ROOT}}/data"
    cd "{{ROOT}}" && cargo run \
        --bin angzarr-standalone \
        --features standalone,sqlite \
        -- --config standalone.yaml

clean:
    rm -rf "{{ROOT}}/data"
