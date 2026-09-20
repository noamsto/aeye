#!/usr/bin/env bash
# Handler exec for pi's tool_result hook, spawned by `hookyard route` with a
# normalized envelope on stdin. Our stdout is parsed as
# {"hookSpecificOutput":{"additionalContext": "..."}}; empty/unparsable stdout
# is a silent abstain, not an error.
#
# Fast-bails on tool name, then on an image/.d2 regex match against the built
# payload, before spawning images.sh / diagrams.sh.
set -euo pipefail

payload="$(cat)"

# hookyard's router capitalizes pi's native lowercase tool names (write/read/bash
# -> Write/Read/Bash) via its cross-agent vocabulary map, but pi's own `edit` and
# `shell` names have no entry in that map and arrive unchanged. Lowercase here so
# the comparison is agent-agnostic, and pass the lowercased form downstream: the
# shared scripts and the manifest's "source" column expect pi's native lowercase
# spelling, not hookyard's normalized one. Do not pass the envelope's tool_name
# through verbatim.
tool_name="$(jq -r '.tool_name // empty' <<<"$payload" | tr '[:upper:]' '[:lower:]')" || tool_name=""

case "$tool_name" in
read | write | edit | bash | shell) ;;
*) exit 0 ;;
esac

built="$(jq -c --arg tn "$tool_name" '{
	tool_name: $tn,
	tool_input: .tool_input,
	tool_response: .native.tool_response,
	cwd: .cwd,
	session_id: .session_id
}' <<<"$payload")"

# Fast-bail: never spawn images.sh/diagrams.sh for a call that plainly touched
# neither an image nor a `.d2` (mirrors aeye.ts's IMAGE_RE/D2_RE fast-bail).
haystack="$(jq -r '(.tool_input | tojson) + "\n" + (.tool_response | tojson)' <<<"$built")"

has_image=0
has_d2=0
grep -qiE '\.(png|jpe?g|gif|webp|bmp)\b' <<<"$haystack" && has_image=1
grep -qiE '\.d2\b' <<<"$haystack" && has_d2=1

if [[ $has_image -eq 0 && $has_d2 -eq 0 ]]; then
	exit 0
fi

script_dir="$(dirname "${BASH_SOURCE[0]}")"

extract_ctx() {
	local out="$1" ctx
	[[ -n $out ]] || return 0
	ctx="$(jq -r '.hookSpecificOutput.additionalContext // empty' <<<"$out" 2>/dev/null)" || return 0
	[[ -n $ctx ]] && printf '%s' "$ctx"
}

warnings=()

if [[ $has_image -eq 1 ]]; then
	# `|| true`: a non-zero exit from images.sh must not skip diagrams.sh below
	# when both regexes matched.
	img_out="$(printf '%s' "$built" | "$script_dir/../scripts/images.sh")" || true
	ctx="$(extract_ctx "$img_out")"
	[[ -n $ctx ]] && warnings+=("$ctx")
fi

if [[ $has_d2 -eq 1 ]]; then
	d2_out="$(printf '%s' "$built" | "$script_dir/../scripts/diagrams.sh")" || true
	ctx="$(extract_ctx "$d2_out")"
	[[ -n $ctx ]] && warnings+=("$ctx")
fi

if [[ ${#warnings[@]} -eq 0 ]]; then
	exit 0
fi

joined=""
for w in "${warnings[@]}"; do
	if [[ -z $joined ]]; then
		joined="$w"
	else
		joined+=$'\n\n'"$w"
	fi
done

jq -n --arg ctx "$joined" '{hookSpecificOutput:{additionalContext:$ctx}}'
