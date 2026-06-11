#!/usr/bin/env bash
# Render a .d2 file the agent wrote into a PNG and append it to the per-pane
# image manifest. PostToolUse hook: reads the hook JSON payload on stdin.
# Mirrors images.sh — self-contained, keyed by $TMUX_PANE or $CLAUDE_CODE_SESSION_ID.
set -euo pipefail

STATE_DIR="${AGENT_CAROUSEL_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
IMAGES_DIR="$STATE_DIR/images"
DIAGRAMS_DIR="$IMAGES_DIR/diagrams"

pane_id="${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}"
[[ -n $pane_id ]] || exit 0
pane_file="${pane_id#%}"
[[ $pane_file =~ ^[A-Za-z0-9_@:.-]+$ ]] || exit 0

payload="$(cat)"
[[ -n $payload ]] || exit 0

cwd="$(jq -r '.cwd // empty' <<<"$payload" 2>/dev/null)"

# The agent's .d2 file path comes from tool_input.file_path (Write/Edit/MultiEdit).
candidate="$(jq -r '.tool_input.file_path // empty' <<<"$payload" 2>/dev/null)"
[[ -n $candidate ]] || exit 0
if [[ $candidate != /* ]] && [[ -n $cwd ]]; then
	candidate="$cwd/$candidate"
fi
# Fast-bail: only .d2 files that exist.
[[ ${candidate,,} == *.d2 ]] || exit 0
[[ -f $candidate ]] || exit 0

mkdir -p "$DIAGRAMS_DIR"
hash="$(sha256sum "$candidate" | cut -c1-16)"
png="$DIAGRAMS_DIR/$hash.png"

# Render browser-free (d2 -> svg -> resvg -> png). Never `d2 ... .png` (Chromium).
svg="$DIAGRAMS_DIR/$hash.svg"
d2 "$candidate" "$svg"
resvg "$svg" "$png"
rm -f "$svg"

mtime="$(stat -c %Y "$png" 2>/dev/null || echo 0)"
printf -v now '%(%FT%T%z)T' -1

manifest="$IMAGES_DIR/$pane_file.jsonl"
jq -nc --arg path "$png" --arg source "d2" --arg ts "$now" --argjson mtime "$mtime" \
	'{type:"image", path:$path, source:$source, ts:$ts, mtime:$mtime}' >>"$manifest"
