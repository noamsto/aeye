#!/usr/bin/env bats
# shellcheck disable=SC2030,SC2031  # bats runs each @test in a subshell; export is intentional
bats_require_minimum_version 1.5.0

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	APP="$(dirname "$BATS_TEST_DIRNAME")/scripts/tmux-claude-images.sh"

	export TMUX="/tmp/fake-tmux-socket"
	export TMUX_PANE="%7"
	echo '{"type":"image","path":"/x.png","source":"d2"}' >"$CLAUDE_STATUS_DIR/images/7.jsonl"

	STUB_BIN="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB_BIN"
	export TMUX_LOG="$BATS_TEST_TMPDIR/tmux.log" KITTY_LOG="$BATS_TEST_TMPDIR/kitty.log"
	: >"$TMUX_LOG"
	: >"$KITTY_LOG"

	cat >"$STUB_BIN/tmux" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$TMUX_LOG"
case "$1" in
list-panes) : ;;
split-window) echo '%99' ;;
*) : ;;
esac
STUB
	cat >"$STUB_BIN/kitty" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$KITTY_LOG"
case "$*" in
"@ ls") [[ -n ${STUB_KITTY_LS:-} ]] && printf '%s' "$STUB_KITTY_LS"; exit "${STUB_KITTY_REACHABLE:-0}" ;;
"@ ls --match"*) exit "${STUB_KITTY_MATCH:-1}" ;;
*) exit 0 ;;
esac
STUB
	printf '#!/usr/bin/env bash\n:\n' >"$STUB_BIN/aeye"
	chmod +x "$STUB_BIN/tmux" "$STUB_BIN/kitty" "$STUB_BIN/aeye"
	export PATH="$STUB_BIN:$PATH"
}

@test "AEYE_HOST=kitty in tmux forces kitty mode but keeps the tmux pane KEY" {
	# shellcheck disable=SC2030
	export AEYE_HOST=kitty
	run bash "$APP" --resolve
	[ "$status" -eq 0 ]
	[ "$(printf '%s' "$output" | cut -f1)" = "kitty" ]
	[ "$(printf '%s' "$output" | cut -f2)" = "%7" ]
}

@test "AEYE_HOST unset in tmux resolves to tmux mode" {
	unset AEYE_HOST
	run bash "$APP" --resolve
	[ "$(printf '%s' "$output" | cut -f1)" = "tmux" ]
}

@test "AEYE_HOST is honored over auto-detection" {
	# shellcheck disable=SC2030,SC2031
	export AEYE_HOST=ghostty
	run bash "$APP" --resolve
	[ "$(printf '%s' "$output" | cut -f1)" = "ghostty" ]
}

@test "bare kitty (no tmux) keys the manifest by session id" {
	unset AEYE_HOST TMUX TMUX_PANE
	export KITTY_LISTEN_ON="unix:/tmp/x" CLAUDE_CODE_SESSION_ID="sess123"
	run bash "$APP" --resolve
	[ "$(printf '%s' "$output" | cut -f1)" = "kitty" ]
	[ "$(printf '%s' "$output" | cut -f2)" = "sess123" ]
}

@test "kitty launch from inside tmux opens a vsplit (no KITTY_WINDOW_ID)" {
	# shellcheck disable=SC2030,SC2031
	export AEYE_HOST=kitty
	unset KITTY_WINDOW_ID
	run bash "$APP"
	[ "$status" -eq 0 ]
	run grep -q -- "@ launch" "$KITTY_LOG"
	[ "$status" -eq 0 ]
	run grep -q -- "--location=vsplit" "$KITTY_LOG"
	[ "$status" -eq 0 ]
}

@test "kitty launch resolves a live KITTY_WINDOW_ID to its host tab id" {
	export AEYE_HOST=kitty
	export KITTY_WINDOW_ID=21
	export STUB_KITTY_LS='[{"tabs":[{"id":3,"windows":[{"id":21}]}]}]'
	run bash "$APP"
	[ "$status" -eq 0 ]
	grep -q -- "--match id:3" "$KITTY_LOG"
	grep -q -- "--next-to id:21" "$KITTY_LOG"
}

@test "kitty launch survives a stale KITTY_WINDOW_ID by falling back to the active window" {
	# The tmux server env can carry a KITTY_WINDOW_ID naming a since-closed window;
	# matching it would error and kill the toggle, so drop the match and place
	# beside the active window instead.
	export AEYE_HOST=kitty
	export KITTY_WINDOW_ID=21
	export STUB_KITTY_LS='[{"tabs":[{"id":3,"windows":[{"id":99}]}]}]' # no window 21
	run bash "$APP"
	[ "$status" -eq 0 ]
	# The launch places beside the active window: vsplit, but no --match on a stale id.
	launch="$(grep -- "@ launch" "$KITTY_LOG")"
	[[ $launch == *"--location=vsplit"* ]]
	[[ $launch != *"--match"* ]]
}

@test "kitty unreachable from tmux falls back to a tmux split" {
	# shellcheck disable=SC2030,SC2031
	export AEYE_HOST=kitty
	export STUB_KITTY_REACHABLE=1
	run bash "$APP"
	[ "$status" -eq 0 ]
	run grep -q -- "split-window" "$TMUX_LOG"
	[ "$status" -eq 0 ]
	run grep -q -- "@ launch" "$KITTY_LOG"
	[ "$status" -ne 0 ]
}
