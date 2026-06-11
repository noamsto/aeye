#!/usr/bin/env bash
# Open the Claude image carousel for the invoking session.
#   - Inside tmux: toggle a split pane (runnable by Claude via a Bash call;
#     also bound to prefix+I if the host tmux config provides that bind).
#     Keyed by $TMUX_PANE.
#   - Outside tmux, in kitty with remote control: toggle a split window via
#     `kitty @ launch`. Keyed by $CLAUDE_CODE_SESSION_ID.
# The carousel binary ($AGENT_CAROUSEL_BIN, default `agent-carousel` on PATH)
# and manifest format are shared.
set -euo pipefail

STATE_DIR="${AGENT_CAROUSEL_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
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
	existing="$(tmux list-panes -F '#{pane_id} #{@claude_img_src}' |
		awk -v s="$KEY" '$2 == s {print $1; exit}')"
	if [[ -n $existing ]]; then
		[[ -n $ENSURE_OPEN ]] && return # already open; ensure-open is a no-op
		tmux kill-pane -t "$existing"
		return
	fi
	local viewer
	viewer="$(tmux split-window -h -P -F '#{pane_id}' "${AGENT_CAROUSEL_BIN:-agent-carousel} '$KEY'")"
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
	kitty @ launch --type=window --var claude_img_src="$KEY" \
		--env AGENT_CAROUSEL_DIR="$STATE_DIR" \
		--env CLAUDE_STATUS_DIR="$STATE_DIR" \
		"${AGENT_CAROUSEL_BIN:-agent-carousel}" "$KEY" >/dev/null
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
	"launch_$MODE"
}

main "$@"
