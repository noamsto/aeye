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

# tmux_server_id -> the current tmux server's pid, or empty outside tmux. $TMUX is
# "<socket>,<server pid>,<session>", so this costs no tmux call.
tmux_server_id() {
	local pid
	IFS=, read -r _ pid _ <<<"${TMUX:-}"
	[[ $pid =~ ^[0-9]+$ ]] && printf '%s' "$pid"
	return 0
}

# resolve_pane_key SESSION_ID -> the manifest key every hook and the viewer share.
# Pane ids are per tmux server (each one numbers its panes from %0) while
# IMAGES_DIR is shared machine-wide, so the key carries the server pid:
# "<server pid>-<pane>". Falls back to the bare pane id when the server id is
# unknown, then to SESSION_ID so the carousel still works in a bare terminal.
resolve_pane_key() {
	local pane="${TMUX_PANE:-}" srv
	pane="${pane#%}"
	if [[ -n $pane ]]; then
		srv="$(tmux_server_id)"
		[[ -n $srv ]] && pane="$srv-$pane"
		printf '%s' "$pane"
		return 0
	fi
	printf '%s' "$1"
}

# manifest_paths PANE_FILE -> sets MANIFEST/OWNER_FILE/OWNERPID_FILE/LOCK_FILE,
# the per-pane sidecar paths under IMAGES_DIR (resolve_state_dirs must run first).
# shellcheck disable=SC2034 # LOCK_FILE/OWNERPID_FILE: consumed by callers, not this file
manifest_paths() {
	MANIFEST="$IMAGES_DIR/$1.jsonl"
	OWNER_FILE="$IMAGES_DIR/$1.owner"
	OWNERPID_FILE="$IMAGES_DIR/$1.ownerpid"
	LOCK_FILE="$IMAGES_DIR/$1.lock"
}

# owner_pid_self -> pid of the agent process holding this pane: the ancestor of
# this hook that is a direct child of the pane's shell. Identified by position,
# not by name, so it resolves claude, codex and cursor-agent alike — and a Nix
# wrapper, whose comm is `.claude-wrapped`, not `claude`. Empty outside tmux,
# where the manifest key falls back to the (per-session unique) session id, so
# two sessions never share a key and the guard below is unnecessary.
owner_pid_self() {
	local pane_pid p pp i
	[[ -n ${TMUX_PANE:-} ]] || return 0
	command -v tmux >/dev/null 2>&1 || return 0
	pane_pid="$(tmux display-message -p -t "$TMUX_PANE" '#{pane_pid}' 2>/dev/null)" || return 0
	[[ $pane_pid =~ ^[0-9]+$ ]] || return 0
	p=$$
	# Bounded: a cycle is impossible, but a walk that never meets pane_pid (the
	# hook was reparented) must end rather than spin.
	for ((i = 0; i < 16; i++)); do
		pp="$(ps -o ppid= -p "$p" 2>/dev/null | tr -d '[:space:]')"
		[[ $pp =~ ^[0-9]+$ ]] || return 0
		((pp <= 1)) && return 0
		if [[ $pp == "$pane_pid" ]]; then
			printf '%s' "$p"
			return 0
		fi
		p="$pp"
	done
	return 0
}

# owner_live PANE_FILE SESSION_ID -> true when the pane is held by a DIFFERENT
# session whose agent process is still running. That is a NESTED agent — one
# started from inside the owner's own session (`claude -p` in a hook or script)
# — which inherits $TMUX_PANE and so resolves the same manifest key. Its
# parent's carousel must survive it.
#
# The recorded pid is the discriminator a session id alone cannot provide: a
# reused pane and a nested agent look identical from the id, and differ only in
# whether the recorded owner is still alive. Absent pid file -> false, which is
# the pre-#234 behaviour, so a manifest written before the upgrade still clears.
owner_live() {
	local session_id="$2" pid
	manifest_paths "$1"
	[[ -f $OWNERPID_FILE && -f $OWNER_FILE ]] || return 1
	[[ $(<"$OWNER_FILE") != "$session_id" ]] || return 1
	pid="$(<"$OWNERPID_FILE")"
	[[ $pid =~ ^[0-9]+$ ]] || return 1
	kill -0 "$pid" 2>/dev/null
}

# owner_claim PANE_FILE SESSION_ID -> stamp this session as the pane's owner and
# record its agent pid, so a later nested start can tell us apart from a pane
# whose agent has exited. No-op without a session id.
owner_claim() {
	local pane_file="$1" session_id="$2" pid
	[[ -n $session_id ]] || return 0
	manifest_paths "$pane_file"
	printf '%s' "$session_id" >"$OWNER_FILE"
	pid="$(owner_pid_self)"
	[[ -n $pid ]] && printf '%s' "$pid" >"$OWNERPID_FILE"
	return 0
}

# owner_selfheal PANE_FILE SESSION_ID -> self-heal against tmux pane-id reuse:
# a manifest last written by a different session belongs to a pane that's
# since been recycled — drop it so this session's carousel never blends in a
# prior session's images, then stamp ownership. No-op without a session id.
owner_selfheal() {
	local pane_file="$1" session_id="$2"
	manifest_paths "$pane_file"
	if [[ -n $session_id ]]; then
		# A live owner is a session we are nested inside, not a recycled pane, so
		# neither its manifest nor its ownership may be taken (#234). We append our
		# own capture to its manifest: the work happened in this pane either way.
		if owner_live "$pane_file" "$session_id"; then
			return 0
		fi
		if [[ -f $OWNER_FILE && $(<"$OWNER_FILE") != "$session_id" ]]; then
			rm -f "$MANIFEST"
		fi
		owner_claim "$pane_file" "$session_id"
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

# _gc_rm BASE -> drop a key's manifest and both sidecars.
_gc_rm() {
	rm -f "$IMAGES_DIR/$1.jsonl" "$IMAGES_DIR/$1.owner" "$IMAGES_DIR/$1.ownerpid" "$IMAGES_DIR/$1.lock"
}

# _gc_expired BASE NOW TTL -> true when BASE has been untouched past TTL, aging
# off the newest of manifest/owner (either may be absent; _mtime yields 0 for a
# missing one, so the surviving file's real mtime wins).
_gc_expired() {
	local j o mtime
	j="$(_mtime "$IMAGES_DIR/$1.jsonl")"
	o="$(_mtime "$IMAGES_DIR/$1.owner")"
	mtime=$((j > o ? j : o))
	(($2 - mtime > $3))
}

# gc_sweep PANE_FILE LIVE_PANES -> sweep manifests (and their orphaned owner
# sidecars) that can no longer belong to a live session, so the shared IMAGES_DIR
# never grows without bound. LIVE_PANES is the caller's tmux probe, which only
# ever sees OUR server (empty when not in tmux or tmux is unavailable). Never GCs
# PANE_FILE itself (the caller just stamped it).
#
# A tmux key is "<server pid>-<pane>". LIVE_PANES only covers OUR server, so a
# foreign server's panes are judged by whether that pid is still alive — plus the
# TTL, since a dead server's pid can be recycled and liveness alone would then
# never bound the dir. Session-keyed files, and legacy bare-pane files written
# before the key carried a server id, have no liveness signal at all.
gc_sweep() {
	local pane_file="$1" live="$2"
	local ttl=$((7 * 86400))
	local now srv
	printf -v now '%(%s)T' -1
	srv="$(tmux_server_id)"

	# Sweep the manifest and its sidecars: an orphaned .owner/.lock (its .jsonl
	# already gone) would otherwise never be reaped, since a clear only fires on
	# a base the loop visits. Dedup the bases so a base with several files is
	# handled once.
	local -A gc_seen=()
	local m base
	for m in "$IMAGES_DIR"/*.jsonl "$IMAGES_DIR"/*.owner "$IMAGES_DIR"/*.ownerpid "$IMAGES_DIR"/*.lock; do
		[[ -e $m ]] || continue
		base="$(basename "$m")"
		base="${base%.jsonl}"
		base="${base%.owner}"
		base="${base%.ownerpid}"
		base="${base%.lock}"
		[[ $base == "$pane_file" ]] && continue # never GC the pane we just stamped
		[[ -n ${gc_seen[$base]:-} ]] && continue
		gc_seen[$base]=1
		if [[ $base =~ ^([0-9]+)-([0-9]+)$ ]]; then
			local base_srv="${BASH_REMATCH[1]}" base_pane="${BASH_REMATCH[2]}"
			if [[ -n $srv && $base_srv == "$srv" ]]; then
				# Our server — GC only when we have a reliable live list and the
				# pane is not in it.
				[[ -n $live ]] || continue
				grep -qxF "$base_pane" <<<"$live" || _gc_rm "$base"
			elif ! kill -0 "$base_srv" 2>/dev/null || _gc_expired "$base" "$now" "$ttl"; then
				_gc_rm "$base"
			fi
		elif _gc_expired "$base" "$now" "$ttl"; then
			_gc_rm "$base"
		fi
	done

	# The loop's last command can be an `if` whose every branch was false (the
	# final file is live, or within its TTL), which returns 1 — force success so
	# that doesn't leak out as the caller's exit status (non-blocking; set -e
	# already aborted on any real earlier failure in this function).
	return 0
}
