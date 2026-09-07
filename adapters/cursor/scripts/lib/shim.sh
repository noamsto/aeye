#!/usr/bin/env bash
# Cursor-specific session id + path-extraction helpers, layered on the
# agent-agnostic core. Cursor CLI payloads leave .cwd empty and put the real
# workspace in workspace_roots[0]; Read/Write use tool_input.file_path (with a
# .path fallback). Pure: no manifest writes, no keying, no toggle. Each function
# echoes a result (or nothing) and returns 0 so callers under
# `set -euo pipefail` are never aborted by a "not found" outcome.

# shellcheck source=../core/manifest-extract.sh disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/../core/manifest-extract.sh"

# cursor_session_id PAYLOAD -> echoes .conversation_id // .session_id or nothing.
cursor_session_id() {
	jq -r '.conversation_id // .session_id // empty' <<<"$1" 2>/dev/null
	return 0
}

# cursor_effective_cwd PAYLOAD -> echoes .cwd if non-empty, else
# .workspace_roots[0], else nothing.
cursor_effective_cwd() {
	jq -r 'if (.cwd // "") != "" then .cwd else (.workspace_roots[0] // empty) end' <<<"$1" 2>/dev/null
	return 0
}

# cursor_extract_touched_paths PAYLOAD -> echoes newline-separated existing
# image/.d2 paths this call wrote, read, or saved (Shell output).
cursor_extract_touched_paths() {
	local payload="$1" cwd name
	cwd="$(cursor_effective_cwd "$payload")"
	name="$(jq -r '.tool_name // empty' <<<"$payload" 2>/dev/null)"

	emit() { # $1 raw path -> resolve, filter, existence-check, print
		local q="$1"
		[[ -z $q ]] && return 0
		[[ $q != /* && -n $cwd ]] && q="$cwd/$q"
		[[ ${q,,} =~ \.(png|jpe?g|gif|webp|bmp|d2)$ ]] || return 0
		[[ -f $q ]] || return 0
		printf '%s\n' "$q"
	}

	case "$name" in
	Read | Write)
		emit "$(jq -r '.tool_input.file_path // .tool_input.path // empty' <<<"$payload" 2>/dev/null)"
		;;
	esac

	# screenshots embedded in tool output (Shell) — shared scanner.
	# Core's containment guard needs .cwd set, so rewrite empty cwd first.
	# scan_response_image_path prints via printf '%s' (no trailing newline),
	# so terminate it here or a read-loop consumer drops this final line.
	local rewritten resp
	rewritten="$(jq -c --arg c "$cwd" '.cwd=$c' <<<"$payload" 2>/dev/null)" || rewritten=""
	[[ -n $rewritten ]] || rewritten="$payload"
	resp="$(scan_response_image_path "$rewritten")"
	[[ -n $resp ]] && printf '%s\n' "$resp"
	return 0
}

# cursor_resume_transcript SESSION_ID -> echoes the path to this conversation's
# agent-transcript file iff exactly one exists and is non-empty, else nothing.
# The existence of a non-empty transcript IS the resume signal (Cursor's
# sessionStart payload carries no `source` field to switch on) — a brand-new
# conversation has no transcript file yet. Glob on the id instead of deriving
# <slug> from workspace_roots: the one observed slug rule (`/`->`-`) is not
# guaranteed stable, the id is.
cursor_resume_transcript() {
	local sid="$1" root matches
	[[ $sid =~ ^[A-Za-z0-9_-]+$ ]] || return 0
	root="${AEYE_CURSOR_HOME:-$HOME/.cursor}"
	matches=("$root"/projects/*/agent-transcripts/"$sid"/"$sid".jsonl)
	[[ ${#matches[@]} -eq 1 && -s ${matches[0]} ]] && printf '%s' "${matches[0]}"
	return 0
}
