#!/usr/bin/env bats

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX="/tmp/fake-tmux-socket"
	export TMUX_PANE="%7"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	# Non-empty manifest so the script proceeds past its "no images yet" guard.
	echo '{"type":"image","path":"/x.png","source":"d2"}' >"$CLAUDE_STATUS_DIR/images/7.jsonl"
	APP="$(dirname "$BATS_TEST_DIRNAME")/scripts/tmux-claude-images.sh"

	# tmux stub: logs every call; list-panes output is controlled by $STUB_EXISTING.
	STUB_BIN="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB_BIN"
	cat >"$STUB_BIN/tmux" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$TMUX_LOG"
case "$1" in
list-panes) [[ -n ${STUB_EXISTING:-} ]] && printf '%%9 %s\n' "$TMUX_PANE" || true ;;
split-window) echo '%99' ;;
*) : ;;
esac
STUB
	chmod +x "$STUB_BIN/tmux"
	export PATH="$STUB_BIN:$PATH"
	export TMUX_LOG="$BATS_TEST_TMPDIR/tmux.log"
	: >"$TMUX_LOG"
}

@test "--ensure-open with an open viewer does NOT kill it" {
	# shellcheck disable=SC2030
	export STUB_EXISTING=1
	run bash "$APP" --ensure-open
	[ "$status" -eq 0 ]
	run grep -c kill-pane "$TMUX_LOG"
	[ "$output" -eq 0 ]
}

@test "--ensure-open with no viewer opens one" {
	unset STUB_EXISTING
	run bash "$APP" --ensure-open
	[ "$status" -eq 0 ]
	run grep -c split-window "$TMUX_LOG"
	[ "$output" -ge 1 ]
}

@test "bare toggle still kills an open viewer" {
	# shellcheck disable=SC2031
	export STUB_EXISTING=1
	run bash "$APP"
	[ "$status" -eq 0 ]
	run grep -c kill-pane "$TMUX_LOG"
	[ "$output" -ge 1 ]
}
