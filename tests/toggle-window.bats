#!/usr/bin/env bats

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
	export PATH="$STUB_BIN:$PATH"
}

@test "resolve: wezterm when WEZTERM_PANE set" {
	export WEZTERM_PANE=3
	run bash "$APP" --resolve
	[ "$status" -eq 0 ]
	[ "$(echo "$output" | cut -f1)" = wezterm ]
}

@test "resolve: ghostty when TERM=xterm-ghostty" {
	export TERM=xterm-ghostty
	run bash "$APP" --resolve
	[ "$status" -eq 0 ]
	[ "$(echo "$output" | cut -f1)" = ghostty ]
}

@test "resolve: none when no host present" {
	run bash "$APP" --resolve
	[ "$status" -eq 0 ]
	[ "$(echo "$output" | cut -f1)" = none ]
}
