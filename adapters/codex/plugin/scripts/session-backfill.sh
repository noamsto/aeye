#!/usr/bin/env bash
# SessionStart(resume) hook: rebuild this pane/session's image manifest from the
# Codex rollout transcript, so the carousel is populated after `codex resume`
# instead of empty. Mirrors the Claude adapter's session-backfill.sh, but the
# rollout .jsonl stores the RAW exec/JS transport (unlike the normalized
# PostToolUse hook payload) — each candidate line is first unwrapped into a
# clean {tool_name, tool_input} shape, then replayed through the shared
# codex_extract_touched_paths extractor.
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

PLUGIN_ROOT="${PLUGIN_ROOT:-$(dirname "${BASH_SOURCE[0]}")/..}"
# shellcheck source=lib/shim.sh disable=SC1091
source "$PLUGIN_ROOT/scripts/lib/shim.sh"
# shellcheck source=../../core/manifest-lifecycle.sh disable=SC1091
source "$PLUGIN_ROOT/../../core/manifest-lifecycle.sh"

resolve_state_dirs

session="$(codex_session_id "$payload")"
pane_id="${TMUX_PANE:-$session}"
[[ -n $pane_id ]] || exit 0
pane_file="${pane_id#%}"
valid_pane_file "$pane_file" || exit 0

manifest_paths "$pane_file"
# shellcheck disable=SC2153 # MANIFEST/OWNER_FILE come from core's manifest_paths
manifest="$MANIFEST"
# shellcheck disable=SC2153
owner_file="$OWNER_FILE"
mkdir -p "$IMAGES_DIR"

# Hold the pane's manifest lock across the whole clear/rebuild — a live images.sh
# or diagrams.sh append fired right after resume must wait, not interleave with
# the authoritative rebuild (which starts by wiping the manifest).
_manifest_lock "$LOCK_FILE"

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
	append_image_line "$manifest" "$1" "$2" "$3"
}

append_diagram() { # $1 png  $2 svg  $3 ts  $4 name
	[[ -n ${seen["$1"]:-} ]] && return 0
	seen["$1"]=1
	append_diagram_line "$manifest" "$1" "$2" "$4" "$3"
}

# _codex_unwrap_apply_patch JS -> decoded patch envelope (empty if none). The
# apply_patch arg is a JSON-escaped string, so lift just that argument out of
# the surrounding JS call and decode it with jq.
_codex_unwrap_apply_patch() {
	local s
	s="$(grep -oE 'tools\.apply_patch\("([^"\\]|\\.)*"' <<<"$1" | sed -E 's/^tools\.apply_patch\(//')"
	[[ -n $s ]] && jq -r . <<<"$s" 2>/dev/null
}

# _codex_unwrap_view_image JS -> path arg (quoted or unquoted key). The object
# literal can use an unquoted `path:` key, so it isn't valid JSON — unwrap with
# a regex instead of jq fromjson.
_codex_unwrap_view_image() {
	grep -oE 'tools\.view_image\(\{[[:space:]]*"?path"?[[:space:]]*:[[:space:]]*"[^"]+"' <<<"$1" |
		grep -oE '"[^"]+"$' | tr -d '"'
}

# Only lines that could plausibly carry an image/.d2, or set the running cwd,
# reach jq (raw grep fast-bail, like images.sh). A crashed prior session can
# leave a truncated, non-JSON final line; jq exits nonzero on it and the bare
# assignment would abort the whole rebuild under set -e, so every jq call
# below defaults to empty on failure and lets that line yield nothing.
cwd=""
while IFS= read -r line; do
	otype="$(jq -r '.type // empty' <<<"$line" 2>/dev/null)" || otype=""
	ts="$(jq -r '.timestamp // empty' <<<"$line" 2>/dev/null)" || ts=""

	# session_meta/turn_context carry the cwd tool calls resolve relative paths
	# against; unlike Claude's transcript, rollout tool-call lines have none of
	# their own.
	if [[ $otype == session_meta || $otype == turn_context ]]; then
		new_cwd="$(jq -r '.payload.cwd // empty' <<<"$line" 2>/dev/null)" || new_cwd=""
		[[ -n $new_cwd ]] && cwd="$new_cwd"
		continue
	fi
	[[ $otype == response_item ]] || continue

	ptype="$(jq -r '.payload.type // empty' <<<"$line" 2>/dev/null)" || ptype=""
	pname="$(jq -r '.payload.name // empty' <<<"$line" 2>/dev/null)" || pname=""

	synth=""
	tool_name=""
	case "$ptype" in
	custom_tool_call)
		case "$pname" in
		exec)
			# The unified exec/JS transport: unwrap whichever call this is.
			# tools.exec_command(...) calls (plain shell) unwrap to neither and
			# fall through — only their paired output can carry a screenshot.
			input="$(jq -r '.payload.input // empty' <<<"$line" 2>/dev/null)" || input=""
			cmd="$(_codex_unwrap_apply_patch "$input")" || cmd=""
			if [[ -n $cmd ]]; then
				tool_name="apply_patch"
				synth="$(jq -nc --arg cmd "$cmd" --arg cwd "$cwd" \
					'{tool_name:"apply_patch", tool_input:{command:$cmd}, tool_response:{}, cwd:$cwd}')"
			else
				path="$(_codex_unwrap_view_image "$input")" || path=""
				if [[ -n $path ]]; then
					tool_name="view_image"
					synth="$(jq -nc --arg path "$path" --arg cwd "$cwd" \
						'{tool_name:"view_image", tool_input:{path:$path}, tool_response:{}, cwd:$cwd}')"
				fi
			fi
			;;
		apply_patch)
			# Legacy direct form: the raw patch envelope, not JS-wrapped.
			cmd="$(jq -r '.payload.input // empty' <<<"$line" 2>/dev/null)" || cmd=""
			tool_name="apply_patch"
			synth="$(jq -nc --arg cmd "$cmd" --arg cwd "$cwd" \
				'{tool_name:"apply_patch", tool_input:{command:$cmd}, tool_response:{}, cwd:$cwd}')"
			;;
		esac
		;;
	custom_tool_call_output | function_call_output)
		# Covers both the modern exec-transport output and the older
		# function_call/exec_command's paired output — either can carry an
		# embedded screenshot path, scanned via the shared tool_response scanner.
		out="$(jq -c '.payload.output // empty' <<<"$line" 2>/dev/null)" || out=""
		[[ -z $out ]] && out=null
		tool_name="screenshot"
		synth="$(jq -nc --argjson out "$out" --arg cwd "$cwd" \
			'{tool_name:"?", tool_input:{}, tool_response:$out, cwd:$cwd}')"
		;;
	esac

	[[ -n $synth ]] || continue

	while IFS= read -r p; do
		[[ -n $p ]] || continue
		if [[ ${p,,} == *.d2 ]]; then
			png="$(d2_render "$p" "$DIAGRAMS_DIR")" || continue
			append_diagram "$png" "${png%.png}.svg" "$ts" "$(basename "$p" .d2)"
		else
			append_image "$p" "$tool_name" "$ts"
		fi
	done < <(codex_extract_touched_paths "$synth")
done < <(grep -E '\.(png|jpe?g|gif|webp|bmp|d2)|"type":"(session_meta|turn_context)"' "$transcript")

# Claim the rebuilt manifest so the live hooks' owner self-heal does not drop it.
if [[ -f $manifest && -n $session ]]; then
	printf '%s' "$session" >"$owner_file"
fi
