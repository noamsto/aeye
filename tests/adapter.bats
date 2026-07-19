#!/usr/bin/env bats

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	MANIFEST="$CLAUDE_STATUS_DIR/images/7.jsonl"
	IMG="$BATS_TEST_TMPDIR/pic.png"
	printf 'x' >"$IMG"
	APP="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/scripts/images.sh"
	unset CLAUDE_CODE_SESSION_ID
}

run_app() { # $1 = fixture name
	sed -e "s#IMGPATH#$IMG#g" -e "s#CWDPATH#$BATS_TEST_TMPDIR#g" "$BATS_TEST_DIRNAME/fixtures/$1" | bash "$APP"
}

@test "Read of an image appends one manifest line" {
	run_app hook-read-image.json
	[ -f "$MANIFEST" ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	run jq -r '.path' "$MANIFEST"
	[ "$output" = "$IMG" ]
	run jq -r '.source' "$MANIFEST"
	[ "$output" = "Read" ]
}

@test "Write of an image is recorded" {
	run_app hook-write-image.json
	run jq -r '.source' "$MANIFEST"
	[ "$output" = "Write" ]
}

@test "screenshot path is extracted from tool_response" {
	run_app hook-screenshot.json
	[ -f "$MANIFEST" ]
	run jq -r '.path' "$MANIFEST"
	[ "$output" = "$IMG" ]
}

@test "non-image is ignored (no manifest)" {
	run_app hook-non-image.json
	[ ! -f "$MANIFEST" ]
}

@test "missing file is ignored" {
	rm -f "$IMG"
	run_app hook-read-image.json
	[ ! -f "$MANIFEST" ]
}

@test "append-only: same image twice -> two lines (viewer dedups on read)" {
	run_app hook-read-image.json
	run_app hook-read-image.json
	run wc -l <"$MANIFEST"
	[ "$output" -eq 2 ]
}

@test "owner self-heal: a different session drops the prior manifest" {
	CLAUDE_CODE_SESSION_ID="sess-A" run_app hook-read-image.json
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	CLAUDE_CODE_SESSION_ID="sess-B" run_app hook-read-image.json
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	run cat "$CLAUDE_STATUS_DIR/images/7.owner"
	[ "$output" = "sess-B" ]
}

@test "owner self-heal: same session keeps appending" {
	CLAUDE_CODE_SESSION_ID="sess-A" run_app hook-read-image.json
	CLAUDE_CODE_SESSION_ID="sess-A" run_app hook-read-image.json
	run wc -l <"$MANIFEST"
	[ "$output" -eq 2 ]
}

@test "no TMUX_PANE and no session id -> no-op, exit 0" {
	unset TMUX_PANE
	unset CLAUDE_CODE_SESSION_ID
	run run_app hook-read-image.json
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}

@test "no TMUX_PANE falls back to CLAUDE_CODE_SESSION_ID key" {
	unset TMUX_PANE
	export CLAUDE_CODE_SESSION_ID="sess-abc"
	run run_app hook-read-image.json
	[ "$status" -eq 0 ]
	sess_manifest="$CLAUDE_STATUS_DIR/images/sess-abc.jsonl"
	[ -f "$sess_manifest" ]
	run jq -r '.path' "$sess_manifest"
	[ "$output" = "$IMG" ]
}

@test "AEYE_DIR takes precedence over CLAUDE_STATUS_DIR" {
	# CLAUDE_STATUS_DIR is already exported by setup(); set the override too.
	export AEYE_DIR="$BATS_TEST_TMPDIR/carousel"
	run_app hook-read-image.json
	[ -f "$AEYE_DIR/images/7.jsonl" ]
	[ ! -f "$MANIFEST" ]
}

# Renderer selection is Go (chooseGridBackend, tested in gallery_test.go).

@test "Phase-2 ignores an over-long path-like string (regex DoS guard)" {
	# A >4096-char token that ends in .png must NOT be scanned/matched.
	big="/$(printf 'a%.0s' {1..5000}).png"
	payload="$(jq -nc --arg t "see $big here" '{tool_name:"X",cwd:"/work",tool_input:{},tool_response:{content:[{type:"text",text:$t}]}}')"
	run bash -c "printf '%s' '$payload' | bash '$APP'"
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}
