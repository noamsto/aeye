#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	export PLUGIN_ROOT="$ROOT/adapters/cursor"
	APP="$PLUGIN_ROOT/scripts/images.sh"
	FIXTURES="$ROOT/tests/fixtures/cursor"

	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	export TMUX="fake,4242,0" # pane ids are per server; pin one so the key is stable
	MANIFEST="$AEYE_DIR/images/4242-7.jsonl"

	# The toggle is only invoked by diagrams.sh, but stub it anyway so a stray
	# --ensure-open call in either script never touches the real tmux state.
	export AEYE_TOGGLE=true
}

# Build a Cursor postToolUse payload on stdout.
# Write/Read: path comes from tool_input only — keep tool_output free of the
# path so scan_response_image_path does not double-emit the same file.
# Shell: path lives in tool_output (see shell_payload).
write_payload() {
	local tool="$1" path="$2"
	local out
	out="$(jq -nc --arg p "$path" --arg wr "$(dirname "$path")" \
		'{conversation_id:"conv-1",session_id:"conv-1",tool_name:"Write",tool_input:{file_path:$p},tool_output:"{\"success\":true}",cwd:"",workspace_roots:[$wr],hook_event_name:"postToolUse"}')"
	case "$tool" in
	Write) printf '%s\n' "$out" ;;
	Read)
		jq -c '.tool_name="Read"' <<<"$out"
		;;
	*)
		printf '%s\n' "$out"
		;;
	esac
}

shell_payload() {
	local path="$1"
	local tool_output
	tool_output="$(jq -nc --arg p "$path" '{output:("saved to "+$p+"\n"),exitCode:0}')"
	jq -nc --arg p "$path" --arg wr "$(dirname "$path")" --arg to "$tool_output" \
		'{conversation_id:"conv-1",session_id:"conv-1",tool_name:"Shell",tool_input:{command:"ls"},tool_output:$to,cwd:"",workspace_roots:[$wr],hook_event_name:"postToolUse"}'
}

@test "Write of a real fixture png appends one image line" {
	PNG="$FIXTURES/shot.png"
	write_payload Write "$PNG" | bash "$APP"
	[ -f "$MANIFEST" ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	run jq -r '.path' "$MANIFEST"
	[ "$output" = "$PNG" ]
	run jq -r '.type' "$MANIFEST"
	[ "$output" = "image" ]
	run jq -r '.source' "$MANIFEST"
	[ "$output" = "Write" ]
}

@test "Read of a real fixture png appends one image line" {
	PNG="$FIXTURES/shot.png"
	write_payload Read "$PNG" | bash "$APP"
	[ -f "$MANIFEST" ]
	run jq -r '.path' "$MANIFEST"
	[ "$output" = "$PNG" ]
	run jq -r '.source' "$MANIFEST"
	[ "$output" = "Read" ]
}

@test "Shell tool_output embedding a screenshot path appends one image line" {
	PNG="$FIXTURES/shot.png"
	shell_payload "$PNG" | bash "$APP"
	[ -f "$MANIFEST" ]
	run jq -r '.path' "$MANIFEST"
	[ "$output" = "$PNG" ]
	run jq -r '.source' "$MANIFEST"
	[ "$output" = "Shell" ]
}

@test "Read of a generated d2 theme variant appends nothing" {
	PNG="$AEYE_DIR/images/diagrams/0123456789abcdef-light.png"
	mkdir -p "$(dirname "$PNG")"
	printf 'x' >"$PNG"

	write_payload Read "$PNG" | bash "$APP"
	[ ! -f "$MANIFEST" ]
}

@test "Write of a .d2 appends nothing (that's diagrams.sh's job)" {
	D2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$D2"
	write_payload Write "$D2" | bash "$APP"
	[ ! -f "$MANIFEST" ]
}

@test "no touched paths -> clean no-op, no state dir created" {
	D2="$BATS_TEST_TMPDIR/nope.d2"
	# Path does not exist on disk — extract returns nothing.
	payload="$(jq -nc --arg p "$D2" --arg wr "$BATS_TEST_TMPDIR" \
		'{conversation_id:"conv-1",session_id:"conv-1",tool_name:"Write",tool_input:{file_path:$p},tool_output:"{}",cwd:"",workspace_roots:[$wr],hook_event_name:"postToolUse"}')"
	echo "$payload" | bash "$APP"
	[ ! -f "$MANIFEST" ]
	[ ! -e "$AEYE_DIR/images/4242-7.lock" ]
	[ ! -e "$AEYE_DIR/images/4242-7.owner" ]
}

@test "keyed by TMUX_PANE, not the cursor conversation id" {
	PNG="$FIXTURES/shot.png"
	write_payload Write "$PNG" | bash "$APP"
	[ -f "$MANIFEST" ]
	[ ! -f "$AEYE_DIR/images/conv-1.jsonl" ]
}
