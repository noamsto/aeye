#!/usr/bin/env bash
# SessionStart hook. Keeps the carousel from showing a different session's images
# when a tmux pane id is reused (tmux renumbers panes from low values on every
# server restart, and the manifest dir is shared machine-wide). Two jobs:
#   1. This pane's manifest — clear it on a fresh start (startup|clear) and stamp
#      the owner, before the viewer reads, so a reader launched right after start
#      sees a manifest that belongs to this session. On compact (a same-session
#      continuation) clear only a manifest proven foreign, then refresh ownership.
#      On resume, do nothing: SessionStart hooks run in parallel, and the backfill
#      hook is the sole writer of a resumed pane's manifest — it rebuilds it from
#      the transcript, so racing it here would only reintroduce bleed.
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
source="$(jq -r '.source // empty' <<<"$payload" 2>/dev/null)"

resolve_state_dirs
[[ -d $IMAGES_DIR ]] || exit 0

# Same keying as images.sh/diagrams.sh so we act on the right manifest.
session="$(codex_session_id "$payload")"
pane_id="${TMUX_PANE:-$session}"
pane_file="${pane_id#%}"

clear_pane() { rm -f "$IMAGES_DIR/$1.jsonl" "$IMAGES_DIR/$1.owner" "$IMAGES_DIR/$1.lock"; }

# --- This pane's manifest ---
if [[ -n $pane_id ]] && valid_pane_file "$pane_file"; then
	# Serialize the clear/owner-stamp against a live images.sh append that may
	# fire the instant the session starts.
	_manifest_lock "$IMAGES_DIR/$pane_file.lock"
	owner_file="$IMAGES_DIR/$pane_file.owner"
	owner=""
	[[ -f $owner_file ]] && owner="$(<"$owner_file")"
	case "$source" in
	startup | clear)
		clear_pane "$pane_file"
		[[ -n $session ]] && printf '%s' "$session" >"$owner_file"
		;;
	resume)
		# SessionStart hooks run in parallel; session-backfill is the sole writer
		# of a resumed pane's manifest (it rebuilds it authoritatively from the
		# transcript). Touching the manifest or owner here would just race it.
		:
		;;
	*)
		# compact/unknown: a same-session continuation. Clear only a manifest
		# proven foreign (reused-pane-id bleed), then refresh ownership.
		[[ -n $session && -n $owner && $owner != "$session" ]] && clear_pane "$pane_file"
		[[ -n $session ]] && printf '%s' "$session" >"$owner_file"
		;;
	esac
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
