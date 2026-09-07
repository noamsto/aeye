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

# extract_d2_path PAYLOAD [SRC_DIR] -> echoes a resolved, existing .d2 path or
# nothing. Three phases: an explicit tool_input.file_path, a scan of
# tool_input.command — a .d2 written by a heredoc, `sed -i`, or a generator
# script has a path only there (#200) — and, given SRC_DIR, the newest .d2 just
# written there when the command's path was built from a shell variable.
# No cwd-containment guard, unlike scan_response_image_path (#139): .d2 sources
# live in the state dir, outside the project, so containment would reject every
# diagram. Consequence: `cat flow.d2` re-renders it too, which d2_render makes
# cheap by skipping a render whose png already exists. A path containing a space
# is not matched.
extract_d2_path() {
	local payload="$1" src_dir="${2:-}" cwd candidate cmd tok saw_var=0

	# Fast-bail before jq unless the raw payload mentions a .d2 at all.
	shopt -s nocasematch
	if [[ ! $payload =~ \.d2 ]]; then
		shopt -u nocasematch
		return 0
	fi
	shopt -u nocasematch

	cwd="$(jq -r '.cwd // empty' <<<"$payload" 2>/dev/null)"

	resolve() { # $1 path -> resolved against cwd if relative
		local q="$1"
		[[ $q != /* && -n $cwd ]] && q="$cwd/$q"
		printf '%s' "$q"
	}

	# Phase 1: an explicit Write/Edit file_path.
	candidate="$(jq -r '.tool_input.file_path // empty' <<<"$payload" 2>/dev/null)"
	if [[ -n $candidate ]]; then
		candidate="$(resolve "$candidate")"
		if [[ ${candidate,,} == *.d2 && -f $candidate ]]; then
			printf '%s' "$candidate"
			return 0
		fi
	fi

	# Phase 2: a .d2 token inside a shell command. Quotes and shell operators
	# delimit the token, so `cat > "$dir/flow.d2" <<EOF` yields the bare path.
	cmd="$(jq -r '.tool_input.command // empty' <<<"$payload" 2>/dev/null)"
	[[ -n $cmd ]] || return 0
	while IFS= read -r tok; do
		[[ $tok == *'$'* ]] && saw_var=1
		tok="$(resolve "$tok")"
		[[ -f $tok ]] || continue
		printf '%s' "$tok"
		return 0
	done < <(grep -oiE $'[^[:space:]\'"<>|;&()]+\\.d2' <<<"$cmd")

	# Phase 3: the token held an unexpanded $var — `cat > "$SRC_DIR/flow.d2"` —
	# which no amount of resolving turns into a path, since the variable lived in
	# the agent's shell, not this hook's. The write just happened, so take the
	# newest .d2 in SRC_DIR, and only one written in the seconds around this call:
	# a command that merely mentions a $var and a .d2 must not adopt a diagram
	# another session wrote (#139's lesson, applied to the source dir).
	((saw_var)) && [[ -n $src_dir ]] || return 0
	local now newest='' newest_mt=0 f mt
	printf -v now '%(%s)T' -1
	for f in "$src_dir"/*.d2; do
		[[ -f $f ]] || continue
		mt="$(_mtime "$f")"
		((mt >= now - 15 && mt > newest_mt)) || continue
		newest="$f" newest_mt="$mt"
	done
	[[ -n $newest ]] && printf '%s' "$newest"
	return 0
}
