#!/usr/bin/env bash
# hookyard session_start handler for the aeye pi adapter. hookyard route feeds a
# normalized envelope on stdin: top-level session_id/cwd, plus a `native` object
# carrying pi's own session_start fields (native.reason, native.session_file).
#
# Runs diagram guidance, a history-driven manifest rebuild when the transcript
# has historical calls, and the pane-ownership reset — then replies with the
# guidance text, if any.
#
# session-reset.sh must run regardless of whether diagram-guidance.sh or the
# backfill step fails. That guarantee is structural, not a chain of `||`
# guards on each intervening step: an EXIT trap invokes session-reset.sh
# unconditionally, using only session_id/cwd/source (established once, right
# after payload parsing, each with its own `|| var=""` fallback) — so a later
# failure anywhere below (a broken mktemp, jq, or sub-script) can never rob
# session-reset.sh of its turn. `set -e` stays on for everything else; only
# the reset step is exempted, by construction, from needing per-line guards.
set -euo pipefail

dir="$(dirname "${BASH_SOURCE[0]}")"

payload="$(cat)"
[[ -n $payload ]] || exit 0

session_id="$(jq -r '.session_id // empty' <<<"$payload")" || session_id=""
cwd="$(jq -r '.cwd // empty' <<<"$payload")" || cwd=""
reason="$(jq -r '.native.reason // empty' <<<"$payload")" || reason=""
session_file="$(jq -r '.native.session_file // empty' <<<"$payload")" || session_file=""

case "$reason" in
resume | fork) source=resume ;;
reload) source=reload ;;
new) source=new ;;
*) source=startup ;;
esac

run_reset() {
	jq -nc --arg sid "$session_id" --arg cwd "$cwd" --arg source "$source" \
		'{session_id: $sid, cwd: $cwd, source: $source}' 2>/dev/null |
		"$dir/../scripts/session-reset.sh" >/dev/null 2>&1 || true
}
trap run_reset EXIT

base="$(jq -nc --arg sid "$session_id" --arg cwd "$cwd" --arg source "$source" \
	'{session_id: $sid, cwd: $cwd, source: $source}')" || base=""

guidance=""
if [[ -n $base ]]; then
	guidance_out="$(printf '%s' "$base" | "$dir/../scripts/diagram-guidance.sh" 2>/dev/null)" || guidance_out=""
	guidance="$(jq -r '.hookSpecificOutput.additionalContext // empty' <<<"$guidance_out" 2>/dev/null)" || guidance=""
fi

if [[ -n $session_file && -n $base ]]; then
	calls_file="$(mktemp 2>/dev/null)" || calls_file=""
	if [[ -n $calls_file ]]; then
		"$dir/backfill-calls.sh" "$session_file" "$cwd" "$session_id" >"$calls_file" 2>/dev/null || true

		if [[ -s $calls_file ]]; then
			backfill_payload="$(jq -nc --arg sid "$session_id" --arg cwd "$cwd" --arg source "$source" --arg cf "$calls_file" \
				'{session_id: $sid, cwd: $cwd, source: $source, calls_file: $cf}')" || backfill_payload=""
			if [[ -n $backfill_payload ]]; then
				printf '%s' "$backfill_payload" | "$dir/../scripts/session-backfill.sh" >/dev/null 2>&1 || true
			fi
		fi

		rm -f "$calls_file"
	fi
fi

if [[ -n $guidance ]]; then
	jq -n --arg ctx "$guidance" '{hookSpecificOutput: {additionalContext: $ctx}}'
fi
