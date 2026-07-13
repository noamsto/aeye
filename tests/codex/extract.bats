#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	LIB="$ROOT/adapters/codex/plugin/scripts/lib/shim.sh"
	FIXTURES="$ROOT/tests/fixtures/codex"
	# shellcheck source=/dev/null
	source "$LIB"
}

@test "codex_extract_touched_paths: apply_patch Add + Update of existing image/d2 paths" {
	D2="$BATS_TEST_TMPDIR/docs/foo.d2"
	mkdir -p "$(dirname "$D2")"
	printf 'a -> b\n' >"$D2"
	PNG="$FIXTURES/bar.png"
	cmd="$(printf '*** Begin Patch\n*** Add File: docs/foo.d2\n+x\n*** Update File: %s\n*** End Patch' "$PNG")"
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR" --arg cmd "$cmd" '{tool_name:"apply_patch",tool_input:{command:$cmd},cwd:$c}')"
	run codex_extract_touched_paths "$payload"
	[ "$status" -eq 0 ]
	[[ $output == *"$D2"* ]]
	[[ $output == *"$PNG"* ]]
}

@test "codex_extract_touched_paths: view_image of an existing png is echoed" {
	PNG="$FIXTURES/shot.png"
	payload="$(jq -nc --arg p "$PNG" '{tool_name:"view_image",tool_input:{path:$p},cwd:"/repo"}')"
	run codex_extract_touched_paths "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$PNG" ]
}

@test "codex_extract_touched_paths: apply_patch touching README.md is not echoed" {
	cmd='*** Begin Patch
*** Update File: README.md
+x
*** End Patch'
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR" --arg cmd "$cmd" '{tool_name:"apply_patch",tool_input:{command:$cmd},cwd:$c}')"
	run codex_extract_touched_paths "$payload"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "codex_extract_touched_paths: Bash tool_response embedding a screenshot path is captured" {
	PNG="$FIXTURES/screenshot.png"
	payload="$(jq -nc --arg p "$PNG" '{tool_name:"Bash",tool_input:{command:"ls"},tool_response:("saved to "+$p),cwd:"/repo"}')"

	# Consume via a read-loop, like diagrams.sh / session-backfill.sh do, so a
	# dropped final (non-newline-terminated) line fails this test.
	paths=()
	while IFS= read -r p; do [[ -n $p ]] && paths+=("$p"); done < <(codex_extract_touched_paths "$payload")

	printf '%s\n' "${paths[@]}" | grep -qF "$PNG"
}

@test "codex_session_id: echoes .session_id" {
	payload="$(jq -nc '{session_id:"abc-123"}')"
	run codex_session_id "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "abc-123" ]
}

@test "codex_session_id: missing session_id -> empty" {
	payload="$(jq -nc '{cwd:"/repo"}')"
	run codex_session_id "$payload"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}
