#!/usr/bin/env bash
# Claude-specific path-extraction helpers for the image/diagram hooks and the
# resume backfill, layered on the agent-agnostic core. Pure: no manifest
# writes, no keying, no toggle. Each function echoes a result (or nothing) and
# returns 0 so callers under `set -euo pipefail` are never aborted by a
# "not found" outcome.

# shellcheck source=../../../../core/manifest-extract.sh disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/../../../../core/manifest-extract.sh"

# extract_image_path PAYLOAD -> echoes a resolved, existing image path or nothing.
# Two phases mirror the live images.sh: explicit tool_input paths, then a scan of
# tool_response strings for an embedded path (delegated to the core scanner).
extract_image_path() {
	local payload="$1" cwd p candidate
	cwd="$(jq -r '.cwd // empty' <<<"$payload" 2>/dev/null)"

	# Fast-bail before jq unless the raw payload mentions an image extension.
	shopt -s nocasematch
	if [[ ! $payload =~ \.(png|jpe?g|gif|webp|bmp) ]]; then
		shopt -u nocasematch
		return 0
	fi
	shopt -u nocasematch

	resolve() { # $1 path -> resolved against cwd if relative
		local q="$1"
		[[ $q != /* && -n $cwd ]] && q="$cwd/$q"
		printf '%s' "$q"
	}
	is_ext() { [[ ${1,,} =~ \.(png|jpe?g|gif|webp|bmp)$ ]]; }

	# Phase 1: explicit tool_input paths.
	for p in \
		"$(jq -r '.tool_input.file_path // empty' <<<"$payload" 2>/dev/null)" \
		"$(jq -r '.tool_input.path // empty' <<<"$payload" 2>/dev/null)" \
		"$(jq -r '.tool_input.output_path // empty' <<<"$payload" 2>/dev/null)"; do
		[[ -n $p ]] || continue
		candidate="$(resolve "$p")"
		is_ext "$candidate" || continue
		[[ -f $candidate ]] || continue
		printf '%s' "$candidate"
		return 0
	done

	# Phase 2: scan tool_response strings for an embedded path.
	scan_response_image_path "$payload"
	return 0
}

# extract_d2_path PAYLOAD -> echoes a resolved, existing .d2 path or nothing.
extract_d2_path() {
	local payload="$1" cwd candidate
	cwd="$(jq -r '.cwd // empty' <<<"$payload" 2>/dev/null)"
	candidate="$(jq -r '.tool_input.file_path // empty' <<<"$payload" 2>/dev/null)"
	[[ -n $candidate ]] || return 0
	[[ $candidate != /* && -n $cwd ]] && candidate="$cwd/$candidate"
	[[ ${candidate,,} == *.d2 ]] || return 0
	[[ -f $candidate ]] || return 0
	printf '%s' "$candidate"
	return 0
}
