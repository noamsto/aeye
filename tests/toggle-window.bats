#!/usr/bin/env bats
# shellcheck disable=SC2030,SC2031  # bats wraps each @test in a subshell; export is intentional

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	# Clean slate: no host signals unless a test opts in.
	unset TMUX KITTY_LISTEN_ON WEZTERM_PANE GHOSTTY_RESOURCES_DIR TERM
	export CLAUDE_CODE_SESSION_ID="sess-123"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	# Non-empty manifest so launch tests get past the "no images yet" guard.
	echo '{"type":"image","path":"/x.png","source":"d2"}' \
		>"$CLAUDE_STATUS_DIR/images/$CLAUDE_CODE_SESSION_ID.jsonl"
	APP="$(dirname "$BATS_TEST_DIRNAME")/scripts/tmux-claude-images.sh"

	STUB_BIN="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB_BIN"
	# Viewer stub so VIEWER_BIN resolves on PATH.
	printf '#!/usr/bin/env bash\n:\n' >"$STUB_BIN/aeye"
	chmod +x "$STUB_BIN/aeye"
	# wezterm stub: logs args; `cli list` reports a live pane only when
	# $STUB_PANE_ALIVE is set; `cli split-pane` echoes the new pane id.
	export WEZTERM_LOG="$BATS_TEST_TMPDIR/wezterm.log"
	: >"$WEZTERM_LOG"
	cat >"$STUB_BIN/wezterm" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$WEZTERM_LOG"
case "$2" in
list) [[ -n ${STUB_PANE_ALIVE:-} ]] && printf 'WINID TABID PANEID\n0 0 %s\n' "$STUB_PANE_ALIVE" || printf 'WINID TABID PANEID\n' ;;
split-pane) echo "${STUB_NEW_PANE:-42}" ;;
*) : ;;
esac
STUB
	chmod +x "$STUB_BIN/wezterm"
	export PATH="$STUB_BIN:$PATH"
}

@test "resolve: wezterm when WEZTERM_PANE set" {
	export WEZTERM_PANE=3
	run bash "$APP" --resolve
	[ "$status" -eq 0 ]
	[ "$(echo "$output" | cut -f1)" = wezterm ]
	[ "$(echo "$output" | cut -f3)" = "$CLAUDE_STATUS_DIR/images/sess-123.jsonl" ]
}

@test "resolve: ghostty when TERM=xterm-ghostty" {
	export TERM=xterm-ghostty
	run bash "$APP" --resolve
	[ "$status" -eq 0 ]
	[ "$(echo "$output" | cut -f1)" = ghostty ]
}

@test "resolve: ghostty when GHOSTTY_RESOURCES_DIR set (TERM not ghostty)" {
	export GHOSTTY_RESOURCES_DIR=/usr/share/ghostty
	run bash "$APP" --resolve
	[ "$status" -eq 0 ]
	[ "$(echo "$output" | cut -f1)" = ghostty ]
}

@test "resolve: none when no host present" {
	run bash "$APP" --resolve
	[ "$status" -eq 0 ]
	[ "$(echo "$output" | cut -f1)" = none ]
}

@test "wezterm: split-pane opens a viewer and records the pane id" {
	export WEZTERM_PANE=3
	unset STUB_PANE_ALIVE
	export STUB_NEW_PANE=77
	run bash "$APP"
	[ "$status" -eq 0 ]
	grep -q split-pane "$WEZTERM_LOG"
	[ "$(cat "$CLAUDE_STATUS_DIR/images/$CLAUDE_CODE_SESSION_ID.wezterm-pane")" = 77 ]
}

@test "wezterm: bare toggle kills the live viewer pane" {
	export WEZTERM_PANE=3
	printf '42\n' >"$CLAUDE_STATUS_DIR/images/$CLAUDE_CODE_SESSION_ID.wezterm-pane"
	export STUB_PANE_ALIVE=42
	run bash "$APP"
	[ "$status" -eq 0 ]
	grep -q "kill-pane --pane-id 42" "$WEZTERM_LOG"
	[ ! -f "$CLAUDE_STATUS_DIR/images/$CLAUDE_CODE_SESSION_ID.wezterm-pane" ]
}

@test "wezterm: --ensure-open with a live pane does not split again" {
	export WEZTERM_PANE=3
	printf '42\n' >"$CLAUDE_STATUS_DIR/images/$CLAUDE_CODE_SESSION_ID.wezterm-pane"
	export STUB_PANE_ALIVE=42
	run bash "$APP" --ensure-open
	[ "$status" -eq 0 ]
	run grep -c split-pane "$WEZTERM_LOG"
	[ "$output" -eq 0 ]
}
