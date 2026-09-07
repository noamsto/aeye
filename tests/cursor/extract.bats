#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	LIB="$ROOT/adapters/cursor/scripts/lib/shim.sh"
	FIXTURES="$ROOT/tests/fixtures/cursor"
	export AEYE_CURSOR_HOME="$BATS_TEST_TMPDIR/cursorhome"
	# shellcheck source=/dev/null
	source "$LIB"
}

@test "cursor_extract_touched_paths: Write of existing png via file_path with empty cwd + workspace_roots" {
	PNG="$FIXTURES/shot.png"
	payload="$(jq -nc --arg p "$PNG" --arg wr "$FIXTURES" \
		'{tool_name:"Write",tool_input:{file_path:$p},cwd:"",workspace_roots:[$wr]}')"
	run cursor_extract_touched_paths "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$PNG" ]
}

@test "cursor_extract_touched_paths: Read of existing png via path field with cwd set" {
	PNG="$FIXTURES/shot.png"
	payload="$(jq -nc --arg p "$PNG" --arg c "$FIXTURES" \
		'{tool_name:"Read",tool_input:{path:$p},cwd:$c}')"
	run cursor_extract_touched_paths "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$PNG" ]
}

@test "cursor_extract_touched_paths: Write of relative auth-flow.d2 resolves against workspace_roots[0]" {
	D2="$BATS_TEST_TMPDIR/auth-flow.d2"
	printf 'a -> b\n' >"$D2"
	payload="$(jq -nc --arg wr "$BATS_TEST_TMPDIR" \
		'{tool_name:"Write",tool_input:{file_path:"auth-flow.d2"},cwd:"",workspace_roots:[$wr]}')"
	run cursor_extract_touched_paths "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "$D2" ]
}

@test "cursor_extract_touched_paths: Write of README.md is not echoed" {
	printf 'x\n' >"$BATS_TEST_TMPDIR/README.md"
	payload="$(jq -nc --arg c "$BATS_TEST_TMPDIR" --arg p "$BATS_TEST_TMPDIR/README.md" \
		'{tool_name:"Write",tool_input:{file_path:$p},cwd:$c}')"
	run cursor_extract_touched_paths "$payload"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "cursor_extract_touched_paths: Shell tool_output JSON string embedding a screenshot path is captured" {
	PNG="$FIXTURES/shot.png"
	tool_output="$(jq -nc --arg p "$PNG" '{output:("saved to "+$p+"\n"),exitCode:0}')"
	payload="$(jq -nc --arg wr "$FIXTURES" --arg to "$tool_output" \
		'{tool_name:"Shell",tool_input:{command:"ls"},tool_output:$to,cwd:"",workspace_roots:[$wr]}')"

	# Consume via a read-loop so a dropped final (non-newline-terminated) line fails.
	paths=()
	while IFS= read -r p; do [[ -n $p ]] && paths+=("$p"); done < <(cursor_extract_touched_paths "$payload")

	printf '%s\n' "${paths[@]}" | grep -qF "$PNG"
}

@test "cursor_session_id: prefers conversation_id; falls back to session_id; missing both -> empty" {
	payload="$(jq -nc '{conversation_id:"conv-1",session_id:"sess-1"}')"
	run cursor_session_id "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "conv-1" ]

	payload="$(jq -nc '{session_id:"sess-1"}')"
	run cursor_session_id "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "sess-1" ]

	payload="$(jq -nc '{cwd:"/repo"}')"
	run cursor_session_id "$payload"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "cursor_effective_cwd: non-empty cwd wins; empty cwd falls back to workspace_roots[0]" {
	payload="$(jq -nc '{cwd:"/from-cwd",workspace_roots:["/from-roots"]}')"
	run cursor_effective_cwd "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "/from-cwd" ]

	payload="$(jq -nc '{cwd:"",workspace_roots:["/from-roots"]}')"
	run cursor_effective_cwd "$payload"
	[ "$status" -eq 0 ]
	[ "$output" = "/from-roots" ]
}

@test "cursor_resume_transcript: exactly one match, non-empty -> echoes that path" {
	SID="conv-1"
	DIR="$AEYE_CURSOR_HOME/projects/some-slug/agent-transcripts/$SID"
	mkdir -p "$DIR"
	printf '{"tool_name":"Read"}\n' >"$DIR/$SID.jsonl"

	run cursor_resume_transcript "$SID"
	[ "$status" -eq 0 ]
	[ "$output" = "$DIR/$SID.jsonl" ]
}

@test "cursor_resume_transcript: zero matches -> empty" {
	run cursor_resume_transcript "no-such-conv"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "cursor_resume_transcript: file exists but is empty -> empty" {
	SID="conv-empty"
	DIR="$AEYE_CURSOR_HOME/projects/some-slug/agent-transcripts/$SID"
	mkdir -p "$DIR"
	: >"$DIR/$SID.jsonl"

	run cursor_resume_transcript "$SID"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "cursor_resume_transcript: two matches -> empty, not either path" {
	SID="conv-dup"
	for slug in slug-a slug-b; do
		DIR="$AEYE_CURSOR_HOME/projects/$slug/agent-transcripts/$SID"
		mkdir -p "$DIR"
		printf '{"tool_name":"Read"}\n' >"$DIR/$SID.jsonl"
	done

	run cursor_resume_transcript "$SID"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "cursor_resume_transcript: glob metacharacter or path traversal in id -> empty, never escapes AEYE_CURSOR_HOME" {
	mkdir -p "$AEYE_CURSOR_HOME/projects/some-slug/agent-transcripts/x"
	printf '{"tool_name":"Read"}\n' >"$AEYE_CURSOR_HOME/projects/some-slug/agent-transcripts/x/x.jsonl"

	OUTSIDE="$BATS_TEST_TMPDIR/outside"
	mkdir -p "$OUTSIDE"
	printf '{"tool_name":"Read"}\n' >"$OUTSIDE/escaped.jsonl"

	run cursor_resume_transcript '*'
	[ "$status" -eq 0 ]
	[ -z "$output" ]

	run cursor_resume_transcript '../outside/escaped'
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test 'cursor_resume_transcript: AEYE_CURSOR_HOME unset falls back to $HOME' {
	unset AEYE_CURSOR_HOME
	export HOME="$BATS_TEST_TMPDIR/fakehome"
	SID="conv-fakehome"
	DIR="$HOME/.cursor/projects/some-slug/agent-transcripts/$SID"
	mkdir -p "$DIR"
	printf '{"tool_name":"Read"}\n' >"$DIR/$SID.jsonl"

	run cursor_resume_transcript "$SID"
	[ "$status" -eq 0 ]
	[ "$output" = "$DIR/$SID.jsonl" ]
}
