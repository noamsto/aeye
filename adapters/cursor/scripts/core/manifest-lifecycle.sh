#!/usr/bin/env bash
# Agent-agnostic manifest lifecycle shared by every adapter's image/diagram
# hooks and resume backfill: state-dir resolution, pane-file keying, owner
# self-heal, manifest append lines, and the shared-dir GC sweep. Session id is
# always a parameter (never read from the environment) so a second adapter can
# pass its own payload-derived id instead of $CLAUDE_CODE_SESSION_ID.

# shellcheck source=manifest-extract.sh disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/manifest-extract.sh"

# resolve_state_dirs -> sets STATE_DIR/IMAGES_DIR/DIAGRAMS_DIR from the shared
# env knobs (AEYE_DIR / CLAUDE_STATUS_DIR), defaulting to /tmp/claude-status.
# shellcheck disable=SC2034 # DIAGRAMS_DIR: consumed by callers, not this file
resolve_state_dirs() {
	STATE_DIR="${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
	IMAGES_DIR="$STATE_DIR/images"
	DIAGRAMS_DIR="$IMAGES_DIR/diagrams"
}

# valid_pane_file PANE -> true when PANE is safe to use as a filename. Guards
# against path traversal: the key becomes a filename; reject anything with
# path separators or outside a safe set (panes are %<int>, sessions are ids).
valid_pane_file() {
	[[ $1 =~ ^[A-Za-z0-9_@:.-]+$ ]]
}

# manifest_paths PANE_FILE -> sets MANIFEST/OWNER_FILE/LOCK_FILE, the three
# per-pane sidecar paths under IMAGES_DIR (resolve_state_dirs must run first).
# shellcheck disable=SC2034 # LOCK_FILE: consumed by callers, not this file
manifest_paths() {
	MANIFEST="$IMAGES_DIR/$1.jsonl"
	OWNER_FILE="$IMAGES_DIR/$1.owner"
	LOCK_FILE="$IMAGES_DIR/$1.lock"
}

# owner_selfheal PANE_FILE SESSION_ID -> self-heal against tmux pane-id reuse:
# a manifest last written by a different session belongs to a pane that's
# since been recycled — drop it so this session's carousel never blends in a
# prior session's images, then stamp ownership. No-op without a session id.
owner_selfheal() {
	local pane_file="$1" session_id="$2"
	manifest_paths "$pane_file"
	if [[ -n $session_id ]]; then
		if [[ -f $OWNER_FILE && $(<"$OWNER_FILE") != "$session_id" ]]; then
			rm -f "$MANIFEST"
		fi
		printf '%s' "$session_id" >"$OWNER_FILE"
	fi
}

# append_image_line MANIFEST PATH SOURCE TS -> append one image record.
append_image_line() {
	local manifest="$1" path="$2" source="$3" ts="$4" mtime
	mtime="$(_mtime "$path")"
	jq -nc --arg path "$path" --arg source "$source" --arg ts "$ts" --argjson mtime "$mtime" \
		'{type:"image", path:$path, source:$source, ts:$ts, mtime:$mtime}' >>"$manifest"
}

# append_diagram_line MANIFEST PNG SVG NAME TS -> append one diagram record.
append_diagram_line() {
	local manifest="$1" png="$2" svg="$3" name="$4" ts="$5" mtime
	mtime="$(_mtime "$png")"
	jq -nc --arg path "$png" --arg vector "$svg" --arg source "d2" --arg name "$name" --arg ts "$ts" --argjson mtime "$mtime" \
		'{type:"image", path:$path, vector:$vector, source:$source, name:$name, ts:$ts, mtime:$mtime}' >>"$manifest"
}

# gc_sweep PANE_FILE LIVE_PANES -> sweep manifests (and their orphaned owner
# sidecars) for tmux panes not in LIVE_PANES, and session-keyed files past a
# TTL, so the shared IMAGES_DIR never grows without bound. LIVE_PANES is the
# caller's tmux probe (empty when not in tmux or tmux is unavailable). Never
# GCs PANE_FILE itself (the caller just stamped it).
gc_sweep() {
	local pane_file="$1" live="$2"
	local ttl=$((7 * 86400))
	local now
	printf -v now '%(%s)T' -1

	# Sweep the manifest and its sidecars: an orphaned .owner/.lock (its .jsonl
	# already gone) would otherwise never be reaped, since a clear only fires on
	# a base the loop visits. Dedup the bases so a base with several files is
	# handled once.
	local -A gc_seen=()
	local m base j o mtime
	for m in "$IMAGES_DIR"/*.jsonl "$IMAGES_DIR"/*.owner "$IMAGES_DIR"/*.lock; do
		[[ -e $m ]] || continue
		base="$(basename "$m")"
		base="${base%.jsonl}"
		base="${base%.owner}"
		base="${base%.lock}"
		[[ $base == "$pane_file" ]] && continue # never GC the pane we just stamped
		[[ -n ${gc_seen[$base]:-} ]] && continue
		gc_seen[$base]=1
		if [[ $base =~ ^[0-9]+$ ]]; then
			# tmux pane files — GC only when we have a reliable live list and the
			# pane is not in it.
			[[ -n $live ]] || continue
			grep -qxF "$base" <<<"$live" || rm -f "$IMAGES_DIR/$base.jsonl" "$IMAGES_DIR/$base.owner" "$IMAGES_DIR/$base.lock"
		else
			# session-keyed files — prune if untouched past the TTL, aging off the
			# newest of the two (either may be absent; _mtime yields 0 for a missing
			# one, so the surviving file's real mtime wins).
			j="$(_mtime "$IMAGES_DIR/$base.jsonl")"
			o="$(_mtime "$IMAGES_DIR/$base.owner")"
			mtime=$((j > o ? j : o))
			((now - mtime > ttl)) && rm -f "$IMAGES_DIR/$base.jsonl" "$IMAGES_DIR/$base.owner" "$IMAGES_DIR/$base.lock"
		fi
	done

	# The loop's last command can be a bare ((...)) that returns 1 when the
	# final file is within its TTL — force success so that doesn't leak out as
	# the caller's exit status (non-blocking; set -e already aborted on any
	# real earlier failure in this function).
	return 0
}
