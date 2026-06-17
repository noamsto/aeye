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

@test "extract_d2_path: existing .d2 file_path" {
	d2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$d2"
	payload="$(jq -nc --arg p "$d2" '{cwd:"/work",tool_input:{file_path:$p}}')"
	run extract_d2_path "$payload"
	[ "$output" = "$d2" ]
}

@test "extract_d2_path: a .png file_path -> empty" {
	payload="$(jq -nc '{cwd:"/work",tool_input:{file_path:"/x/pic.png"}}')"
	run extract_d2_path "$payload"
	[ -z "$output" ]
}

@test "d2_png_for: hash-stable png path under the diagrams dir" {
	d2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$d2"
	run d2_png_for "$d2" "/tmp/diagrams"
	[ "$status" -eq 0 ]
	[[ $output == /tmp/diagrams/*.png ]]
	# stable for identical content
	first="$output"
	run d2_png_for "$d2" "/tmp/diagrams"
	[ "$output" = "$first" ]
}

@test "d2_render: renders png via stubbed aeye render-diagram" {
	STUB="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB"
	cat >"$STUB/aeye" <<'STUB'
#!/usr/bin/env bash
[[ ${1:-} == render-diagram ]] || exit 0
printf '<svg/>' >"${3%.png}.svg"
printf 'PNG' >"$3"
STUB
	chmod +x "$STUB/aeye"
	export PATH="$STUB:$PATH"
	d2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$d2"
	run d2_render "$d2" "$BATS_TEST_TMPDIR/diagrams"
	[ "$status" -eq 0 ]
	[ -f "$output" ]
	[[ $output == *.png ]]
}
