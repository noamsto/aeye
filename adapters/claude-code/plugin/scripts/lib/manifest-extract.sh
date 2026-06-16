#!/usr/bin/env bash
# Shared path-extraction + d2-render helpers for the image/diagram hooks and the
# resume backfill. Pure: no manifest writes, no keying, no toggle. Each function
# echoes a result (or nothing) and returns 0 so callers under `set -euo pipefail`
# are never aborted by a "not found" outcome.

# extract_image_path PAYLOAD -> echoes a resolved, existing image path or nothing.
# Two phases mirror the live images.sh: explicit tool_input paths, then a scan of
# tool_response strings for an embedded path.
extract_image_path() {
	local payload="$1" cwd p candidate response_path
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
	response_path="$(jq -r '
    [.tool_response | .. | strings
      | select(length < 4096)
      | capture("(?<p>(?:/|\\./)[^\\s]*\\.(?:png|jpe?g|gif|webp|bmp))"; "i")
      | .p
    ] | first // empty
  ' <<<"$payload" 2>/dev/null)"
	if [[ -n $response_path ]]; then
		response_path="$(resolve "$response_path")"
		if is_ext "$response_path" && [[ -f $response_path ]]; then
			printf '%s' "$response_path"
		fi
	fi
	return 0
}
