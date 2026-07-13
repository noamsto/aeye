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

# Format Go sources
fmt:
    gofmt -w .

# Remove the built binary
clean:
    rm -f {{bin}}
