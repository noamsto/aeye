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

# d2_png_for SRC DIAGRAMS_DIR -> echoes the cache png path (hash of source content).
d2_png_for() {
	local src="$1" dir="$2" hash
	hash="$(sha256sum "$src" | cut -c1-16)"
	printf '%s/%s.png' "$dir" "$hash"
}

# _d2_render_fail DIR PNG MSG -> log a render failure and clean its partials.
# svg/err are derived from PNG so callers pass only the message.
_d2_render_fail() {
	local dir="$1" png="$2" msg="$3" base now
	base="${png%.png}"
	printf -v now '%(%FT%T%z)T' -1
	printf '%s\t%s\t%s\n' "$now" "$(basename "$base")" "$msg" \
		>>"$dir/render-errors.log"
	rm -f "$base.svg" "$base.err" "$png"
}

# d2_render SRC DIAGRAMS_DIR -> renders SRC to a cached png (if absent) and echoes
# the png path. Returns 1 (no output) when renderers are missing or rendering
# fails (failure is logged to render-errors.log). Does not append or toggle.
d2_render() {
	local src="$1" dir="$2" png svg err
	png="$(d2_png_for "$src" "$dir")"
	svg="${png%.png}.svg"
	mkdir -p "$dir"

	if [[ -f $png ]]; then
		printf '%s' "$png"
		return 0
	fi

	local d2_bin="${AEYE_D2:-d2}" resvg_bin="${AEYE_RESVG:-resvg}"
	command -v "$d2_bin" >/dev/null 2>&1 || return 1
	command -v "$resvg_bin" >/dev/null 2>&1 || return 1
	err="${png%.png}.err"

	local d2_args=(-t "${AEYE_D2_THEME:-105}")
	[[ ${AEYE_D2_SKETCH:-1} != 0 ]] && d2_args+=(--sketch)
	if ! "$d2_bin" "${d2_args[@]}" "$src" "$svg" 2>"$err"; then
		_d2_render_fail "$dir" "$png" "$(tr '\n' ' ' <"$err")"
		return 1
	fi

	# resvg can't use d2's embedded @font-face fonts; rewrite to an installed family.
	if ! bash "$(dirname "${BASH_SOURCE[0]}")/../d2-fix-fonts.sh" "$svg" 2>>"$err"; then
		_d2_render_fail "$dir" "$png" "$(tr '\n' ' ' <"$err")"
		return 1
	fi

	# Recolor labels to contrast their node's fill. Best-effort.
	local contrast_bin
	contrast_bin="$(command -v "${AEYE_BIN:-aeye}" 2>/dev/null || true)"
	if [[ -n $contrast_bin ]]; then
		"$contrast_bin" svg-contrast "$svg" 2>>"$err" || true
	fi

	local resvg_args=()
	if [[ -n ${AEYE_D2_FONT_DIR:-} ]]; then
		resvg_args+=(--skip-system-fonts --use-fonts-dir "$AEYE_D2_FONT_DIR")
	fi
	if ! "$resvg_bin" "${resvg_args[@]}" "$svg" "$png" 2>>"$err"; then
		_d2_render_fail "$dir" "$png" "$(tr '\n' ' ' <"$err")"
		return 1
	fi
	rm -f "$err"
	printf '%s' "$png"
	return 0
}
