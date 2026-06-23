#!/usr/bin/env bash
# SessionStart hook. Keeps the carousel from showing a different session's images
# when a tmux pane id is reused (tmux renumbers panes from low values on every
# server restart, and the manifest dir is shared machine-wide). Two jobs:
#   1. This pane's manifest — clear it on a fresh start (startup|clear), or on any
#      start whose recorded owner is a *different* session (a resume/compact that
#      landed in a recycled pane id). Keep it when the owner is this same session
#      (a genuine continuation — the backfill hook reconciles it). Then stamp the
#      owner now, before the viewer reads, so a reader launched right after start
#      sees a manifest that belongs to this session.
#   2. GC — sweep manifests for tmux panes no longer in the server, and
#      session-keyed manifests past a TTL, so the shared dir never grows without
#      bound. Reads the hook JSON on stdin.
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
		;;
	*)
		# resume/compact/unknown: clear only when the pane was last owned by a
		# different session — the reused-pane-id bleed. Same session continues.
		[[ -n $session && -n $owner && $owner != "$session" ]] && clear_pane "$pane_file"
		;;
	esac
	[[ -n $session ]] && printf '%s' "$session" >"$owner_file"
fi

# --- GC the shared dir ---
# In tmux a manifest for a pane id absent from the server is dead. Outside tmux,
# session-keyed manifests have no liveness signal, so age them out instead.
live=""
[[ -n ${TMUX:-} ]] && command -v tmux >/dev/null 2>&1 &&
	live="$(tmux list-panes -a -F '%#{pane_id}' 2>/dev/null | tr -d '%')"
ttl=$((7 * 86400))
printf -v now '%(%s)T' -1

for m in "$IMAGES_DIR"/*.jsonl; do
	[[ -e $m ]] || continue
	base="$(basename "$m" .jsonl)"
	[[ $base == "$pane_file" ]] && continue # never GC the pane we just stamped
	if [[ $base =~ ^[0-9]+$ ]]; then
		# tmux pane manifest — GC only when we have a reliable live list and the
		# pane is not in it.
		[[ -n $live ]] || continue
		grep -qxF "$base" <<<"$live" || clear_pane "$base"
	else
		# session-keyed manifest — prune if untouched past the TTL.
		mtime="$(stat -c %Y "$m" 2>/dev/null || echo "$now")"
		((now - mtime > ttl)) && clear_pane "$base"
	fi
done
