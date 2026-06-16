#!/usr/bin/env bash
# SessionStart(resume) hook: rebuild this pane/session's image manifest from the
# session transcript, so the carousel is populated after `claude --resume` instead
# of empty. Replays each image/diagram-bearing transcript line as a synthetic hook
# payload through the shared extractors. Reads the hook JSON on stdin.
set -euo pipefail

payload="$(cat)"
[[ -n $payload ]] || exit 0
[[ "$(jq -r '.source // empty' <<<"$payload" 2>/dev/null)" == resume ]] || exit 0

transcript="$(jq -r '.transcript_path // empty' <<<"$payload" 2>/dev/null)"
[[ -n $transcript && -r $transcript ]] || exit 0

STATE_DIR="${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
IMAGES_DIR="$STATE_DIR/images"
DIAGRAMS_DIR="$IMAGES_DIR/diagrams"

pane_id="${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}"
[[ -n $pane_id ]] || exit 0
pane_file="${pane_id#%}"
[[ $pane_file =~ ^[A-Za-z0-9_@:.-]+$ ]] || exit 0

# shellcheck source=lib/manifest-extract.sh disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/lib/manifest-extract.sh"

manifest="$IMAGES_DIR/$pane_file.jsonl"
mkdir -p "$IMAGES_DIR"

# Seed the seen-set with paths already in the manifest, then track within-run
# appends, so a kept-manifest resume and a repeated transcript path never double.
declare -A seen=()
if [[ -f $manifest ]]; then
	while IFS= read -r p; do [[ -n $p ]] && seen["$p"]=1; done \
		< <(jq -r '.path // empty' "$manifest" 2>/dev/null)
fi

append_image() { # $1 path  $2 source  $3 ts
	[[ -n ${seen["$1"]:-} ]] && return 0
	seen["$1"]=1
	local mtime
	mtime="$(stat -c %Y "$1" 2>/dev/null || echo 0)"
	jq -nc --arg path "$1" --arg source "$2" --arg ts "$3" --argjson mtime "$mtime" \
		'{type:"image", path:$path, source:$source, ts:$ts, mtime:$mtime}' >>"$manifest"
}

append_diagram() { # $1 png  $2 svg  $3 ts
	[[ -n ${seen["$1"]:-} ]] && return 0
	seen["$1"]=1
	local mtime
	mtime="$(stat -c %Y "$1" 2>/dev/null || echo 0)"
	jq -nc --arg path "$1" --arg vector "$2" --arg source "d2" --arg ts "$3" --argjson mtime "$mtime" \
		'{type:"image", path:$path, vector:$vector, source:$source, ts:$ts, mtime:$mtime}' >>"$manifest"
}

# Only image/diagram-bearing lines reach jq (raw grep fast-bail, like images.sh).
while IFS= read -r line; do
	cwd="$(jq -r '.cwd // empty' <<<"$line" 2>/dev/null)"
	ts="$(jq -r '.timestamp // empty' <<<"$line" 2>/dev/null)"

	# An assistant tool_use line -> synthetic {tool_name, tool_input, cwd}.
	while IFS= read -r tu; do
		[[ -n $tu ]] || continue
		synth="$(jq -nc --argjson tu "$tu" --arg cwd "$cwd" \
			'{tool_name:$tu.name, tool_input:$tu.input, tool_response:{}, cwd:$cwd}')"
		img="$(extract_image_path "$synth")"
		if [[ -n $img ]]; then
			append_image "$img" "$(jq -r '.name' <<<"$tu")" "$ts"
			continue
		fi
		d2="$(extract_d2_path "$synth")"
		if [[ -n $d2 ]]; then
			png="$(d2_render "$d2" "$DIAGRAMS_DIR")" || continue
			append_diagram "$png" "${png%.png}.svg" "$ts"
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
done < <(grep -nE '\.(png|jpe?g|gif|webp|bmp|d2)' "$transcript" | cut -d: -f2-)

# Claim the rebuilt manifest so the live hooks' owner self-heal does not drop it.
if [[ -f $manifest && -n ${CLAUDE_CODE_SESSION_ID:-} ]]; then
	printf '%s' "$CLAUDE_CODE_SESSION_ID" >"$IMAGES_DIR/$pane_file.owner"
fi
