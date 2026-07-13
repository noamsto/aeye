#!/usr/bin/env bash
# Append images Codex touches (apply_patch/view_image/Bash) to a per-pane
# manifest. PostToolUse hook: reads the hook JSON payload on stdin.
# Mirrors the Claude adapter's images.sh — self-contained, keyed by $TMUX_PANE
# or the Codex session id.
set -euo pipefail

PLUGIN_ROOT="${PLUGIN_ROOT:-$(dirname "${BASH_SOURCE[0]}")/..}"
# shellcheck source=lib/shim.sh disable=SC1091
source "$PLUGIN_ROOT/scripts/lib/shim.sh"
# shellcheck source=core/manifest-lifecycle.sh disable=SC1091
source "$PLUGIN_ROOT/scripts/core/manifest-lifecycle.sh"

resolve_state_dirs

payload="$(cat)"
[[ -n $payload ]] || exit 0

session="$(codex_session_id "$payload")"

# Key by tmux pane when inside tmux, else the Codex session id so the carousel
# works in a bare terminal. No pane and no session id → no-op.
pane_id="${TMUX_PANE:-$session}"
[[ -n $pane_id ]] || exit 0
pane_file="${pane_id#%}"
# Guard against path traversal: the key becomes a filename; reject anything
# with path separators or outside a safe set (panes are %<int>, sessions are ids).
valid_pane_file "$pane_file" || exit 0

source_tool="$(jq -r '.tool_name // "?"' <<<"$payload" 2>/dev/null)"

# codex_extract_touched_paths returns both images and .d2 paths in one call;
# diagrams.sh owns .d2 (it renders + records the png), so skip those here.
paths=()
while IFS= read -r p; do
	[[ -n $p ]] || continue
	[[ ${p,,} == *.d2 ]] && continue
	paths+=("$p")
done < <(codex_extract_touched_paths "$payload")
[[ ${#paths[@]} -gt 0 ]] || exit 0

printf -v now '%(%FT%T%z)T' -1

manifest_paths "$pane_file"
# shellcheck disable=SC2153 # MANIFEST comes from core's manifest_paths
manifest="$MANIFEST"
mkdir -p "$IMAGES_DIR"

# Serialize the owner self-heal + append against a concurrent diagrams.sh
# rewrite or backfill rebuild of the same manifest.
_manifest_lock "$LOCK_FILE"

# Self-heal against tmux pane-id reuse: a manifest last written by a different
# Codex session belongs to a pane that's since been recycled — drop it so this
# session's carousel never blends in a prior session's images. (The SessionStart
# reset already covers fresh starts; this also guards a start the reset missed.)
owner_selfheal "$pane_file" "$session"

# Append-only: no write-side dedup. Concurrent firings can emit duplicate
# (path,mtime) lines; the viewer collapses them on read (parseManifest).
for path in "${paths[@]}"; do
	append_image_line "$manifest" "$path" "$source_tool" "$now"
done
