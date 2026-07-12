#!/usr/bin/env bash
# SessionStart(resume) hook: rebuild this pane/session's image manifest from the
# session transcript, so the carousel is populated after `claude --resume` instead
# of empty. Replays each image/diagram-bearing transcript line as a synthetic hook
# payload through the shared extractors. Reads the hook JSON on stdin.
#
# The transcript is authoritative: the manifest is rebuilt from scratch, so a
# prior session's images left under a reused tmux pane id cannot bleed through.
# This is the sole writer of a resumed pane's manifest — session-reset defers to
# it on resume because SessionStart hooks run in parallel with no ordered turn.
set -euo pipefail

payload="$(cat)"
[[ -n $payload ]] || exit 0
[[ "$(jq -r '.source // empty' <<<"$payload" 2>/dev/null)" == resume ]] || exit 0

transcript="$(jq -r '.transcript_path // empty' <<<"$payload" 2>/dev/null)"

STATE_DIR="${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
IMAGES_DIR="$STATE_DIR/images"
DIAGRAMS_DIR="$IMAGES_DIR/diagrams"

pane_id="${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}"
[[ -n $pane_id ]] || exit 0
pane_file="${pane_id#%}"
[[ $pane_file =~ ^[A-Za-z0-9_@:.-]+$ ]] || exit 0

manifest="$IMAGES_DIR/$pane_file.jsonl"
owner_file="$IMAGES_DIR/$pane_file.owner"
session="${CLAUDE_CODE_SESSION_ID:-}"
mkdir -p "$IMAGES_DIR"

# shellcheck source=lib/manifest-extract.sh disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/lib/manifest-extract.sh"

# Hold the pane's manifest lock across the whole clear/rebuild — a live images.sh
# or diagrams.sh append fired right after resume must wait, not interleave with
# the authoritative rebuild (which starts by wiping the manifest).
_manifest_lock "$IMAGES_DIR/$pane_file.lock"

# Without a readable transcript we can't rebuild. Drop a manifest we can't prove
# belongs to this session (the reused-pane-id bleed); keep one this session owns
# (a legit continuation whose transcript is unreadable) rather than blank it.
if [[ -z $transcript || ! -r $transcript ]]; then
	owner=""
	[[ -f $owner_file ]] && owner="$(<"$owner_file")"
	[[ -f $manifest && (-z $owner || $owner != "$session") ]] && rm -f "$manifest" "$owner_file"
	exit 0
fi

# Authoritative rebuild: the transcript is the record of what this session
# touched, so start from empty rather than merge into whatever the pane held —
# on a reused pane id that's a prior session's images. seen dedups within this
# replay only (a repeated transcript path).
rm -f "$manifest"
declare -A seen=()

append_image() { # $1 path  $2 source  $3 ts
	[[ -n ${seen["$1"]:-} ]] && return 0
	seen["$1"]=1
	local mtime
	mtime="$(_mtime "$1")"
	jq -nc --arg path "$1" --arg source "$2" --arg ts "$3" --argjson mtime "$mtime" \
		'{type:"image", path:$path, source:$source, ts:$ts, mtime:$mtime}' >>"$manifest"
}

append_diagram() { # $1 png  $2 svg  $3 ts  $4 name
	[[ -n ${seen["$1"]:-} ]] && return 0
	seen["$1"]=1
	local mtime
	mtime="$(_mtime "$1")"
	jq -nc --arg path "$1" --arg vector "$2" --arg source "d2" --arg name "$4" --arg ts "$3" --argjson mtime "$mtime" \
		'{type:"image", path:$path, vector:$vector, source:$source, name:$name, ts:$ts, mtime:$mtime}' >>"$manifest"
}

# Only image/diagram-bearing lines reach jq (raw grep fast-bail, like images.sh).
while IFS= read -r line; do
	# A crashed prior session can leave a truncated, non-JSON final line; jq exits
	# nonzero on it and the bare assignment would abort the whole backfill under
	# set -e. Default to empty and let the line yield nothing (its inner jq scans
	# are in process substitutions, which already swallow the parse error).
	cwd="$(jq -r '.cwd // empty' <<<"$line" 2>/dev/null)" || cwd=""
	ts="$(jq -r '.timestamp // empty' <<<"$line" 2>/dev/null)" || ts=""

	# An assistant tool_use line -> synthetic {tool_name, tool_input, cwd}.
	while IFS= read -r tu; do
		[[ -n $tu ]] || continue
		synth="$(jq -nc --argjson tu "$tu" --arg cwd "$cwd" \
			'{tool_name:$tu.name, tool_input:$tu.input, tool_response:{}, cwd:$cwd}')"
		img="$(extract_image_path "$synth")"
		if [[ -n $img ]]; then
			append_image "$img" "$(jq -r '.name // "?"' <<<"$tu")" "$ts"
			continue
		fi
		d2="$(extract_d2_path "$synth")"
		if [[ -n $d2 ]]; then
			png="$(d2_render "$d2" "$DIAGRAMS_DIR")" || continue
			append_diagram "$png" "${png%.png}.svg" "$ts" "$(basename "$d2" .d2)"
		fi
	done < <(jq -c '.message.content[]? | select(.type=="tool_use")' <<<"$line" 2>/dev/null)

	# A user tool_result line -> synthetic {tool_response, cwd} for screenshot paths.
	while IFS= read -r tr; do
		[[ -n $tr ]] || continue
		synth="$(jq -nc --argjson tr "$tr" --arg cwd "$cwd" \
			'{tool_name:"?", tool_input:{}, tool_response:$tr, cwd:$cwd}')"
		img="$(extract_image_path "$synth")"
		[[ -n $img ]] && append_image "$img" "screenshot" "$ts"
	done < <(jq -c '.message.content[]? | select(.type=="tool_result") | .content' <<<"$line" 2>/dev/null)
done < <(grep -E '\.(png|jpe?g|gif|webp|bmp|d2)' "$transcript")

# Claim the rebuilt manifest so the live hooks' owner self-heal does not drop it.
if [[ -f $manifest && -n ${CLAUDE_CODE_SESSION_ID:-} ]]; then
	printf '%s' "$CLAUDE_CODE_SESSION_ID" >"$IMAGES_DIR/$pane_file.owner"
fi
