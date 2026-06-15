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

# Fast path: PostToolUse fires on every tool call, but only a few touch an image.
# Bail before spawning any jq unless the raw payload even mentions an image
# extension. No false negatives: both phases below also require the extension to
# appear literally, so a payload without one would yield no path anyway.
# nocasematch keeps the match case-insensitive without ${payload,,}, which would
# copy the whole payload — megabytes when a Read embeds file contents.
shopt -s nocasematch
[[ $payload =~ \.(png|jpe?g|gif|webp|bmp) ]] || exit 0
shopt -u nocasematch

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

# Append-only: no write-side dedup. Concurrent firings can emit duplicate
# (path,mtime) lines; the viewer collapses them on read (parseManifest).
jq -nc --arg path "$path" --arg source "$source_tool" --arg ts "$now" --argjson mtime "$mtime" \
	'{type:"image", path:$path, source:$source, ts:$ts, mtime:$mtime}' >>"$manifest"
