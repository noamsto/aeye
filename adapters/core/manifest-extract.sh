#!/usr/bin/env bash
# Agent-agnostic path-extraction + d2-render helpers shared by every adapter's
# image/diagram hooks and resume backfill. Pure: no manifest writes, no keying,
# no toggle. Each function echoes a result (or nothing) and returns 0 so
# callers under `set -euo pipefail` are never aborted by a "not found" outcome.

# _mtime PATH -> file mtime in epoch seconds (0 if absent/unstatable). Portable
# across GNU stat (-c) and BSD/macOS stat (-f): the carousel keys dedup and the
# raster cache on this value, and session-reset ages files out by it.
_mtime() {
	stat -c %Y "$1" 2>/dev/null || stat -f %m "$1" 2>/dev/null || echo 0
}

# _manifest_lock LOCKPATH -> take an exclusive advisory lock on fd 9, serializing
# the concurrent hook mutations of one pane's manifest (images.sh appends,
# diagrams.sh's read-modify-write prune, session-backfill's rebuild,
# session-reset's clear). Without it, diagrams.sh's tmp+mv rewrite can clobber a
# concurrent images.sh append (lost update) and a resume-time append can race the
# backfill rebuild. Released when the script exits (fd 9 closes). Best-effort:
# without flock on PATH the prior lock-free behavior stands.
_manifest_lock() {
	command -v flock >/dev/null 2>&1 || return 0
	exec 9>"$1" || return 0
	flock 9 2>/dev/null || true
}

# scan_response_image_path PAYLOAD -> echoes a resolved, existing image path or
# nothing. Scans tool_response strings for an embedded path — the agent-agnostic
# Phase-2 half of Claude's extract_image_path, carved out so a second adapter's
# extract_image_path can delegate to it after its own Phase-1 (tool-input) check.
# The path must resolve under cwd: Phase 2 catches a tool that *saved* an image
# into the session's project, so a path outside cwd is an incidental mention in
# shell output (e.g. an `ls` of another repo) and never enters the carousel (#139).
scan_response_image_path() {
	local payload="$1" cwd response_path
	cwd="$(jq -r '.cwd // empty' <<<"$payload" 2>/dev/null)"

	resolve() { # $1 path -> resolved against cwd if relative
		local q="$1"
		[[ $q != /* && -n $cwd ]] && q="$cwd/$q"
		printf '%s' "$q"
	}
	is_ext() { [[ ${1,,} =~ \.(png|jpe?g|gif|webp|bmp)$ ]]; }

	response_path="$(jq -r '
    [.tool_response | .. | strings
      | select(length < 4096)
      | capture("(?<p>(?:/|\\./)[^\\s]*\\.(?:png|jpe?g|gif|webp|bmp))"; "i")
      | .p
    ] | first // empty
  ' <<<"$payload" 2>/dev/null)"
	if [[ -n $response_path ]]; then
		response_path="$(resolve "$response_path")"
		if is_ext "$response_path" && [[ -n $cwd && $response_path == "$cwd"/* ]] && [[ -f $response_path ]]; then
			printf '%s' "$response_path"
		fi
	fi
	return 0
}

# d2_png_for SRC DIAGRAMS_DIR THEME -> echoes the cache png path for one theme
# variant (<hash>-<theme>.png). The hash is of the source content only — the
# palette/theme is applied by aeye at render time, so both variants share a hash
# and differ only by the suffix the carousel swaps to match the live theme.
d2_png_for() {
	local src="$1" dir="$2" theme="$3" hash
	hash="$(sha256sum "$src" | cut -c1-16)"
	printf '%s/%s-%s.png' "$dir" "$hash" "$theme"
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

# d2_rm_render_set DARK_PNG -> remove both theme variants (png/svg/err) of one
# render. The manifest records the dark png; the light sibling and the .err files
# share the <hash> prefix, so strip the -dark.png suffix and sweep the lot.
d2_rm_render_set() {
	local base="${1%-dark.png}"
	rm -f "$base-dark.png" "$base-dark.svg" "$base-dark.err" \
		"$base-light.png" "$base-light.svg" "$base-light.err"
}

# d2_render SRC DIAGRAMS_DIR -> renders both theme variants of SRC (each only if
# absent) and echoes the canonical (dark) png path for the manifest; the carousel
# swaps the suffix to the live theme at view time. Returns 1 (no output) when the
# aeye binary is missing or any render fails (failure logged to render-errors.log).
d2_render() {
	local src="$1" dir="$2" theme id png err
	mkdir -p "$dir"

	local aeye_bin="${AEYE_BIN:-aeye}"
	command -v "$aeye_bin" >/dev/null 2>&1 || return 1

	# aeye render-diagram does the whole pipeline in-process (embedded d2 compile
	# -> font rewrite -> label contrast -> resvg), writing $png and its sibling
	# .svg. theme via AEYE_D2_THEME (105 light / 200 dark); resvg from PATH/AEYE_RESVG.
	for theme in light dark; do
		case $theme in
		light) id=105 ;;
		dark) id=200 ;;
		esac
		png="$(d2_png_for "$src" "$dir" "$theme")"
		[[ -f $png ]] && continue
		err="${png%.png}.err"
		if ! AEYE_D2_THEME="$id" "$aeye_bin" render-diagram "$src" "$png" 2>"$err"; then
			_d2_render_fail "$dir" "$png" "$(tr '\n' ' ' <"$err")"
			return 1
		fi
		rm -f "$err"
	done

	printf '%s' "$(d2_png_for "$src" "$dir" dark)"
	return 0
}
