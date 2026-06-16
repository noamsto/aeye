#!/usr/bin/env bash
# Append images Claude touches (Read/Write/screenshots) to a per-pane manifest.
# PostToolUse hook: reads the hook JSON payload on stdin.
# Mirrors claude-status-update.sh — self-contained, keyed by $TMUX_PANE or $CLAUDE_CODE_SESSION_ID.
set -euo pipefail

STATE_DIR="${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
IMAGES_DIR="$STATE_DIR/images"

# Key by tmux pane when inside tmux, else the Claude Code session id so the
# carousel works in a bare terminal. No pane and no session id → no-op.
pane_id="${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}"
[[ -n $pane_id ]] || exit 0
pane_file="${pane_id#%}"
# Guard against path traversal: the key becomes a filename; reject anything
# with path separators or outside a safe set (panes are %<int>, sessions are ids).
[[ $pane_file =~ ^[A-Za-z0-9_@:.-]+$ ]] || exit 0

payload="$(cat)"
[[ -n $payload ]] || exit 0

# shellcheck source=lib/manifest-extract.sh disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/lib/manifest-extract.sh"

source_tool="$(jq -r '.tool_name // "?"' <<<"$payload" 2>/dev/null)"
path="$(extract_image_path "$payload")"
[[ -n $path ]] || exit 0

mtime="$(stat -c %Y "$path" 2>/dev/null || echo 0)"
printf -v now '%(%FT%T%z)T' -1

manifest="$IMAGES_DIR/$pane_file.jsonl"
mkdir -p "$IMAGES_DIR"

# Self-heal against tmux pane-id reuse: a manifest last written by a different
# Claude session belongs to a pane that's since been recycled — drop it so this
# session's carousel never blends in a prior session's images. (The SessionStart
# reset already covers fresh starts; this also guards a start the reset missed.)
owner="$IMAGES_DIR/$pane_file.owner"
if [[ -n ${CLAUDE_CODE_SESSION_ID:-} ]]; then
	if [[ -f $owner && $(<"$owner") != "$CLAUDE_CODE_SESSION_ID" ]]; then
		rm -f "$manifest"
	fi
	printf '%s' "$CLAUDE_CODE_SESSION_ID" >"$owner"
fi

# Append-only: no write-side dedup. Concurrent firings can emit duplicate
# (path,mtime) lines; the viewer collapses them on read (parseManifest).
jq -nc --arg path "$path" --arg source "$source_tool" --arg ts "$now" --argjson mtime "$mtime" \
	'{type:"image", path:$path, source:$source, ts:$ts, mtime:$mtime}' >>"$manifest"
