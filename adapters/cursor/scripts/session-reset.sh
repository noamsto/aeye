#!/usr/bin/env bash
# sessionStart hook. Keeps the carousel from showing a different session's images
# when a tmux pane id is reused (tmux renumbers panes from low values on every
# server restart, and the manifest dir is shared machine-wide). Two jobs:
#   1. This pane's manifest — Cursor has no `source` field and sessionStart only
#      fires for new conversations, so always apply startup semantics: clear the
#      pane manifest and stamp the owner with cursor_session_id, before the
#      viewer reads, so a reader launched right after start sees a manifest that
#      belongs to this session.
#   2. GC — sweep manifests (and their orphaned owner sidecars) for tmux panes no
#      longer in the server, and session-keyed files past a TTL, so the shared dir
#      never grows without bound. Reads the hook JSON on stdin.
set -euo pipefail

PLUGIN_ROOT="${PLUGIN_ROOT:-$(dirname "${BASH_SOURCE[0]}")/..}"
# shellcheck source=lib/shim.sh disable=SC1091
source "$PLUGIN_ROOT/scripts/lib/shim.sh"
# shellcheck source=core/manifest-lifecycle.sh disable=SC1091
source "$PLUGIN_ROOT/scripts/core/manifest-lifecycle.sh"

payload="$(cat)"
[[ -n $payload ]] || exit 0

resolve_state_dirs
[[ -d $IMAGES_DIR ]] || exit 0

# Same keying as images.sh/diagrams.sh so we act on the right manifest.
session="$(cursor_session_id "$payload")"
pane_id="${TMUX_PANE:-$session}"
pane_file="${pane_id#%}"

clear_pane() { rm -f "$IMAGES_DIR/$1.jsonl" "$IMAGES_DIR/$1.owner" "$IMAGES_DIR/$1.lock"; }

# --- This pane's manifest ---
if [[ -n $pane_id ]] && valid_pane_file "$pane_file"; then
	# Serialize the clear/owner-stamp against a live images.sh append that may
	# fire the instant the session starts.
	_manifest_lock "$IMAGES_DIR/$pane_file.lock"
	owner_file="$IMAGES_DIR/$pane_file.owner"
	# Always startup: clear + stamp. No resume/compact branch (Cursor has no
	# source field; sessionStart is new-conversation-only).
	clear_pane "$pane_file"
	[[ -n $session ]] && printf '%s' "$session" >"$owner_file"
fi

# --- GC the shared dir ---
# In tmux a manifest for a pane id absent from the server is dead. Outside tmux,
# session-keyed manifests have no liveness signal, so age them out instead.
live=""
[[ -n ${TMUX:-} ]] && command -v tmux >/dev/null 2>&1 &&
	live="$(tmux list-panes -a -F '%#{pane_id}' 2>/dev/null | tr -d '%')"

gc_sweep "$pane_file" "$live"

# gc_sweep forces its own success; this exit is just the hook's overall status.
exit 0
