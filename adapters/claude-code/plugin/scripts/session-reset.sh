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

payload="$(cat)"
[[ -n $payload ]] || exit 0
source="$(jq -r '.source // empty' <<<"$payload" 2>/dev/null)"

STATE_DIR="${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
IMAGES_DIR="$STATE_DIR/images"
[[ -d $IMAGES_DIR ]] || exit 0

# Same keying as images.sh/diagrams.sh so we act on the right manifest.
pane_id="${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}"
pane_file="${pane_id#%}"
session="${CLAUDE_CODE_SESSION_ID:-}"

clear_pane() { rm -f "$IMAGES_DIR/$1.jsonl" "$IMAGES_DIR/$1.owner"; }

# --- This pane's manifest ---
if [[ -n $pane_id && $pane_file =~ ^[A-Za-z0-9_@:.-]+$ ]]; then
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
ttl=$((7 * 86400))
printf -v now '%(%s)T' -1

# Sweep both the manifest and its owner sidecar: an orphaned .owner (its .jsonl
# already gone) would otherwise never be reaped, since clear_pane only fires on a
# base the loop visits. Dedup the bases so a base with both files is handled once.
declare -A gc_seen=()
for m in "$IMAGES_DIR"/*.jsonl "$IMAGES_DIR"/*.owner; do
	[[ -e $m ]] || continue
	base="$(basename "$m")"
	base="${base%.jsonl}"
	base="${base%.owner}"
	[[ $base == "$pane_file" ]] && continue # never GC the pane we just stamped
	[[ -n ${gc_seen[$base]:-} ]] && continue
	gc_seen[$base]=1
	if [[ $base =~ ^[0-9]+$ ]]; then
		# tmux pane files — GC only when we have a reliable live list and the
		# pane is not in it.
		[[ -n $live ]] || continue
		grep -qxF "$base" <<<"$live" || clear_pane "$base"
	else
		# session-keyed files — prune if untouched past the TTL, aging off the
		# newest of the two (either may be absent; the || true keeps a missing-file
		# stat from tripping set -o pipefail and aborting the sweep).
		mtime="$({ stat -c %Y "$IMAGES_DIR/$base.jsonl" "$IMAGES_DIR/$base.owner" 2>/dev/null || true; } | sort -rn | head -1)"
		: "${mtime:=$now}"
		((now - mtime > ttl)) && clear_pane "$base"
	fi
done
