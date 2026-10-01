# Go examples
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
#   - Commands execute directly via `just <target>`
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
    docker build --network=host -t {{IMAGE}} -f "{{ROOT}}/.devcontainer/Containerfile" "{{ROOT}}/.devcontainer"

# Run just target in container (or directly if already in devcontainer)
[private]
_container +ARGS: _build-image
    #!/usr/bin/env bash
    if [ "${DEVCONTAINER:-}" = "true" ]; then
        just {{ARGS}}
    else
        # Mount the shared git dir at its host path so linked worktrees
        # (whose .git file points there) resolve inside the container.
        git_common="$(git rev-parse --path-format=absolute --git-common-dir)"
        docker run --rm --network=host \
            -v "{{ROOT}}:/workspace:Z" \
            -v "${git_common}:${git_common}" \
            -v "{{ROOT}}/justfile.container:/workspace/justfile:ro" \
            -w /workspace \
            -e GIT_CONFIG_COUNT=1 \
            -e GIT_CONFIG_KEY_0=safe.directory \
            -e GIT_CONFIG_VALUE_0='*' \
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

clean:
    rm -rf "{{ROOT}}/data"
