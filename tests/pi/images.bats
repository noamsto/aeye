#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	export PLUGIN_ROOT="$ROOT/adapters/pi"
	APP="$PLUGIN_ROOT/scripts/images.sh"
	FIXTURES="$ROOT/tests/fixtures/codex"

	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	export TMUX="fake,4242,0" # pane ids are per server; pin one so the key is stable
	MANIFEST="$AEYE_DIR/images/4242-7.jsonl"
}

run_app() { # $1 = JSON payload
	printf '%s' "$1" | bash "$APP"
}

@test "read of an image appends one manifest line" {
	IMG="$FIXTURES/bar.png"
	payload="$(jq -nc --arg p "$IMG" '{tool_name:"read",tool_input:{path:$p},tool_response:{content:[""]},cwd:"/repo",session_id:"sess-A"}')"
	run_app "$payload"
	[ -f "$MANIFEST" ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	run jq -r '.path' "$MANIFEST"
	[ "$output" = "$IMG" ]
	run jq -r '.source' "$MANIFEST"
	[ "$output" = "read" ]
}

@test "write of an image is recorded" {
	IMG="$FIXTURES/shot.png"
	payload="$(jq -nc --arg p "$IMG" '{tool_name:"write",tool_input:{file_path:$p},tool_response:{content:[""]},cwd:"/repo",session_id:"sess-A"}')"
	run_app "$payload"
	run jq -r '.source' "$MANIFEST"
	[ "$output" = "write" ]
}

@test "read of a generated d2 theme variant appends nothing" {
	generated="$AEYE_DIR/images/diagrams/0123456789abcdef-light.png"
	mkdir -p "$(dirname "$generated")"
	printf 'x' >"$generated"
	payload="$(jq -nc --arg p "$generated" '{tool_name:"read",tool_input:{path:$p},tool_response:{content:[""]},cwd:"/repo",session_id:"sess-A"}')"
	run_app "$payload"
	[ ! -f "$MANIFEST" ]
}

@test "screenshot path is extracted from bash tool_response" {
	IMG="$FIXTURES/screenshot.png"
	payload="$(jq -nc --arg p "$IMG" --arg c "$(dirname "$IMG")" \
		'{tool_name:"bash",tool_input:{command:"ls"},tool_response:{content:[("saved to "+$p)]},cwd:$c,session_id:"sess-A"}')"
	run_app "$payload"
	[ -f "$MANIFEST" ]
	run jq -r '.path' "$MANIFEST"
	[ "$output" = "$IMG" ]
}

@test "a .d2 write appends nothing (that's diagrams.sh's job)" {
	D2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$D2"
	payload="$(jq -nc --arg p "$D2" '{tool_name:"write",tool_input:{file_path:$p},tool_response:{content:[""]},cwd:"/repo",session_id:"sess-A"}')"
	run_app "$payload"
	[ ! -f "$MANIFEST" ]
}

@test "non-image is ignored (no manifest)" {
	payload="$(jq -nc '{tool_name:"read",tool_input:{path:"/repo/main.go"},tool_response:{content:["text"]},cwd:"/repo",session_id:"sess-A"}')"
	run_app "$payload"
	[ ! -f "$MANIFEST" ]
}

@test "missing file is ignored" {
	payload="$(jq -nc '{tool_name:"read",tool_input:{path:"/nope/missing.png"},tool_response:{content:[""]},cwd:"/repo",session_id:"sess-A"}')"
	run_app "$payload"
	[ ! -f "$MANIFEST" ]
}

@test "append-only: same image twice -> two lines (viewer dedups on read)" {
	IMG="$FIXTURES/bar.png"
	payload="$(jq -nc --arg p "$IMG" '{tool_name:"read",tool_input:{path:$p},tool_response:{content:[""]},cwd:"/repo",session_id:"sess-A"}')"
	run_app "$payload"
	run_app "$payload"
	run wc -l <"$MANIFEST"
	[ "$output" -eq 2 ]
}

@test "keyed by TMUX_PANE, not the pi session id" {
	IMG="$FIXTURES/bar.png"
	payload="$(jq -nc --arg p "$IMG" '{tool_name:"read",tool_input:{path:$p},tool_response:{content:[""]},cwd:"/repo",session_id:"sess-abc"}')"
	run_app "$payload"
	[ -f "$MANIFEST" ]
	[ ! -f "$AEYE_DIR/images/sess-abc.jsonl" ]
}

@test "outside tmux falls back to the pi session id key" {
	unset TMUX_PANE
	IMG="$FIXTURES/bar.png"
	payload="$(jq -nc --arg p "$IMG" '{tool_name:"read",tool_input:{path:$p},tool_response:{content:[""]},cwd:"/repo",session_id:"sess-abc"}')"
	run_app "$payload"
	sess_manifest="$AEYE_DIR/images/sess-abc.jsonl"
	[ -f "$sess_manifest" ]
	run jq -r '.path' "$sess_manifest"
	[ "$output" = "$IMG" ]
}

@test "no TMUX_PANE and no session id -> no-op, exit 0" {
	unset TMUX_PANE
	run run_app '{"tool_name":"read","tool_input":{"path":"/x.png"}}'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}

@test "AEYE_DIR takes precedence over CLAUDE_STATUS_DIR" {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/other"
	export AEYE_DIR="$BATS_TEST_TMPDIR/carousel"
	IMG="$FIXTURES/bar.png"
	payload="$(jq -nc --arg p "$IMG" '{tool_name:"read",tool_input:{path:$p},tool_response:{content:[""]},cwd:"/repo",session_id:"sess-A"}')"
	run_app "$payload"
	[ -f "$AEYE_DIR/images/4242-7.jsonl" ]
	[ ! -f "$CLAUDE_STATUS_DIR/images/4242-7.jsonl" ]
}
