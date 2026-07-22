#!/usr/bin/env bash
# Open the Claude image carousel for the invoking session.
#   - Inside tmux: toggle a split pane (runnable by Claude via a Bash call;
#     also bound to prefix+I if the host tmux config provides that bind).
#   - In kitty with remote control: toggle a split window via `kitty @ launch`.
#   - In wezterm: toggle a real split via `wezterm cli split-pane`.
#   - In ghostty: toggle a separate window via `ghostty +new-window`
#     (Linux) / `open -na ghostty` (macOS).
# AEYE_HOST=<mode> overrides auto-detection. The useful case is AEYE_HOST=kitty
# from inside tmux: it opens a vsplit in the enclosing kitty window over the RC
# socket, falling back to a tmux split if that socket is unreachable.
# The manifest is keyed by $TMUX_PANE (else $CLAUDE_CODE_SESSION_ID) regardless
# of mode, so capture and viewer always agree. The carousel binary ($AEYE_BIN,
# default `aeye` on PATH) and manifest format are shared.
set -euo pipefail

STATE_DIR="${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
IMAGES_DIR="$STATE_DIR/images"
ENSURE_OPEN=""

# A terminal cell is roughly twice as tall as it is wide, so the window's pixel
# aspect ratio is cols : (CELL_ASPECT * rows). Split the longer pixel axis so
# both panes stay usable: a landscape window splits SIDE, a portrait one BOTTOM.
readonly CELL_ASPECT=2

# resolve_axis WIDTH HEIGHT -> "side" | "bottom".
# AEYE_SPLIT=side|bottom forces the axis; unset/auto/any-other-value measures.
# Empty or non-positive dims fall back to "side" (today's behavior; no regression).
resolve_axis() {
	case "${AEYE_SPLIT:-auto}" in
	side | bottom)
		printf '%s\n' "$AEYE_SPLIT"
		return
		;;
	esac
	local w=${1:-0} h=${2:-0}
	[[ $w =~ ^[0-9]+$ ]] || w=0
	[[ $h =~ ^[0-9]+$ ]] || h=0
	if ((w > 0 && h > 0 && w <= CELL_ASPECT * h)); then
		printf 'bottom\n'
	else
		printf 'side\n'
	fi
}

# resolve_target sets MODE/KEY/MANIFEST from the environment. KEY is always
# ${TMUX_PANE:-cc session id} (the capture hook's key), independent of MODE.
#   MODE=tmux       inside tmux
#   MODE=kitty      kitty remote control up (or AEYE_HOST=kitty from inside tmux)
#   MODE=wezterm    in wezterm
#   MODE=ghostty    in ghostty
#   MODE=iterm      in iTerm2 (macOS, AppleScript)
#   MODE=none       no host available
# AEYE_HOST=<mode> forces MODE, bypassing the auto-detection below.
#
# Adding a terminal:
#   1. Detect it here by a distinct env var; set MODE/KEY/MANIFEST.
#   2. Add launch_<mode>(): open-or-toggle the viewer as "$VIEWER_BIN" "$KEY".
#   3. Crisp images need the kitty graphics protocol's UNICODE PLACEHOLDERS
#      (U+10EEEE) — add the $TERM prefix to chooseGridBackend in
#      gallery_render.go. Without them the host falls back to chafa.
resolve_target() {
	# Key the manifest exactly as the capture hook (adapters/.../images.sh) does —
	# pane id inside tmux, else the Claude session id — INDEPENDENT of launch MODE.
	# That way capture and viewer always read the same file even when AEYE_HOST
	# sends a tmux user down the kitty launch path.
	KEY="${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}"
	MANIFEST="$IMAGES_DIR/${KEY#%}.jsonl"

	# AEYE_HOST forces the launcher; unset = auto-detect. Only `kitty` is useful
	# from inside tmux (it can open a split over the RC socket); other values are
	# honored but sensible only outside tmux, and degrade via launch_kitty's
	# fallback / main's mode guard.
	if [[ -n ${AEYE_HOST:-} ]]; then
		MODE="$AEYE_HOST"
		return
	fi
	if [[ -n ${TMUX:-} ]]; then
		MODE=tmux
	elif [[ -n ${KITTY_LISTEN_ON:-} ]]; then
		MODE=kitty
	elif [[ -n ${WEZTERM_PANE:-} ]]; then
		MODE=wezterm
	# TERM can be overridden in a user's shell; GHOSTTY_RESOURCES_DIR is a reliable fallback.
	elif [[ ${TERM:-} == xterm-ghostty* || -n ${GHOSTTY_RESOURCES_DIR:-} ]]; then
		MODE=ghostty
	elif [[ ${TERM_PROGRAM:-} == iTerm.app ]]; then
		MODE=iterm
	else
		MODE=none
	fi
}

launch_tmux() {
	local existing
	# -s scans the whole session, not just the active window: the viewer lives in
	# Claude's window, which may not be the one the user is currently looking at.
	existing="$(tmux list-panes -s -F '#{pane_id} #{@claude_img_src}' |
		awk -v s="$KEY" '$2 == s {print $1; exit}')"
	if [[ -n $existing ]]; then
		[[ -n $ENSURE_OPEN ]] && return # already open; ensure-open is a no-op
		tmux kill-pane -t "$existing"
		return
	fi
	# Anchor the split to Claude's pane (-t) so it lands in Claude's window even
	# if the user has switched away. -d on an automatic ensure-open so it never
	# yanks their focus; on a manual toggle the user pressed the key, so move
	# focus to the viewer.
	local detach=()
	[[ -n $ENSURE_OPEN ]] && detach=(-d)
	# Pass viewer env via tmux -e, NOT a shell `env` prefix: the split runs the
	# command through the pane's shell, and a shell that wraps `env` (e.g. fish with
	# the grc plugin) would colourise the viewer's stdout and destroy its
	# kitty-graphics / alt-screen stream. Forward the session id as AEYE_OWNER when
	# we have it (the split inherits tmux's env, not Claude's shell env) so the
	# viewer can reject a reused pane's stale manifest. printf '%q' the command so a
	# binary path with spaces survives tmux re-parsing it via the shell.
	local env_args=()
	[[ -n ${CLAUDE_CODE_SESSION_ID:-} ]] && env_args+=(-e AEYE_OWNER="$CLAUDE_CODE_SESSION_ID")
	[[ -n ${AEYE_DEBUG:-} ]] && env_args+=(-e AEYE_DEBUG="$AEYE_DEBUG")
	local cmd
	printf -v cmd '%q %q' "$VIEWER_BIN" "$KEY"
	# Split along the host window's longer axis (overridable via AEYE_SPLIT); record
	# the choice as a pane option so the viewer's `s` toggle knows the current state.
	local w h axis flag
	read -r w h < <(tmux display-message -p -t "$KEY" '#{window_width} #{window_height}' 2>/dev/null) || true
	axis="$(resolve_axis "$w" "$h")"
	[[ $axis == bottom ]] && flag=-v || flag=-h
	local viewer
	viewer="$(tmux split-window "$flag" "${detach[@]}" ${env_args[@]+"${env_args[@]}"} -t "$KEY" -P -F '#{pane_id}' "$cmd")"
	tmux set-option -p -t "$viewer" @claude_img_src "$KEY"
	tmux set-option -p -t "$viewer" @claude_img_axis "$axis"
}

# Echo (NUL-separated) the `kitty @ launch` placement args for a split beside
# the tmux-hosting window, along the host window's longer axis (resolve_axis,
# same rule as launch_tmux). Inside tmux, KITTY_WINDOW_ID is frozen at whatever
# it was when the tmux server started, so it can name a since-closed kitty window;
# `kitty @ launch --match window_id:<gone>` then errors and kills the toggle. So
# look the id up in `@ ls` first: when it resolves, pin its tab with --match (so
# --next-to, otherwise ignored across tabs, anchors the viewer beside Claude even
# when another tab is active) and read that window's own dims; when it's stale
# or unset, fall back to the focused window's dims and layout. vsplit/hsplit only
# takes effect in the splits layout, so switch the target tab to it first, else a
# stacking layout drops the viewer in the bottom row. --keep-focus so it never
# steals focus.
kitty_place_args() {
	local tab="" win_dims="" w="" h="" axis loc
	# Cache one `kitty @ ls` round trip and query it repeatedly — this is the
	# shared launch/reconcile hot path, so avoid a remote-control call per query.
	# Guarded so a failed/empty `@ ls` can't abort under set -e or feed jq empty
	# stdin; ls_json staying empty just leaves win_dims empty (side fallback).
	local ls_json
	ls_json="$(kitty @ ls 2>/dev/null || true)"
	if [[ -n $ls_json && -n ${KITTY_WINDOW_ID:-} ]]; then
		tab="$(jq -r --argjson w "$KITTY_WINDOW_ID" \
			'first(.[].tabs[] | select(any(.windows[]; .id == $w)) | .id) // empty' <<<"$ls_json" || true)"
		# .columns/.lines default to 0 (not missing) so a window object lacking
		# them still yields numeric "0 0" for resolve_axis's (( )) arithmetic,
		# instead of the literal string "null null" aborting under set -u.
		win_dims="$(jq -r --argjson w "$KITTY_WINDOW_ID" \
			'first(.[].tabs[].windows[] | select(.id == $w) | "\(.columns // 0) \(.lines // 0)") // empty' <<<"$ls_json" || true)"
	fi
	# Fall back to the focused window's geometry when the host id is stale/unset.
	if [[ -z $win_dims && -n $ls_json ]]; then
		win_dims="$(jq -r 'first(.[].tabs[] | select(.is_focused) | .windows[] | select(.is_focused) |
			"\(.columns // 0) \(.lines // 0)") // empty' <<<"$ls_json" || true)"
	fi
	read -r w h <<<"$win_dims" || true
	axis="$(resolve_axis "$w" "$h")"
	[[ $axis == bottom ]] && loc=hsplit || loc=vsplit
	if [[ -n $tab ]]; then
		kitty @ goto-layout --match "id:$tab" splits >/dev/null 2>&1 || true
		printf '%s\0' --match "id:$tab" --location="$loc" --next-to "id:$KITTY_WINDOW_ID" --keep-focus
	else
		kitty @ goto-layout splits >/dev/null 2>&1 || true
		printf '%s\0' --location="$loc" --keep-focus
	fi
}

# True iff $KEY is a pane in the active window of an attached session — i.e. the
# user is currently looking at it. Unlike reconcile's context-dependent query,
# this runs in the capturing pane's context, so it must scan all panes (-a) and
# filter explicitly rather than trust the calling pane as "current".
_key_on_screen() {
	tmux list-panes -a -F '#{pane_id} #{window_active} #{session_attached}' 2>/dev/null |
		awk -v k="$KEY" '$1==k && $2==1 && $3>=1 {f=1} END{exit !f}'
}

launch_kitty() {
	# A bare `kitty @ ls` lists windows iff the remote-control socket is reachable
	# (distinct from the toggle's `@ ls --match`, which also fails on no match).
	# Inside tmux the socket usually isn't reachable, so degrade to a tmux split
	# rather than failing; outside tmux there's nothing to fall back to.
	if ! kitty @ ls >/dev/null 2>&1; then
		if [[ -n ${TMUX:-} ]]; then
			echo "aeye: kitty remote control unreachable from tmux; using a tmux split (see README: kitty-pane mode)" >&2
			launch_tmux
			return
		fi
		echo "aeye: kitty remote control unreachable (enable allow_remote_control + listen_on)" >&2
		exit 1
	fi
	# Forward the session id as AEYE_OWNER (the launched window never saw Claude's
	# env) so the viewer can reject a reused pane's stale manifest. Empty when the
	# toggle runs without the session id (e.g. a manual prefix+I) — then the guard
	# stays inert in the viewer.
	local owner_env=()
	[[ -n ${CLAUDE_CODE_SESSION_ID:-} ]] && owner_env=(--env AEYE_OWNER="$CLAUDE_CODE_SESSION_ID")
	local debug_env=()
	[[ -n ${AEYE_DEBUG:-} ]] && debug_env=(--env AEYE_DEBUG="$AEYE_DEBUG")
	# Toggle: a viewer window is tagged with user_var claude_img_src=$KEY.
	# `kitty @ ls --match` exits non-zero when nothing matches.
	if kitty @ ls --match "var:claude_img_src=$KEY" >/dev/null 2>&1; then
		[[ -n $ENSURE_OPEN ]] && return # already open; ensure-open is a no-op
		kitty @ close-window --match "var:claude_img_src=$KEY"
		return
	fi
	# Auto-open for an off-screen pane: create it stashed so it never appears over
	# the window the user is actually looking at; reconcile reveals it on focus.
	# Deliberately bypasses kitty_place_args (no host goto-layout side effect) —
	# placement is deferred to _carousel_unstash's `goto-layout … splits`.
	if [[ -n $ENSURE_OPEN && -n ${TMUX:-} ]] && ! _key_on_screen; then
		_ensure_stash_tab
		kitty @ launch --type=window --match "var:aeye_stash=1" --keep-focus \
			--var claude_img_src="$KEY" \
			--env AEYE_DIR="$STATE_DIR" \
			--env CLAUDE_STATUS_DIR="$STATE_DIR" \
			${owner_env[@]+"${owner_env[@]}"} \
			${debug_env[@]+"${debug_env[@]}"} \
			"$VIEWER_BIN" "$KEY" >/dev/null
		return
	fi
	# Placement (vsplit beside the tmux host) is shared with the reconcile path.
	local placement=()
	mapfile -d '' placement < <(kitty_place_args)
	kitty @ launch --type=window ${placement[@]+"${placement[@]}"} --var claude_img_src="$KEY" \
		--env AEYE_DIR="$STATE_DIR" \
		--env CLAUDE_STATUS_DIR="$STATE_DIR" \
		${owner_env[@]+"${owner_env[@]}"} \
		${debug_env[@]+"${debug_env[@]}"} \
		"$VIEWER_BIN" "$KEY" >/dev/null
}

launch_wezterm() {
	# wezterm has a real mux CLI: split-pane returns the new pane id, kill-pane
	# removes it. We persist the id (keyed by session id) for the toggle.
	local panefile="$IMAGES_DIR/$KEY.wezterm-pane" pane=""
	[[ -f $panefile ]] && pane="$(<"$panefile")"
	# Liveness: `wezterm cli list` prints a PANEID column (3rd field; row 1 is the
	# header). A stale id (pane already gone) falls through to a fresh split. The
	# header guard turns a future column reorder into a loud error, not a silent
	# mismatch that would orphan panes.
	if [[ -n $pane ]] &&
		wezterm cli list 2>/dev/null |
		awk -v p="$pane" 'NR==1 && $3!="PANEID"{exit 2} NR>1 && $3==p{f=1} END{exit !f}'; then
		[[ -n $ENSURE_OPEN ]] && return # already open; ensure-open is a no-op
		wezterm cli kill-pane --pane-id "$pane" >/dev/null 2>&1 || true
		rm -f "$panefile"
		return
	fi
	# split-pane defaults its target to $WEZTERM_PANE, so it lands next to the
	# agent. env forwards the state dir — the mux server never saw our env.
	local dbg=()
	[[ -n ${AEYE_DEBUG:-} ]] && dbg=(AEYE_DEBUG="$AEYE_DEBUG")
	# Host pane size in cells: `wezterm cli list` gives .size.cols/.size.rows for
	# the $WEZTERM_PANE entry. Split its longer axis (overridable via AEYE_SPLIT).
	# // 0 defaults absent fields so resolve_axis never sees literal "null null"
	# under set -u; the read is guarded (|| true) since an empty/failed list
	# would otherwise hit set -e's read-at-EOF trap.
	local w h axis dir
	read -r w h < <(wezterm cli list --format json 2>/dev/null |
		jq -r --argjson p "${WEZTERM_PANE:-0}" \
			'first(.[] | select(.pane_id == $p) | "\(.size.cols // 0) \(.size.rows // 0)") // empty') || true
	axis="$(resolve_axis "$w" "$h")"
	[[ $axis == bottom ]] && dir=--bottom || dir=--right
	pane="$(wezterm cli split-pane "$dir" --percent 40 --cwd "$STATE_DIR" -- \
		env ${dbg[@]+"${dbg[@]}"} AEYE_DIR="$STATE_DIR" CLAUDE_STATUS_DIR="$STATE_DIR" "$VIEWER_BIN" "$KEY")"
	printf '%s\n' "$pane" >"$panefile"
}

launch_ghostty() {
	# ghostty has no window query/close IPC (+close is unshipped), so toggle on
	# the viewer process itself: it runs as `"$VIEWER_BIN" "$KEY"` and $KEY is the
	# unique CC session id, so pgrep matches exactly our viewer.
	# Known tolerated race: the short-lived `ghostty +new-window … "$VIEWER_BIN" "$KEY"`
	# launcher briefly carries the same string. It only matters on a manual toggle
	# (--ensure-open with any match is a no-op, never a kill), and the window is
	# milliseconds wide — acceptable.
	local pids
	pids="$(pgrep -f "$VIEWER_BIN $KEY" 2>/dev/null || true)"
	if [[ -n $pids ]]; then
		[[ -n $ENSURE_OPEN ]] && return # already open; ensure-open is a no-op
		# shellcheck disable=SC2086 # pgrep may return several pids; split intentionally
		kill $pids 2>/dev/null || true # viewer exit closes its ghostty window
		return
	fi
	# env forwards the state dir (D-Bus/new instance never saw our env);
	# --working-directory is explicit to dodge the 1.3.0 -e working-dir bug.
	local dbg=()
	[[ -n ${AEYE_DEBUG:-} ]] && dbg=(AEYE_DEBUG="$AEYE_DEBUG")
	local cmd=(env ${dbg[@]+"${dbg[@]}"} AEYE_DIR="$STATE_DIR" CLAUDE_STATUS_DIR="$STATE_DIR" "$VIEWER_BIN" "$KEY")
	case "$(uname -s)" in
	Darwin) open -na ghostty --args --working-directory="$STATE_DIR" -e "${cmd[@]}" ;;
	*) ghostty +new-window --working-directory="$STATE_DIR" -e "${cmd[@]}" ;;
	esac
}

# iTerm2's only stable IPC is AppleScript. We split the current session to run the
# viewer, persist the new session's unique id (keyed by cc session id), and close by
# id on toggle. pgrep-on-viewer (ghostty's trick) can kill the viewer but not reliably
# close the split pane — that depends on the profile's "When session ends" setting.
# iterm_dims -> "COLUMNS ROWS" of the current session (empty on failure).
iterm_dims() {
	osascript \
		-e 'tell application "iTerm2"' \
		-e 'tell current session of current window' \
		-e 'return (columns as text) & " " & (rows as text)' \
		-e 'end tell' \
		-e 'end tell' 2>/dev/null || true
}

# iterm_split VERB COMMAND -> new session id. VERB is "vertically" (SIDE) or
# "horizontally" (BOTTOM).
iterm_split() {
	osascript \
		-e 'on run argv' \
		-e 'tell application "iTerm2"' \
		-e 'tell current session of current window' \
		-e 'if (item 1 of argv) is "vertically" then' \
		-e 'set s to (split vertically with default profile command (item 2 of argv))' \
		-e 'else' \
		-e 'set s to (split horizontally with default profile command (item 2 of argv))' \
		-e 'end if' \
		-e 'end tell' \
		-e 'return id of s' \
		-e 'end tell' \
		-e 'end run' \
		-- "$1" "$2"
}

iterm_alive() {
	local r
	r="$(osascript \
		-e 'on run argv' \
		-e 'set targetId to item 1 of argv' \
		-e 'tell application "iTerm2"' \
		-e 'repeat with w in windows' \
		-e 'repeat with t in tabs of w' \
		-e 'repeat with s in sessions of t' \
		-e 'if (id of s) is targetId then return "1"' \
		-e 'end repeat' \
		-e 'end repeat' \
		-e 'end repeat' \
		-e 'end tell' \
		-e 'return "0"' \
		-e 'end run' \
		-- "$1" 2>/dev/null)" || return 1
	[[ $r == 1 ]]
}

iterm_close() {
	osascript \
		-e 'on run argv' \
		-e 'set targetId to item 1 of argv' \
		-e 'tell application "iTerm2"' \
		-e 'repeat with w in windows' \
		-e 'repeat with t in tabs of w' \
		-e 'repeat with s in sessions of t' \
		-e 'if (id of s) is targetId then tell s to close' \
		-e 'end repeat' \
		-e 'end repeat' \
		-e 'end repeat' \
		-e 'end tell' \
		-e 'end run' \
		-- "$1" >/dev/null 2>&1 || true
}

launch_iterm() {
	local idfile="$IMAGES_DIR/$KEY.iterm-session" session=""
	[[ -f $idfile ]] && session="$(<"$idfile")"
	if [[ -n $session ]] && iterm_alive "$session"; then
		[[ -n $ENSURE_OPEN ]] && return # already open; ensure-open is a no-op
		iterm_close "$session"
		rm -f "$idfile"
		return
	fi
	# command runs under iTerm2's app environment (never saw our env), so forward the
	# state dir explicitly — same as the wezterm/ghostty paths. printf '%q' (bash-3.2
	# safe) quotes each value so the command string survives iTerm2's shell re-parsing
	# a path with spaces (e.g. /Users/Jane Doe/...).
	local dbg_frag=""
	[[ -n ${AEYE_DEBUG:-} ]] && printf -v dbg_frag 'AEYE_DEBUG=%q ' "$AEYE_DEBUG"
	local cmd
	cmd="env ${dbg_frag}AEYE_DIR=$(printf '%q' "$STATE_DIR") CLAUDE_STATUS_DIR=$(printf '%q' "$STATE_DIR") $(printf '%q' "$VIEWER_BIN") $(printf '%q' "$KEY")"
	# Split along the current session's longer axis (overridable via AEYE_SPLIT);
	# the read is guarded (|| true) since a failed/empty iterm_dims would otherwise
	# hit set -e's read-at-EOF trap.
	local w h axis verb
	read -r w h < <(iterm_dims) || true
	axis="$(resolve_axis "$w" "$h")"
	[[ $axis == bottom ]] && verb=horizontally || verb=vertically
	session="$(iterm_split "$verb" "$cmd")"
	printf '%s\n' "$session" >"$idfile"
}

# --reconcile (driven by tmux focus hooks): in kitty-pane mode, make carousel
# windows track the visible tmux window — keep those whose pane is in the
# on-screen window beside the host, stash the rest in a hidden tab. No-op unless
# kitty mode applies — AEYE_HOST=kitty or a reachable kitty RC socket. Idempotent
# and lock-serialized, so it's safe to fire on every focus change; a tmux-split
# user pays only a fast exit.
reconcile() {
	# AEYE_HOST=kitty is the explicit opt-in, but a run-shell hook only sees it
	# when it's threaded into the tmux env; the socket fallback keeps the hook
	# working without it. The kitty @ ls guard makes either path safe — a
	# tmux-split carousel has no claude_img_src window to touch.
	[[ ${AEYE_HOST:-} == kitty || -n ${KITTY_LISTEN_ON:-} ]] || return 0
	command -v kitty >/dev/null 2>&1 || return 0
	kitty @ ls >/dev/null 2>&1 || return 0
	# Serialize overlapping hook firings; a run that can't take the lock is
	# redundant — the holder reconciles the same state.
	exec 9>"$STATE_DIR/.carousel-reconcile.lock"
	flock -n 9 2>/dev/null || return 0
	_reconcile_apply
}

# Diff carousel windows (tagged claude_img_src=<pane>) against the panes of the
# visible tmux window: in-window + stashed -> bring back; off-window + shown ->
# stash. A carousel is "stashed" when its tab holds the aeye_stash marker window.
_reconcile_apply() {
	local ls visible host_tab src in_stash touched=0
	ls="$(kitty @ ls 2>/dev/null)" || return 0
	visible="$(tmux list-panes -F '#{pane_id}' 2>/dev/null)"
	# Host tab = where the tmux host lives: the active non-stash tab, or — since a
	# prior stash may have left kitty focused on the stash tab — the first non-stash
	# tab. Don't rely on "active" alone.
	host_tab="$(jq -r '
		[ .[].tabs[] | select((.windows | map(.user_vars.aeye_stash) | any) | not) ] as $hosts
		| ($hosts[] | select(.is_active) | .id), ($hosts[0].id // empty)' <<<"$ls" | head -1)"
	while IFS=$'\t' read -r src in_stash; do
		[[ -n $src ]] || continue
		if grep -qxF "$src" <<<"$visible"; then
			if [[ $in_stash == true ]]; then
				_carousel_unstash "$src" "$host_tab"
				touched=1
			fi
		else
			if [[ $in_stash != true ]]; then
				_carousel_stash "$src"
				touched=1
			fi
		fi
	done < <(jq -r '
		.[].tabs[] as $t
		| ($t.windows | map(.user_vars.aeye_stash) | any) as $stash
		| $t.windows[] | select(.user_vars.claude_img_src)
		| [ .user_vars.claude_img_src, $stash ] | @tsv' <<<"$ls")
	# detach-window leaves the moved carousel active in the host tab, so focus the
	# tmux host window explicitly (the host-tab window with neither carousel nor
	# stash var) — focus-tab alone would land on the carousel. host_win comes from
	# the pre-move snapshot; the tmux host window never moves.
	if [[ $touched -eq 1 && -n $host_tab ]]; then
		local host_win
		host_win="$(jq -r --arg t "$host_tab" '
			.[].tabs[] | select((.id|tostring) == $t) | .windows[]
			| select((.user_vars.claude_img_src // "") == "" and (.user_vars.aeye_stash // "") == "")
			| .id' <<<"$ls" | head -1)"
		if [[ -n $host_win ]]; then
			kitty @ focus-window --match "id:$host_win" >/dev/null 2>&1 || true
		else
			kitty @ focus-tab --match "id:$host_tab" >/dev/null 2>&1 || true
		fi
	fi
	return 0
}

_carousel_stash() { # $1 = pane id (claude_img_src)
	_ensure_stash_tab
	kitty @ detach-window --match "var:claude_img_src=$1" --target-tab "var:aeye_stash=1" >/dev/null 2>&1 || true
}

# Detach back to the host tab; that tab is in the splits layout (we ensure it),
# so kitty re-splits the window beside the host automatically — no reposition.
_carousel_unstash() { # $1 = pane id   $2 = host tab id
	[[ -n $2 ]] || return 0
	kitty @ goto-layout --match "id:$2" splits >/dev/null 2>&1 || true
	kitty @ detach-window --match "var:claude_img_src=$1" --target-tab "id:$2" >/dev/null 2>&1 || true
}

# Lazily create the hidden stash tab — a parked sleeper window carries the marker
# var; --keep-focus so creating it never pulls the user away.
_ensure_stash_tab() {
	kitty @ ls 2>/dev/null |
		jq -e '.[].tabs[] | select(.windows | map(.user_vars.aeye_stash) | any)' >/dev/null 2>&1 && return 0
	kitty @ launch --type=tab --keep-focus --var aeye_stash=1 --title aeye-stash \
		sh -c 'while :; do sleep 86400; done' >/dev/null 2>&1 || true
}

main() {
	if [[ ${1:-} == --reconcile ]]; then
		reconcile
		return
	fi
	if [[ ${1:-} == --resolve-axis ]]; then # test seam: resolve axis from given dims
		resolve_axis "${2:-}" "${3:-}"
		return
	fi
	resolve_target
	[[ ${1:-} == --ensure-open ]] && ENSURE_OPEN=1
	if [[ ${1:-} == --resolve ]]; then # test seam: print resolution, no launch
		printf '%s\t%s\t%s\n' "$MODE" "${KEY:-}" "${MANIFEST:-}"
		return
	fi
	case $MODE in
	none)
		echo "image carousel needs tmux, kitty remote control, wezterm, ghostty, or iTerm2" >&2
		exit 0
		;;
	kitty | wezterm | ghostty | iterm)
		[[ -n $KEY ]] || {
			echo "no CLAUDE_CODE_SESSION_ID; cannot locate images" >&2
			exit 0
		}
		;;
	esac
	if [[ ! -s $MANIFEST ]]; then
		[[ $MODE == tmux ]] && tmux display-message "no images yet for this pane"
		exit 0
	fi
	# Resolve to an absolute path here, where our PATH includes the binary (the nix
	# wrapper puts it there). kitty/tmux launch the viewer in the *server's*
	# environment, which never saw our PATH — a bare name wouldn't be found there.
	VIEWER_BIN="$(command -v "${AEYE_BIN:-aeye}" 2>/dev/null || true)"
	if [[ -z $VIEWER_BIN ]]; then
		echo "aeye not found: set AEYE_BIN or put '${AEYE_BIN:-aeye}' on PATH" >&2
		exit 1
	fi
	"launch_$MODE"
}

main "$@"
