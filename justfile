# aeye dev tasks. Run `just` for the list.
#
# The carousel renders images via the kitty graphics protocol. Inside tmux,
# aeye wraps graphics in tmux-passthrough escapes — which don't survive a
# kitty -> tmux -> nested-kitty layout (the inner kitty gets the wrapper raw).
# So the dev launch recipes spawn a *fresh, top-level* kitty window with TMUX
# unset, giving aeye a clean native-graphics surface.

bin := justfile_directory() / "aeye"
state_dir := env_var_or_default("AEYE_DIR", env_var_or_default("CLAUDE_STATUS_DIR", "/tmp/claude-status"))

# List recipes
default:
    @just --list

# Build the aeye viewer binary at repo root
build:
    go build -o {{bin}} .

# Launch the carousel in a fresh, clean kitty window. KEY defaults to the newest manifest.
carousel key="": build
    #!/usr/bin/env bash
    set -euo pipefail
    key="{{key}}"
    if [[ -z "$key" ]]; then
        manifest=$(ls -t "{{state_dir}}"/images/*.jsonl 2>/dev/null | head -1 || true)
        [[ -n "$manifest" ]] || { echo "no manifests under {{state_dir}}/images — run 'just seed' first" >&2; exit 1; }
        key=$(basename "$manifest" .jsonl)
    fi
    echo "carousel: key=$key  state={{state_dir}}"
    kitty bash -c "unset TMUX TMUX_PANE; export AEYE_DIR='{{state_dir}}'; exec '{{bin}}' '$key'" >/dev/null 2>&1 &
    disown

# Open a clean kitty window with a plain login shell — overrides kitty.conf's
# tmux-attaching shell and unsets TMUX, so no tmux auto-attach.
kitty:
    env -u TMUX -u TMUX_PANE kitty --detach fish -l

# Run the real toggle wrapper against the dev binary (shipped tmux/kitty path).
toggle: build
    AEYE_BIN='{{bin}}' AEYE_DIR='{{state_dir}}' {{justfile_directory()}}/scripts/tmux-claude-images.sh

# Seed a manifest with a rendered demo diagram (no live session needed). KEY names the manifest.
seed key="demo":
    #!/usr/bin/env bash
    set -euo pipefail
    imgdir="{{state_dir}}/images"
    srcdir="$imgdir/diagrams/src"
    mkdir -p "$srcdir"
    src="$srcdir/seed-demo.d2"
    svg="$imgdir/diagrams/seed-demo.svg"
    png="$imgdir/diagrams/seed-demo.png"
    cat > "$src" <<'D2'
    direction: right
    title: |md # aeye seed demo | { near: top-center }
    classes: {
      svc:   { style: { stroke: "#1565C0"; stroke-width: 2 } }
      store: { shape: cylinder; style: { stroke: "#2E7D32"; stroke-width: 2 } }
    }
    capture: Capture hook { class: svc }
    manifest: Manifest { class: store }
    viewer: aeye viewer { class: svc }
    capture -> manifest: append entry
    manifest -> viewer: render
    D2
    d2 "$src" "$svg" 2>/dev/null
    resvg "$svg" "$png" 2>/dev/null
    manifest="$imgdir/{{key}}.jsonl"
    ts=$(date -Iseconds)
    mt=$(stat -c %Y "$png")
    printf '{"type":"image","path":"%s","vector":"%s","source":"d2","ts":"%s","mtime":%s}\n' "$png" "$svg" "$ts" "$mt" > "$manifest"
    echo "seeded $manifest — view with: just carousel {{key}}"

# Go unit tests
test:
    go test ./...

# Bats integration tests (adapter, toggle, diagrams, hooks)
test-bats:
    bats --recursive tests/

# Re-vendor core/ into the Codex plugin (codex plugin add copies the plugin
# dir only, not its core/ sibling, so the plugin carries its own synced copy).
sync-codex-core:
    cp adapters/core/manifest-extract.sh adapters/core/manifest-lifecycle.sh adapters/codex/plugin/scripts/core/

# Re-vendor core/ into the pi adapter (pi packages are loaded from the adapter
# dir; the vendored copy keeps it working when only that dir is shipped).
sync-pi-core:
    cp adapters/core/manifest-extract.sh adapters/core/manifest-lifecycle.sh adapters/pi/scripts/core/

hookyard_src := env_var_or_default("HOOKYARD_SRC", "")

# Regenerate adapters/pi's hookyard-built plugin package (extensions/hookyard.ts,
# bin/hookyard*, hookyard/table.json, package.json's pi.extensions entry).
# Requires `hookyard` on PATH — at the commit adapters/pi/hookyard.json and the
# handler scripts were written against (noamsto/hookyard @ f7ada54, "consolidate
# pi's hook surface"); an older binary predating `build --engine pi`/`commands[]`
# fails with an opaque `--engine pi` error rather than a clear version mismatch.
# HOOKYARD_SRC must point at a hookyard source checkout at that same commit, used
# to cross-build both arches aeye ships (hookyard build only ever bundles the
# arch it ran on — see build.go — so the *other* arch needs an explicit build or
# it's a silent no-op on that platform, per bin/hookyard's fail-open dispatcher).
build-pi-plugin:
    #!/usr/bin/env bash
    set -euo pipefail
    [ -n "{{hookyard_src}}" ] || { echo "set HOOKYARD_SRC to a hookyard checkout" >&2; exit 1; }
    hookyard build --engine pi --manifest adapters/pi/hookyard.json --out adapters/pi --name aeye-pi
    (cd "{{hookyard_src}}" && GOOS=linux  GOARCH=amd64 go build -o "{{justfile_directory()}}/adapters/pi/bin/hookyard-linux-amd64"  ./cmd/hookyard)
    (cd "{{hookyard_src}}" && GOOS=darwin GOARCH=arm64 go build -o "{{justfile_directory()}}/adapters/pi/bin/hookyard-darwin-arm64" ./cmd/hookyard)

# Re-vendor core/ into the Cursor adapter (hooks.json install ships its own copy).
sync-cursor-core:
    cp adapters/core/manifest-extract.sh adapters/core/manifest-lifecycle.sh adapters/cursor/scripts/core/

# Format Go sources
fmt:
    gofmt -w .

# Remove the built binary
clean:
    rm -f {{bin}}
