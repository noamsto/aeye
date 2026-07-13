#!/usr/bin/env bash
# Append images Claude touches (Read/Write/screenshots) to a per-pane manifest.
# PostToolUse hook: reads the hook JSON payload on stdin.
# Mirrors claude-status-update.sh — self-contained, keyed by $TMUX_PANE or $CLAUDE_CODE_SESSION_ID.
set -euo pipefail

# shellcheck source=lib/manifest-extract.sh disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/lib/manifest-extract.sh"
# shellcheck source=../../../core/manifest-lifecycle.sh disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/../../../core/manifest-lifecycle.sh"

resolve_state_dirs

# Key by tmux pane when inside tmux, else the Claude Code session id so the
# carousel works in a bare terminal. No pane and no session id → no-op.
pane_id="${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}"
[[ -n $pane_id ]] || exit 0
pane_file="${pane_id#%}"
# Guard against path traversal: the key becomes a filename; reject anything
# with path separators or outside a safe set (panes are %<int>, sessions are ids).
valid_pane_file "$pane_file" || exit 0

payload="$(cat)"
[[ -n $payload ]] || exit 0

source_tool="$(jq -r '.tool_name // "?"' <<<"$payload" 2>/dev/null)"
path="$(extract_image_path "$payload")"
[[ -n $path ]] || exit 0

printf -v now '%(%FT%T%z)T' -1

manifest_paths "$pane_file"
# shellcheck disable=SC2153 # MANIFEST comes from core's manifest_paths
manifest="$MANIFEST"
mkdir -p "$IMAGES_DIR"

# Serialize the owner self-heal + append against a concurrent diagrams.sh
# rewrite or backfill rebuild of the same manifest.
_manifest_lock "$LOCK_FILE"

# Self-heal against tmux pane-id reuse: a manifest last written by a different
# Claude session belongs to a pane that's since been recycled — drop it so this
# session's carousel never blends in a prior session's images. (The SessionStart
# reset already covers fresh starts; this also guards a start the reset missed.)
owner_selfheal "$pane_file" "${CLAUDE_CODE_SESSION_ID:-}"

# Append-only: no write-side dedup. Concurrent firings can emit duplicate
# (path,mtime) lines; the viewer collapses them on read (parseManifest).
append_image_line "$manifest" "$path" "$source_tool" "$now"
