#!/usr/bin/env bash
# Open the Claude image carousel for the invoking session.
#   - Inside tmux: toggle a split pane (runnable by Claude via a Bash call;
#     also bound to prefix+I if the host tmux config provides that bind).
#     Keyed by $TMUX_PANE.
#   - Outside tmux, in kitty with remote control: toggle a split window via
#     `kitty @ launch`. Keyed by $CLAUDE_CODE_SESSION_ID.
#   - Outside tmux, in wezterm: toggle a real split via `wezterm cli split-pane`.
#   - Outside tmux, in ghostty: toggle a separate window via `ghostty +new-window`
#     (Linux) / `open -na ghostty` (macOS). Keyed by $CLAUDE_CODE_SESSION_ID.
# The carousel binary ($AEYE_BIN, default `aeye` on PATH)
# and manifest format are shared.
set -euo pipefail

STATE_DIR="${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
IMAGES_DIR="$STATE_DIR/images"
ENSURE_OPEN=""

# resolve_target sets MODE/KEY/MANIFEST from the environment.
#   MODE=tmux    + KEY=<pane id>        inside tmux
#   MODE=kitty   + KEY=<cc session id>  outside tmux, kitty remote control up
#   MODE=wezterm + KEY=<cc session id>  outside tmux, in wezterm
#   MODE=ghostty + KEY=<cc session id>  outside tmux, in ghostty
#   MODE=iterm   + KEY=<cc session id>  outside tmux, in iTerm2 (macOS, AppleScript)
#   MODE=none                           no host available
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
	local viewer
	viewer="$(tmux split-window -h "${detach[@]}" -t "$KEY" -P -F '#{pane_id}' "$VIEWER_BIN '$KEY'")"
	tmux set-option -p -t "$viewer" @claude_img_src "$KEY"
}

launch_kitty() {
	# Toggle: a viewer window is tagged with user_var claude_img_src=$KEY.
	# `kitty @ ls --match` exits non-zero when nothing matches.
	if kitty @ ls --match "var:claude_img_src=$KEY" >/dev/null 2>&1; then
		[[ -n $ENSURE_OPEN ]] && return # already open; ensure-open is a no-op
		kitty @ close-window --match "var:claude_img_src=$KEY"
		return
	fi
	# Anchor to Claude's kitty window (its id is in our inherited env) so the
	# viewer opens in Claude's tab even if the user switched away, and not the
	# active one. --match selects that tab as the launch target (a remote-control
	# --location/--next-to is ignored across tabs without it); --location=vsplit
	# opens the split to the right of Claude (mirroring the tmux split-window -h
	# path); --next-to anchors it beside Claude; --keep-focus so opening it never
	# steals focus. vsplit only takes effect in the splits layout, so switch
	# Claude's tab to it first — otherwise a stacking layout (e.g. fat) drops the
	# viewer in the bottom row. Verified over the live RC socket: the window lands
	# in the target's tab to the right and leaves focus where it was.
	local placement=()
	if [[ -n ${KITTY_WINDOW_ID:-} ]]; then
		kitty @ goto-layout --match "window_id:$KITTY_WINDOW_ID" splits >/dev/null 2>&1 || true
		placement=(--match "window_id:$KITTY_WINDOW_ID" --location=vsplit --next-to "id:$KITTY_WINDOW_ID" --keep-focus)
	fi
	kitty @ launch --type=window ${placement[@]+"${placement[@]}"} --var claude_img_src="$KEY" \
		--env AEYE_DIR="$STATE_DIR" \
		--env CLAUDE_STATUS_DIR="$STATE_DIR" \
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
	pane="$(wezterm cli split-pane --right --percent 40 --cwd "$STATE_DIR" -- \
		env AEYE_DIR="$STATE_DIR" CLAUDE_STATUS_DIR="$STATE_DIR" "$VIEWER_BIN" "$KEY")"
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
	local cmd=(env AEYE_DIR="$STATE_DIR" CLAUDE_STATUS_DIR="$STATE_DIR" "$VIEWER_BIN" "$KEY")
	case "$(uname -s)" in
	Darwin) open -na ghostty --args --working-directory="$STATE_DIR" -e "${cmd[@]}" ;;
	*) ghostty +new-window --working-directory="$STATE_DIR" -e "${cmd[@]}" ;;
	esac
}

# iTerm2's only stable IPC is AppleScript. We split the current session to run the
# viewer, persist the new session's unique id (keyed by cc session id), and close by
# id on toggle. pgrep-on-viewer (ghostty's trick) can kill the viewer but not reliably
# close the split pane — that depends on the profile's "When session ends" setting.
iterm_split() {
	osascript \
		-e 'on run argv' \
		-e 'tell application "iTerm2"' \
		-e 'tell current session of current window' \
		-e 'set s to (split horizontally with default profile command (item 1 of argv))' \
		-e 'end tell' \
		-e 'return id of s' \
		-e 'end tell' \
		-e 'end run' \
		-- "$1"
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
	local cmd
	cmd="env AEYE_DIR=$(printf '%q' "$STATE_DIR") CLAUDE_STATUS_DIR=$(printf '%q' "$STATE_DIR") $(printf '%q' "$VIEWER_BIN") $(printf '%q' "$KEY")"
	session="$(iterm_split "$cmd")"
	printf '%s\n' "$session" >"$idfile"
}

main() {
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
