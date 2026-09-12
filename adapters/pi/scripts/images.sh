#!/usr/bin/env bash
# Append images this pi session touches (read/write/edit/bash screenshots) to a
# per-pane manifest. Runs from the aeye pi extension's tool_result handler,
# which feeds a normalized hook JSON payload on stdin. Self-contained, keyed per
# tmux server+pane or the pi session id.
set -euo pipefail

PLUGIN_ROOT="${PLUGIN_ROOT:-$(dirname "${BASH_SOURCE[0]}")/..}"
# shellcheck source=lib/shim.sh disable=SC1091
source "$PLUGIN_ROOT/scripts/lib/shim.sh"
# shellcheck source=core/manifest-lifecycle.sh disable=SC1091
source "$PLUGIN_ROOT/scripts/core/manifest-lifecycle.sh"

resolve_state_dirs

payload="$(cat)"
[[ -n $payload ]] || exit 0

# Key by tmux pane when inside tmux, else the pi session id so the carousel works
# in a bare terminal. No pane and no session id -> no-op.
session="$(pi_session_id "$payload")"
pane_file="$(resolve_pane_key "$session")"
[[ -n $pane_file ]] || exit 0
# Guard against path traversal: the key becomes a filename; reject anything with
# path separators or outside a safe set (panes are %<int>, sessions are ids).
valid_pane_file "$pane_file" || exit 0

source_tool="$(jq -r '.tool_name // "?"' <<<"$payload" 2>/dev/null)"
path="$(extract_image_path "$payload")"
[[ -n $path ]] || exit 0
# diagrams.sh already owns these dual-theme renders. A read used to inspect one
# must not add that fixed theme as a second, plain carousel image.
is_d2_render_artifact "$path" "$DIAGRAMS_DIR" && exit 0

printf -v now '%(%FT%T%z)T' -1

manifest_paths "$pane_file"
# shellcheck disable=SC2153 # MANIFEST comes from core's manifest_paths
manifest="$MANIFEST"
mkdir -p "$IMAGES_DIR"

# Serialize the owner self-heal + append against a concurrent diagrams.sh
# rewrite or backfill rebuild of the same manifest.
_manifest_lock "$LOCK_FILE"

# Self-heal against tmux pane-id reuse: a manifest last written by a different
# session belongs to a pane that's since been recycled — drop it so this
# session's carousel never blends in a prior session's images. (The session_start
# reset already covers fresh starts; this also guards a start the reset missed.)
owner_selfheal "$pane_file" "$session"

# Append-only: no write-side dedup. Concurrent firings can emit duplicate
# (path,mtime) lines; the viewer collapses them on read (parseManifest).
append_image_line "$manifest" "$path" "$source_tool" "$now"
