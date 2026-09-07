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
	mkdir -p "$BATS_TEST_TMPDIR/proj"
	shot="$BATS_TEST_TMPDIR/proj/shot.png"
	printf 'x' >"$shot"
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR/proj" --arg p "$shot" '{cwd:$c,tool_input:{},tool_response:{content:[{type:"text",text:("saved to "+$p)}]}}')"
	run extract_image_path "$payload"
	[ "$output" = "$shot" ]
}

@test "extract_image_path: phase-2 ignores a path outside cwd" {
	# A Bash command whose output merely mentions an existing image in an
	# unrelated project must not land in this pane's carousel (#139).
	mkdir -p "$BATS_TEST_TMPDIR/proj" "$BATS_TEST_TMPDIR/other"
	foreign="$BATS_TEST_TMPDIR/other/pic.png"
	printf 'x' >"$foreign"
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR/proj" --arg p "$foreign" '{cwd:$c,tool_input:{},tool_response:{content:[{type:"text",text:("ls: "+$p)}]}}')"
	run extract_image_path "$payload"
	[ -z "$output" ]
}

@test "extract_image_path: non-image payload -> empty" {
	payload="$(jq -nc '{cwd:"/work",tool_input:{command:"ls"},tool_response:{}}')"
	run extract_image_path "$payload"
	[ -z "$output" ]
}

@test "scan_response_image_path: tool_output JSON string (Cursor)" {
	mkdir -p "$BATS_TEST_TMPDIR/proj"
	shot="$BATS_TEST_TMPDIR/proj/shot.png"
	printf 'x' >"$shot"
	inner="$(jq -nc --arg p "$shot" '{output:("saved to "+$p),exitCode:0}')"
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR/proj" --arg o "$inner" '{cwd:$c,tool_output:$o}')"
	run scan_response_image_path "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$shot" ]
}

@test "scan_response_image_path: tool_output plain non-JSON string" {
	mkdir -p "$BATS_TEST_TMPDIR/proj"
	shot="$BATS_TEST_TMPDIR/proj/shot.png"
	printf 'x' >"$shot"
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR/proj" --arg p "$shot" '{cwd:$c,tool_output:("saved to "+$p)}')"
	run scan_response_image_path "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$shot" ]
}

@test "scan_response_image_path: neither tool_response nor tool_output -> empty" {
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR/proj" '{cwd:$c}')"
	run scan_response_image_path "$payload"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "scan_response_image_path: relative path with internal slash is not truncated (#213)" {
	mkdir -p "$BATS_TEST_TMPDIR/proj/work/pw-output"
	shot="$BATS_TEST_TMPDIR/proj/work/pw-output/page-2026-09-07T05-53-54-492Z.png"
	printf 'x' >"$shot"
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR/proj" '{cwd:$c,tool_output:"work/pw-output/page-2026-09-07T05-53-54-492Z.png"}')"
	run scan_response_image_path "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$shot" ]
}

@test "scan_response_image_path: leading dot-dir relative path is not truncated (#213)" {
	mkdir -p "$BATS_TEST_TMPDIR/proj/.playwright-mcp"
	shot="$BATS_TEST_TMPDIR/proj/.playwright-mcp/page-2026-09-07T05-55-22-156Z.png"
	printf 'x' >"$shot"
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR/proj" '{cwd:$c,tool_output:".playwright-mcp/page-2026-09-07T05-55-22-156Z.png"}')"
	run scan_response_image_path "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$shot" ]
}

@test "scan_response_image_path: explicit ./ relative path still captured whole" {
	mkdir -p "$BATS_TEST_TMPDIR/proj"
	printf 'x' >"$BATS_TEST_TMPDIR/proj/explicit-shot.png"
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR/proj" '{cwd:$c,tool_output:"./explicit-shot.png"}')"
	run scan_response_image_path "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$BATS_TEST_TMPDIR/proj/./explicit-shot.png" ]
}

@test "scan_response_image_path: absolute path still captured whole" {
	mkdir -p "$BATS_TEST_TMPDIR/proj/tmp/foo"
	shot="$BATS_TEST_TMPDIR/proj/tmp/foo/bar.png"
	printf 'x' >"$shot"
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR/proj" --arg p "$shot" '{cwd:$c,tool_output:$p}')"
	run scan_response_image_path "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$shot" ]
}

@test "scan_response_image_path: non-image extension does not match" {
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR/proj" '{cwd:$c,tool_output:"see docs/readme.md for details"}')"
	run scan_response_image_path "$payload"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "is_d2_render_artifact: matches only direct themed render outputs" {
	dir="$BATS_TEST_TMPDIR/images/diagrams"

	run is_d2_render_artifact "$dir/0123456789abcdef-light.png" "$dir"
	[ "$status" -eq 0 ]

	run is_d2_render_artifact "$dir/0123456789abcdef-dark.svg" "$dir"
	[ "$status" -eq 0 ]

	run is_d2_render_artifact "$dir/preview-light.png" "$dir"
	[ "$status" -ne 0 ]

	run is_d2_render_artifact "$dir/nested/0123456789abcdef-light.png" "$dir"
	[ "$status" -ne 0 ]

	run is_d2_render_artifact "${dir}-backup/0123456789abcdef-light.png" "$dir"
	[ "$status" -ne 0 ]
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

@test "extract_d2_path: heredoc write via the Bash tool" {
	d2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$d2"
	cmd="cat > $d2 <<'EOF'
a -> b
EOF"
	payload="$(jq -nc --arg c "$cmd" '{cwd:"/work",tool_name:"Bash",tool_input:{command:$c}}')"
	run extract_d2_path "$payload"
	[ "$output" = "$d2" ]
}

@test "extract_d2_path: shell command path resolved against cwd" {
	mkdir -p "$BATS_TEST_TMPDIR/proj"
	printf 'a -> b\n' >"$BATS_TEST_TMPDIR/proj/flow.d2"
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR/proj" '{cwd:$c,tool_name:"Bash",tool_input:{command:"sed -i s/a/b/ flow.d2"}}')"
	run extract_d2_path "$payload"
	[ "$output" = "$BATS_TEST_TMPDIR/proj/flow.d2" ]
}

@test "extract_d2_path: quoted heredoc target" {
	d2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$d2"
	payload="$(jq -nc --arg p "$d2" '{cwd:"/work",tool_name:"Bash",tool_input:{command:("cat > \"" + $p + "\" <<EOF")}}')"
	run extract_d2_path "$payload"
	[ "$output" = "$d2" ]
}

@test "extract_d2_path: shell command naming a .d2 that does not exist -> empty" {
	payload="$(jq -nc '{cwd:"/work",tool_name:"Bash",tool_input:{command:"cat > /nope/flow.d2 <<EOF"}}')"
	run extract_d2_path "$payload"
	[ -z "$output" ]
}

@test "extract_d2_path: shell command with no .d2 mention -> empty" {
	payload="$(jq -nc '{cwd:"/work",tool_name:"Bash",tool_input:{command:"go test ./..."}}')"
	run extract_d2_path "$payload"
	[ -z "$output" ]
}

@test "extract_d2_path: first existing .d2 wins over an absent earlier one" {
	d2="$BATS_TEST_TMPDIR/real.d2"
	printf 'a -> b\n' >"$d2"
	payload="$(jq -nc --arg p "$d2" '{cwd:"/work",tool_name:"Bash",tool_input:{command:("rm -f /gone/old.d2; cat > " + $p + " <<EOF")}}')"
	run extract_d2_path "$payload"
	[ "$output" = "$d2" ]
}

@test "extract_d2_path: variable-built heredoc target -> newest .d2 in the src dir" {
	src="$BATS_TEST_TMPDIR/src"
	mkdir -p "$src"
	printf 'a -> b\n' >"$src/old.d2"
	touch -d "@$(($(date +%s) - 600))" "$src/old.d2"
	printf 'a -> b\n' >"$src/fresh.d2"
	payload="$(jq -nc '{cwd:"/work",tool_name:"Bash",tool_input:{command:"cat > \"$SRC_DIR/fresh.d2\" <<EOF"}}')"
	run extract_d2_path "$payload" "$src"
	[ "$output" = "$src/fresh.d2" ]
}

@test "extract_d2_path: variable-built target with no src dir -> empty" {
	payload="$(jq -nc '{cwd:"/work",tool_name:"Bash",tool_input:{command:"cat > \"$SRC_DIR/fresh.d2\" <<EOF"}}')"
	run extract_d2_path "$payload"
	[ -z "$output" ]
}

@test "extract_d2_path: a stale .d2 in the src dir is not adopted" {
	src="$BATS_TEST_TMPDIR/src"
	mkdir -p "$src"
	printf 'a -> b\n' >"$src/old.d2"
	touch -d "@$(($(date +%s) - 600))" "$src/old.d2"
	payload="$(jq -nc '{cwd:"/work",tool_name:"Bash",tool_input:{command:"grep -c . \"$SRC_DIR/old.d2\""}}')"
	run extract_d2_path "$payload" "$src"
	[ -z "$output" ]
}

@test "extract_d2_path: a literal .d2 mention never falls back to the src dir" {
	src="$BATS_TEST_TMPDIR/src"
	mkdir -p "$src"
	printf 'a -> b\n' >"$src/fresh.d2"
	payload="$(jq -nc '{cwd:"/work",tool_name:"Bash",tool_input:{command:"ls *.d2"}}')"
	run extract_d2_path "$payload" "$src"
	[ -z "$output" ]
}

@test "d2_png_for: hash-stable themed png path under the diagrams dir" {
	d2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$d2"
	run d2_png_for "$d2" "/tmp/diagrams" dark
	[ "$status" -eq 0 ]
	[[ $output == /tmp/diagrams/*-dark.png ]]
	# stable for identical content
	first="$output"
	run d2_png_for "$d2" "/tmp/diagrams" dark
	[ "$output" = "$first" ]
	# light variant shares the hash, differs only by the theme suffix
	run d2_png_for "$d2" "/tmp/diagrams" light
	[[ $output == /tmp/diagrams/*-light.png ]]
	[ "${output/-light.png/-dark.png}" = "$first" ]
}

@test "d2_render: renders both theme variants, echoes the dark one" {
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
	[[ $output == *-dark.png ]]
	[ -f "$output" ]
	# both variants rendered
	[ -f "${output/-dark.png/-light.png}" ]
}
