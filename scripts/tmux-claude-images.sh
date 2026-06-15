#!/usr/bin/env bash
# Open the Claude image carousel for the invoking session.
#   - Inside tmux: toggle a split pane (runnable by Claude via a Bash call;
#     also bound to prefix+I if the host tmux config provides that bind).
#     Keyed by $TMUX_PANE.
#   - Outside tmux, in kitty with remote control: toggle a split window via
#     `kitty @ launch`. Keyed by $CLAUDE_CODE_SESSION_ID.
# The carousel binary ($AEYE_BIN, default `aeye` on PATH)
# and manifest format are shared.
set -euo pipefail

STATE_DIR="${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
IMAGES_DIR="$STATE_DIR/images"
ENSURE_OPEN=""

# resolve_target sets MODE/KEY/MANIFEST from the environment.
#   MODE=tmux  + KEY=<pane id>         inside tmux
#   MODE=kitty + KEY=<cc session id>   outside tmux, kitty remote control up
#   MODE=none                          neither host available
resolve_target() {
	if [[ -n ${TMUX:-} ]]; then
		MODE=tmux
		KEY="${TMUX_PANE:-$(tmux display-message -p '#{pane_id}')}"
		MANIFEST="$IMAGES_DIR/${KEY#%}.jsonl"
	elif [[ -n ${KITTY_LISTEN_ON:-} ]]; then
		MODE=kitty
		KEY="${CLAUDE_CODE_SESSION_ID:-}"
		MANIFEST="$IMAGES_DIR/$KEY.jsonl"
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
	# if the user has switched away; -d so opening it never yanks their focus.
	local viewer
	viewer="$(tmux split-window -h -d -t "$KEY" -P -F '#{pane_id}' "$VIEWER_BIN '$KEY'")"
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

main() {
	resolve_target
	[[ ${1:-} == --ensure-open ]] && ENSURE_OPEN=1
	if [[ ${1:-} == --resolve ]]; then # test seam: print resolution, no launch
		printf '%s\t%s\t%s\n' "$MODE" "${KEY:-}" "${MANIFEST:-}"
		return
	fi
	case $MODE in
	none)
		echo "image carousel needs tmux or kitty remote control" >&2
		exit 0
		;;
	kitty)
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
