#!/usr/bin/env bats

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	unset CLAUDE_CODE_SESSION_ID
	APP="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/scripts/session-reset.sh"
	MANIFEST="$CLAUDE_STATUS_DIR/images/7.jsonl"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	printf '{"type":"image","path":"/x.png"}\n' >"$MANIFEST"
}

@test "source=startup removes the manifest" {
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}

@test "source=clear removes the manifest" {
	run bash "$APP" <<<'{"source":"clear"}'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}

@test "source=resume keeps the manifest" {
	run bash "$APP" <<<'{"source":"resume"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
}

@test "source=compact keeps the manifest" {
	run bash "$APP" <<<'{"source":"compact"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
}

@test "no key (no pane, no session) is a clean no-op" {
	unset TMUX_PANE
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
}

@test "outside tmux: keys by session id and removes that manifest" {
	unset TMUX_PANE
	export CLAUDE_CODE_SESSION_ID="sess-abc"
	sess_manifest="$CLAUDE_STATUS_DIR/images/sess-abc.jsonl"
	printf '{"type":"image","path":"/y.png"}\n' >"$sess_manifest"
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
	[ ! -f "$sess_manifest" ]
}

@test "missing manifest -> exit 0, no error" {
	rm -f "$MANIFEST"
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
}

@test "empty payload -> clean no-op" {
	run bash "$APP" <<<''
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
}
