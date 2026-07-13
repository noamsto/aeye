#!/usr/bin/env bash
# Codex-specific session id + path-extraction helpers, layered on the
# agent-agnostic core. Codex normalizes the PostToolUse payload: tool_name is a
# clean apply_patch|view_image|Bash and tool_input is structured, so no JS-string
# unwrapping is needed here (that's a backfill concern — the raw rollout
# transcript uses the unwrapped exec/JS transport). Pure: no manifest writes, no
# keying, no toggle. Each function echoes a result (or nothing) and returns 0 so
# callers under `set -euo pipefail` are never aborted by a "not found" outcome.

# shellcheck source=../core/manifest-extract.sh disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/../core/manifest-extract.sh"

# _codex_apply_patch_paths ENVELOPE -> echoes each Add/Update File path (one
# per line) from an apply_patch envelope's `*** Begin/End Patch` command text.
_codex_apply_patch_paths() {
	sed -n 's/^\*\*\* \(Add\|Update\) File: //p' <<<"$1"
}

# codex_extract_touched_paths PAYLOAD -> echoes newline-separated existing
# image/.d2 paths this call wrote or viewed.
codex_extract_touched_paths() {
	local payload="$1" cwd name p
	cwd="$(jq -r '.cwd // empty' <<<"$payload" 2>/dev/null)"
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
	apply_patch)
		local env
		env="$(jq -r '.tool_input.command // empty' <<<"$payload" 2>/dev/null)"
		while IFS= read -r p; do emit "$p"; done < <(_codex_apply_patch_paths "$env")
		;;
	view_image)
		emit "$(jq -r '.tool_input.path // empty' <<<"$payload" 2>/dev/null)"
		;;
	esac

	# screenshots embedded in tool output (Bash/MCP) — shared scanner.
	# scan_response_image_path prints via printf '%s' (no trailing newline),
	# so terminate it here or a read-loop consumer drops this final line.
	local resp
	resp="$(scan_response_image_path "$payload")"
	[[ -n $resp ]] && printf '%s\n' "$resp"
	return 0
}

# codex_session_id PAYLOAD -> echoes .session_id or nothing.
codex_session_id() { jq -r '.session_id // empty' <<<"$1" 2>/dev/null; }
