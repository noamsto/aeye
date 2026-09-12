#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	LIB="$ROOT/adapters/pi/scripts/lib/shim.sh"
	# shellcheck source=/dev/null
	source "$LIB"
}

@test "pi_session_id: echoes .session_id" {
	payload="$(jq -nc '{session_id:"abc-123"}')"
	run pi_session_id "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "abc-123" ]
}

@test "pi_session_id: missing session_id -> empty" {
	payload="$(jq -nc '{cwd:"/repo"}')"
	run pi_session_id "$payload"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "extract_image_path: read of an explicit .path is echoed" {
	PNG="$ROOT/tests/fixtures/codex/bar.png"
	payload="$(jq -nc --arg p "$PNG" '{tool_name:"read",tool_input:{path:$p},cwd:"/repo"}')"
	run extract_image_path "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$PNG" ]
}

@test "extract_image_path: write/edit file_path is echoed" {
	PNG="$ROOT/tests/fixtures/codex/shot.png"
	payload="$(jq -nc --arg p "$PNG" '{tool_name:"write",tool_input:{file_path:$p},cwd:"/repo"}')"
	run extract_image_path "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$PNG" ]
}

@test "extract_image_path: a .d2 is not an image" {
	D2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$D2"
	payload="$(jq -nc --arg p "$D2" '{tool_name:"write",tool_input:{file_path:$p},cwd:"/repo"}')"
	run extract_image_path "$payload"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "extract_image_path: screenshot path embedded in bash output under cwd is captured" {
	PNG="$ROOT/tests/fixtures/codex/screenshot.png"
	payload="$(jq -nc --arg c "$ROOT/tests/fixtures/codex" --arg p "$PNG" \
		'{tool_name:"bash",tool_input:{command:"ls"},tool_response:{content:[("saved to "+$p)]},cwd:$c}')"
	run extract_image_path "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$PNG" ]
}

@test "extract_d2_path: write file_path is echoed" {
	D2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$D2"
	payload="$(jq -nc --arg p "$D2" '{tool_name:"write",tool_input:{file_path:$p},cwd:"/repo"}')"
	run extract_d2_path "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$D2" ]
}

@test "extract_d2_path: a .d2 token in a bash command is echoed" {
	D2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$D2"
	payload="$(jq -nc --arg p "$D2" '{tool_name:"bash",tool_input:{command:("cat > "+$p+" <<EOF\na -> b\nEOF")},cwd:"/repo"}')"
	run extract_d2_path "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$D2" ]
}

@test 'extract_d2_path: a $var path falls back to the newest .d2 in src' {
	SRC="$BATS_TEST_TMPDIR/src"
	mkdir -p "$SRC"
	D2="$SRC/flow.d2"
	printf 'a -> b\n' >"$D2"
	payload="$(jq -nc '{tool_name:"bash",tool_input:{command:"cat > \"$SRC_DIR/flow.d2\" <<EOF"},cwd:"/repo"}')"
	run extract_d2_path "$payload" "$SRC"
	[ "$status" -eq 0 ]
	[ "$output" = "$D2" ]
}
