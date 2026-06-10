#!/usr/bin/env bash
# Append images Claude touches (Read/Write/screenshots) to a per-pane manifest.
# PostToolUse hook: reads the hook JSON payload on stdin.
# Mirrors claude-status-update.sh — self-contained, keyed by $TMUX_PANE or $CLAUDE_CODE_SESSION_ID.
set -euo pipefail

STATE_DIR="${CLAUDE_STATUS_DIR:-/tmp/claude-status}"
IMAGES_DIR="$STATE_DIR/images"

# Key by tmux pane when inside tmux, else the Claude Code session id so the
# carousel works in a bare terminal. No pane and no session id → no-op.
pane_id="${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}"
[[ -n $pane_id ]] || exit 0
pane_file="${pane_id#%}"

payload="$(cat)"
[[ -n $payload ]] || exit 0

cwd="$(jq -r '.cwd // empty' <<<"$payload" 2>/dev/null)"
source_tool="$(jq -r '.tool_name // "?"' <<<"$payload" 2>/dev/null)"

# Resolve a candidate path against cwd if relative.
resolve_path() {
	local p="$1"
	if [[ $p != /* ]] && [[ -n $cwd ]]; then
		p="$cwd/$p"
	fi
	printf '%s' "$p"
}

# Check image extension (case-insensitive).
is_image_ext() {
	local p="$1"
	[[ ${p,,} =~ \.(png|jpe?g|gif|webp|bmp)$ ]]
}

path=""

# Phase 1: try explicit tool_input paths (file_path, path, output_path).
# Only accept if the candidate is an image extension AND the file exists.
# We do NOT try tool_input.filename here — relative bare names (e.g. "shot.png")
# are ambiguous; fall through to the response scan instead.
for candidate_raw in \
	"$(jq -r '.tool_input.file_path // empty' <<<"$payload" 2>/dev/null)" \
	"$(jq -r '.tool_input.path // empty' <<<"$payload" 2>/dev/null)" \
	"$(jq -r '.tool_input.output_path // empty' <<<"$payload" 2>/dev/null)"; do
	[[ -n $candidate_raw ]] || continue
	candidate="$(resolve_path "$candidate_raw")"
	is_image_ext "$candidate" || continue
	[[ -f $candidate ]] || continue
	path="$candidate"
	break
done

# Phase 2: if no tool_input path matched, scan tool_response strings for an
# embedded image path. We look for path-like tokens (starting with / or ./)
# that end with an image extension, extracted via capture from each string.
if [[ -z $path ]]; then
	response_path="$(jq -r '
    [.tool_response | .. | strings
      | select(length < 4096)
      | capture("(?<p>(?:/|\\./)[^\\s]*\\.(?:png|jpe?g|gif|webp|bmp))"; "i")
      | .p
    ] | first // empty
  ' <<<"$payload" 2>/dev/null)"
	if [[ -n $response_path ]]; then
		response_path="$(resolve_path "$response_path")"
		if is_image_ext "$response_path" && [[ -f $response_path ]]; then
			path="$response_path"
		fi
	fi
fi

[[ -n $path ]] || exit 0

mtime="$(stat -c %Y "$path" 2>/dev/null || echo 0)"
printf -v now '%(%FT%T%z)T' -1

manifest="$IMAGES_DIR/$pane_file.jsonl"
mkdir -p "$IMAGES_DIR"

# Dedup by (path, mtime).
# Best-effort dedup: concurrent hook firings can still produce duplicate
# (path,mtime) entries. Manifest consumers must tolerate duplicates.
if [[ -f $manifest ]] &&
	jq -e --arg p "$path" --argjson m "$mtime" \
		'select(.path == $p and .mtime == $m)' "$manifest" >/dev/null 2>&1; then
	exit 0
fi

jq -nc --arg path "$path" --arg source "$source_tool" --arg ts "$now" --argjson mtime "$mtime" \
	'{type:"image", path:$path, source:$source, ts:$ts, mtime:$mtime}' >>"$manifest"
