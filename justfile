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
        docker run --rm --network=host \
            -v "{{ROOT}}:/workspace:Z" \
            -v "{{ROOT}}/justfile.container:/workspace/justfile:ro" \
            -w /workspace \
            {{IMAGE}} just {{ARGS}}
    fi

# Run a mutation-testing target with the workspace mounted READ-ONLY.
#
# WHY:
#   The mutation suite (ooze) symlinks source files into a tmpdir then
#   os.WriteFile-overwrites the symlink path (so the rewrite lands in the
#   tmpdir, not the original file). In the current ooze version this leaves
#   the host tree alone — BUT (a) custom viruses or a future ooze release
#   could rewrite source in place, and (b) `proto-gen`/`go mod tidy`
#   invoked from inside `mutation-test` mutate go.mod/go.sum and write
#   *.pb.go files. Either way the mount is RW today and a crashed run
#   would leak. This helper closes both holes: source is mounted at
#   /src:ro, copied into /work inside the container's WRITABLE OVERLAY
#   LAYER, and `--rm` destroys the overlay (and every byte ooze touched)
#   on exit.
#
# WHAT TOUCHES THE HOST:
#   - {{ROOT}}/.mutants-cache/{go-pkg,go-build} — Go module/build caches
#     only. NEVER contains mutated source files. Gitignored. Delete the
#     dir to purge the cache.
#   - {{ROOT}}/mutation-results/mutation-test.log — tee'd combined output.
#
# WHAT NEVER TOUCHES THE HOST:
#   - Mutated source trees (live in /work, container overlay, --rm wipes).
#   - ooze's tmpdir symlink farms and overwritten files (live in $TMPDIR
#     inside the container).
#   - go mod tidy / buf generate edits to go.mod / *.pb.go (live in /work).
[private]
_container-ephemeral +ARGS: _build-image
    #!/usr/bin/env bash
    set -euo pipefail
    if [ "${DEVCONTAINER:-}" = "true" ]; then
        # Already inside a devcontainer — that container IS the ephemeral
        # boundary. Run directly; the outer just wrapper ensures --rm.
        just {{ARGS}}
        exit 0
    fi
    mkdir -p "{{ROOT}}/mutation-results" \
             "{{ROOT}}/.mutants-cache/go-pkg" \
             "{{ROOT}}/.mutants-cache/go-build"
    docker run --rm --network=host \
        -v "{{ROOT}}:/src:ro,Z" \
        -v "{{ROOT}}/mutation-results:/out:Z" \
        -v "{{ROOT}}/.mutants-cache/go-pkg:/go-pkg:Z" \
        -v "{{ROOT}}/.mutants-cache/go-build:/go-build:Z" \
        -v "{{ROOT}}/justfile.container:/etc/angzarr-justfile:ro" \
        -e GOPATH=/go-pkg \
        -e GOCACHE=/go-build \
        -e GOMODCACHE=/go-pkg/mod \
        -e MUTANTS_EPHEMERAL=1 \
        -w /work \
        {{IMAGE}} bash -eu -o pipefail -c '
            echo "[ephemeral] copying /src -> /work (container overlay)"
            mkdir -p /work
            # tar|tar: excludes mirror what we never want in the working
            # copy — vendored deps, build output, the mutants cache
            # itself, prior mutation output, and editor crud. The
            # angzarr-client-go submodule IS copied (required by the
            # `replace` directive in go.mod).
            tar -C /src \
                --exclude=./vendor \
                --exclude=./.mutants-cache \
                --exclude=./mutation-results \
                --exclude=./deploy \
                -cf - . \
                | tar -C /work -xf -
            # Mount the container-side justfile into the copy so `just`
            # finds it (the original /src is read-only, but /work is
            # writable).
            cp /etc/angzarr-justfile /work/justfile
            cd /work
            just {{ARGS}} 2>&1 | tee /out/mutation-test.log
        '

default:
    @just --list

# =============================================================================
# Proto generation — cross-language model (project_proto_generation_model)
# =============================================================================
# `.proto` sources live in the angzarr-project submodule. Bindings are NEVER
# committed (see .gitignore: proto/**/*pb.go; submodule angzarr-client-go has
# the same rule). They are regenerated:
#   1. on `post-checkout` / `post-merge` via lefthook (covers fresh clones,
#      branch switches, submodule bumps)
#   2. transparently as a recipe dependency of build/test/lint/check
# Idempotent: mtime guard short-circuits when bindings are newer than the
# newest .proto source.
#
# Runs in the same devcontainer image as build/test/mutation so the
# buf + protoc-gen-go toolchain is fixed. Rootless docker requires `-u 0:0`
# per feedback_docker_rootless.
#
# Tool integration (no `//go:generate` for the pre-build trigger): regen
# orchestration stays in `just` so the same invocation pattern works across
# all 6 langs. Plain `go build` consumes the pre-emitted *.pb.go files.
#
# Output path note: buf.gen.yaml emits to `angzarr-client-go/proto/` (the
# embedded client-go submodule worktree). The submodule's own .gitignore
# already excludes `proto/**/*pb.go`, so bindings are never tracked from
# either repo.

PROTO_SRC_DIR := ROOT + "/angzarr-project/proto"
PROTO_OUT_DIR := ROOT + "/angzarr-client-go/proto"

# Public entry point. Idempotent.
generate-proto:
    #!/usr/bin/env bash
    set -euo pipefail
    src_dir="{{PROTO_SRC_DIR}}"
    out_dir="{{PROTO_OUT_DIR}}"
    if [ ! -d "$src_dir" ]; then
        echo "[generate-proto] $src_dir missing — initialize angzarr-project submodule" >&2
        exit 1
    fi
    # Glob `*.pb.go` covers both protocolbuffers/go and grpc/go output.
    #
    # NEWEST (not OLDEST as in Python/Rust): the Go tree has stale orphan
    # .pb.go files from old buf configs (the two-path leaf-subpkg dodge —
    # see prompt). Using OLDEST would never converge here. Cleanup is a
    # queued migration; out of scope. Semantic effect of NEWEST: a real
    # submodule bump (new proto mtimes) still triggers regen. Manual single-
    # file deletion still requires `generate-proto-force`.
    newest_proto=$(find "$src_dir" -name '*.proto' -printf '%T@\n' 2>/dev/null \
                    | sort -n | tail -1)
    newest_pb=$(find "$out_dir" -name '*.pb.go' -printf '%T@\n' 2>/dev/null \
                    | sort -n | tail -1)
    if [ -n "$newest_proto" ] && [ -n "$newest_pb" ] \
        && awk -v p="$newest_proto" -v b="$newest_pb" 'BEGIN{exit !(b>p)}'; then
        echo "[generate-proto] bindings up-to-date, skipping (use generate-proto-force to override)"
        exit 0
    fi
    just generate-proto-force

# Force regeneration. Uses the devcontainer image directly because
# feedback_docker_rootless mandates `-u 0:0` for rootless writes to bind
# mounts; _container's default UID/GID only works rootful.
generate-proto-force: _build-image
    #!/usr/bin/env bash
    set -euo pipefail
    if [ "${DEVCONTAINER:-}" = "true" ]; then
        just --justfile "{{ROOT}}/justfile.container" generate-proto-force
        exit 0
    fi
    # Detect rootless vs rootful per feedback_docker_rootless.
    if docker info --format '{{{{.SecurityOptions}}}}' 2>/dev/null | grep -q rootless; then
        USER_FLAG="-u 0:0"
    else
        USER_FLAG="-u $(id -u):$(id -g)"
    fi
    docker run --rm --network=host \
        $USER_FLAG \
        -v "{{ROOT}}:/workspace:Z" \
        -v "{{ROOT}}/justfile.container:/workspace/justfile:ro" \
        -w /workspace \
        -e DEVCONTAINER=true \
        {{IMAGE}} just generate-proto-force

# Legacy aliases — kept so existing recipe-deps and muscle memory keep working.
proto-gen: generate-proto
proto: generate-proto

build: generate-proto
    just _container build

test-unit: generate-proto
    just _container test-unit

test-acceptance: generate-proto
    just _container test-acceptance

# Run mutation tests in an ephemeral container (source mounted read-only).
# All mutations live in the container overlay and die with `--rm`.
# Host go-test/ooze invocations are FORBIDDEN — see _container-ephemeral.
mutation-test: generate-proto
    just _container-ephemeral mutation-test

# Mutation image: re-uses the example devcontainer build (the
# .devcontainer/Containerfile now installs `gremlins` v0.5.0 per
# .plan/mutation-container-isolation.md), so we just alias to _build-image.
[private]
_ensure-image: _build-image

# Cross-language `mutate` recipe per .plan/mutation-container-isolation.md.
mutate: _ensure-image generate-proto
    mkdir -p "{{ROOT}}/mutants-reports"
    docker run --rm --network=host \
        -u 0:0 \
        --mount type=bind,src="{{ROOT}}",dst=/src,readonly \
        --tmpfs /work:rw,exec,size=4g \
        --mount type=bind,src="{{ROOT}}/mutants-reports",dst=/reports \
        -w /work \
        {{IMAGE}} \
        bash -eu -o pipefail -c '\
            if command -v rsync >/dev/null 2>&1; then \
                rsync -a /src/ /work/; \
            else \
                tar -C /src -cf - . | tar -C /work -xf -; \
            fi && \
            cd /work && \
            (gremlins unleash . --output mutants.json 2>&1 || true) && \
            (cp -f /work/mutants.json /reports/ 2>/dev/null || true) && \
            echo "[mutate] mutants.json copied to host mutants-reports/" \
        '

mutants: mutate

# Purge the local mutation build cache (.mutants-cache/ — Go caches only).
mutants-purge-cache:
    rm -rf "{{ROOT}}/.mutants-cache"
    @echo "Removed {{ROOT}}/.mutants-cache"

test: test-unit test-acceptance

fmt: generate-proto
    just _container fmt

lint: generate-proto
    just _container lint

# Cross-language alias — `just check` runs lint + fmt-check.
check: lint fmt

# Run poker in standalone mode (host - needs Rust)
run: build
    mkdir -p "{{ROOT}}/data"
    cd "{{ROOT}}" && cargo run \
        --bin angzarr-standalone \
        --features standalone,sqlite \
        -- --config standalone.yaml

clean:
    rm -rf "{{ROOT}}/data"

# Auto-format code
fmt-fix: generate-proto
    just _container fmt-fix
