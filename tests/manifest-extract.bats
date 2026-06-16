#!/usr/bin/env bats

setup() {
	LIB="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/scripts/lib/manifest-extract.sh"
	IMG="$BATS_TEST_TMPDIR/pic.png"
	printf 'x' >"$IMG"
	# shellcheck source=/dev/null
	source "$LIB"
}

@test "extract_image_path: tool_input.file_path that exists" {
	payload="$(jq -nc --arg p "$IMG" '{cwd:"/work",tool_input:{file_path:$p},tool_response:{}}')"
	run extract_image_path "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$IMG" ]
}

@test "extract_image_path: missing file -> empty" {
	payload="$(jq -nc '{cwd:"/work",tool_input:{file_path:"/nope/x.png"},tool_response:{}}')"
	run extract_image_path "$payload"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "extract_image_path: relative path resolved against cwd" {
	mkdir -p "$BATS_TEST_TMPDIR/proj"
	printf 'x' >"$BATS_TEST_TMPDIR/proj/shot.png"
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR/proj" '{cwd:$c,tool_input:{output_path:"shot.png"},tool_response:{}}')"
	run extract_image_path "$payload"
	[ "$output" = "$BATS_TEST_TMPDIR/proj/shot.png" ]
}

@test "extract_image_path: phase-2 scan of tool_response" {
	payload="$(jq -nc --arg p "$IMG" '{cwd:"/work",tool_input:{},tool_response:{content:[{type:"text",text:("saved to "+$p)}]}}')"
	run extract_image_path "$payload"
	[ "$output" = "$IMG" ]
}

@test "extract_image_path: non-image payload -> empty" {
	payload="$(jq -nc '{cwd:"/work",tool_input:{command:"ls"},tool_response:{}}')"
	run extract_image_path "$payload"
	[ -z "$output" ]
}
