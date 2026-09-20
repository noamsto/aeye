#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	export PLUGIN_ROOT="$ROOT/adapters/pi"
	APP="$PLUGIN_ROOT/handlers/backfill-calls.sh"

	TRANSCRIPT="$BATS_TEST_TMPDIR/transcript.jsonl"
	# Chain (root -> tail): root -> m1 (bashExecution) -> mc1 (model_change, a
	# non-message parent) -> m2 (assistant toolCall tc1) -> m3 (toolResult tc1)
	# -> m4 (assistant toolCall tc2) — m4 is the last message line, so it's the
	# head of the active branch.
	#
	# mAB forks off m1 as a sibling of mc1: nothing in the chain from m4 walks
	# back through it, so it's an abandoned branch and must not appear in the
	# output even though its tool_name ("bash") would otherwise pass the filter.
	cat >"$TRANSCRIPT" <<'EOF'
{"id":"root","parentId":null,"type":"session"}
{"id":"m1","parentId":"root","type":"message","message":{"role":"bashExecution","command":"ls -la","output":"file1\nfile2"}}
{"id":"mAB","parentId":"m1","type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc-abandoned","name":"bash","arguments":{"command":"echo abandoned"}}]}}
{"id":"mc1","parentId":"m1","type":"model_change","model":"claude-x"}
{"id":"m2","parentId":"mc1","type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc1","name":"Read","arguments":{"path":"/tmp/foo.png"}}]}}
{"id":"m3","parentId":"m2","type":"message","message":{"role":"toolResult","toolCallId":"tc1","content":[{"type":"text","text":"read ok"}]}}
{"id":"m4","parentId":"m3","type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc2","name":"edit","arguments":{"file_path":"/tmp/bar.d2"}}]}}
EOF
}

@test "replays the active branch, dropping the abandoned fork" {
	run bash "$APP" "$TRANSCRIPT" "/work" "sess-test"
	[ "$status" -eq 0 ]

	expected="$(
		jq -nc '{tool_name:"bash",tool_input:{command:"ls -la"},tool_response:{content:["file1\nfile2"]},cwd:"/work",session_id:"sess-test"}'
		jq -nc '{tool_name:"Read",tool_input:{path:"/tmp/foo.png"},tool_response:{content:["read ok"]},cwd:"/work",session_id:"sess-test"}'
		jq -nc '{tool_name:"edit",tool_input:{file_path:"/tmp/bar.d2"},tool_response:{content:[""]},cwd:"/work",session_id:"sess-test"}'
	)"
	[ "$output" = "$expected" ]
}

@test "an empty or missing session file produces no output" {
	run bash "$APP" "" "/work" "sess-test"
	[ "$status" -eq 0 ]
	[ -z "$output" ]

	run bash "$APP" "/nonexistent/transcript.jsonl" "/work" "sess-test"
	[ "$status" -eq 0 ]
	[ -z "$output" ]

	: >"$BATS_TEST_TMPDIR/empty.jsonl"
	run bash "$APP" "$BATS_TEST_TMPDIR/empty.jsonl" "/work" "sess-test"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "drops calls whose tool is not in the capture set" {
	transcript="$BATS_TEST_TMPDIR/other-tool.jsonl"
	cat >"$transcript" <<'EOF'
{"id":"root","parentId":null,"type":"session"}
{"id":"m1","parentId":"root","type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc1","name":"grep","arguments":{"pattern":"foo"}}]}}
EOF
	run bash "$APP" "$transcript" "/work" "sess-test"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "a parentId cycle terminates instead of looping forever" {
	transcript="$BATS_TEST_TMPDIR/cycle.jsonl"
	# m1 <-> m2 point at each other; nothing here is a legitimate root. The
	# steps<=max_steps bound must stop the walk rather than spin.
	cat >"$transcript" <<'EOF'
{"id":"m1","parentId":"m2","type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc1","name":"bash","arguments":{"command":"echo one"}}]}}
{"id":"m2","parentId":"m1","type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc2","name":"bash","arguments":{"command":"echo two"}}]}}
EOF
	timeout 5 bash "$APP" "$transcript" "/work" "sess-test"
	status=$?
	[ "$status" -ne 124 ] # 124 == timeout killed it (would mean an infinite loop)
	[ "$status" -eq 0 ]
}
