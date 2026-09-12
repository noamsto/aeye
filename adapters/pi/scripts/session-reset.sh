#!/usr/bin/env bash
# session_start for the aeye pi extension. Keeps the carousel from showing a
# different session's images when a tmux pane id is reused (tmux renumbers panes
# from low values on every server restart, and the manifest dir is shared
# machine-wide). Two jobs:
#   1. This pane's manifest — clear a manifest that belongs to a different (or
#      unknown) session and stamp ownership, before the viewer reads. A
#      same-session continuation (pi -c, /reload) keeps its manifest, so a
#      resumed run does not blank the carousel. On resume/fork the extension's
#      backfill script is the sole writer and rebuilds the manifest from the
#      session history, so nothing is done here.
#   2. GC — sweep manifests (and their orphaned owner sidecars) for tmux panes no
#      longer in the server, and session-keyed files past a TTL, so the shared
#      dir never grows without bound. Reads the hook JSON on stdin.
set -euo pipefail

PLUGIN_ROOT="${PLUGIN_ROOT:-$(dirname "${BASH_SOURCE[0]}")/..}"
# shellcheck source=lib/shim.sh disable=SC1091
source "$PLUGIN_ROOT/scripts/lib/shim.sh"
# shellcheck source=core/manifest-lifecycle.sh disable=SC1091
source "$PLUGIN_ROOT/scripts/core/manifest-lifecycle.sh"

payload="$(cat)"
[[ -n $payload ]] || exit 0
source="$(jq -r '.source // empty' <<<"$payload" 2>/dev/null)"

resolve_state_dirs
[[ -d $IMAGES_DIR ]] || exit 0

# Same keying as images.sh/diagrams.sh so we act on the right manifest.
session="$(pi_session_id "$payload")"
pane_file="$(resolve_pane_key "$session")"

clear_pane() { rm -f "$IMAGES_DIR/$1.jsonl" "$IMAGES_DIR/$1.owner" "$IMAGES_DIR/$1.ownerpid" "$IMAGES_DIR/$1.lock"; }

# --- This pane's manifest ---
if [[ -n $pane_file ]] && valid_pane_file "$pane_file"; then
	# Serialize the clear/owner-stamp against a live images.sh append that may
	# fire the instant the session starts.
	_manifest_lock "$IMAGES_DIR/$pane_file.lock"
	owner_file="$IMAGES_DIR/$pane_file.owner"
	owner=""
	[[ -f $owner_file ]] && owner="$(<"$owner_file")"
	# A nested agent — one started from inside this session (a headless run in a
	# hook, script or tool call) — inherits $TMUX_PANE and so resolves this same
	# manifest key, then reports its own fresh start. Clearing there wipes the
	# carousel of the session that spawned it (#234). Skip while the recorded
	# owner is a different, still-live session; a dead or unrecorded owner is the
	# exited agent this clear is actually for.
	if owner_live "$pane_file" "$session"; then
		:
	elif [[ $source == resume || $source == fork ]]; then
		# The backfill script is the sole writer of a resumed/forked pane's
		# manifest (it rebuilds it authoritatively from the session history).
		# Touching the manifest or owner here would just race it.
		:
	elif [[ -z $owner || $owner != "$session" ]]; then
		# A fresh start, a reused pane id, or a manifest with no ownership: drop
		# it so this session never blends in a prior one's images, then claim it.
		clear_pane "$pane_file"
		owner_claim "$pane_file" "$session"
	else
		# Same-session continuation (pi -c, /reload): keep the captured images,
		# just refresh ownership.
		owner_claim "$pane_file" "$session"
	fi
fi

# --- GC the shared dir ---
# In tmux a manifest for a pane id absent from the server is dead. Outside tmux,
# session-keyed manifests have no liveness signal, so age them out instead.
live=""
if [[ -n ${TMUX:-} ]] && command -v tmux >/dev/null 2>&1; then
	# A failed probe (server shutting down, unreachable socket) must leave live
	# empty, not abort the hook — pipefail would surface it as our exit status.
	live="$(tmux list-panes -a -F '%#{pane_id}' 2>/dev/null | tr -d '%')" || live=""
fi

gc_sweep "$pane_file" "$live"

# gc_sweep forces its own success; this exit is just the hook's overall status.
exit 0
