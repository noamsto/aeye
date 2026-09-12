#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	export PLUGIN_ROOT="$ROOT/adapters/pi"
	APP="$PLUGIN_ROOT/scripts/session-backfill.sh"
	FIXTURES="$ROOT/tests/fixtures/codex"

	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	export TMUX="fake,4242,0"
	MANIFEST="$AEYE_DIR/images/4242-7.jsonl"
	mkdir -p "$AEYE_DIR/images"

	# Stub the d2 render pipeline (see tests/pi/diagrams.bats for the rationale).
	STUB_BIN="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB_BIN"
	cat >"$STUB_BIN/aeye" <<'STUB'
#!/usr/bin/env bash
[[ ${1:-} == render-diagram ]] || exit 0
in="$2"
out="$3"
printf '<svg/>' >"${out%.png}.svg"
printf 'PNG' >"$out"
STUB
	chmod +x "$STUB_BIN/aeye"
	export PATH="$STUB_BIN:$PATH"
}

# write_calls FILE JSON... -> one call per line
write_calls() {
	local file="$1"
	shift
	: >"$file"
	for call in "$@"; do printf '%s\n' "$call" >>"$file"; done
}

@test "rebuilds the manifest from the session history" {
	IMG="$FIXTURES/bar.png"
	read_call="$(jq -nc --arg p "$IMG" '{tool_name:"read",tool_input:{path:$p},tool_response:{content:[""]},cwd:"/repo"}')"
	calls="$BATS_TEST_TMPDIR/calls.jsonl"
	write_calls "$calls" "$read_call"

	payload="$(jq -nc --arg sid sess-A --arg f "$calls" '{session_id:$sid,cwd:"/repo",calls_file:$f}')"
	printf '%s' "$payload" | bash "$APP"
	[ -f "$MANIFEST" ]
	run jq -r '.path' "$MANIFEST"
	[ "$output" = "$IMG" ]
	[ "$(cat "$AEYE_DIR/images/4242-7.owner")" = "sess-A" ]
}

@test "rebuild is authoritative: a foreign manifest is wiped, not merged" {
	printf '{"type":"image","path":"/foreign.png","source":"read","mtime":1}\n' >"$MANIFEST"
	IMG="$FIXTURES/bar.png"
	read_call="$(jq -nc --arg p "$IMG" '{tool_name:"read",tool_input:{path:$p},tool_response:{content:[""]},cwd:"/repo"}')"
	calls="$BATS_TEST_TMPDIR/calls.jsonl"
	write_calls "$calls" "$read_call"

	payload="$(jq -nc --arg sid sess-A --arg f "$calls" '{session_id:$sid,cwd:"/repo",calls_file:$f}')"
	printf '%s' "$payload" | bash "$APP"
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	run jq -r '.path' "$MANIFEST"
	[ "$output" = "$IMG" ]
}

@test "renders a historical .d2 into the manifest" {
	D2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$D2"
	d2_call="$(jq -nc --arg p "$D2" '{tool_name:"write",tool_input:{file_path:$p},tool_response:{content:[""]},cwd:"/repo"}')"
	calls="$BATS_TEST_TMPDIR/calls.jsonl"
	write_calls "$calls" "$d2_call"

	payload="$(jq -nc --arg sid sess-A --arg f "$calls" '{session_id:$sid,cwd:"/repo",calls_file:$f}')"
	printf '%s' "$payload" | bash "$APP"
	[ -f "$MANIFEST" ]
	run jq -r '.source' "$MANIFEST"
	[ "$output" = "d2" ]
	[ -f "$(jq -r '.path' "$MANIFEST")" ]
}

@test "dedups a repeated path within the replay" {
	IMG="$FIXTURES/bar.png"
	read_call="$(jq -nc --arg p "$IMG" '{tool_name:"read",tool_input:{path:$p},tool_response:{content:[""]},cwd:"/repo"}')"
	calls="$BATS_TEST_TMPDIR/calls.jsonl"
	write_calls "$calls" "$read_call" "$read_call"

	payload="$(jq -nc --arg sid sess-A --arg f "$calls" '{session_id:$sid,cwd:"/repo",calls_file:$f}')"
	printf '%s' "$payload" | bash "$APP"
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
}

@test "an empty history clears the pane's manifest" {
	printf '{"type":"image","path":"/foreign.png","source":"read","mtime":1}\n' >"$MANIFEST"
	calls="$BATS_TEST_TMPDIR/calls.jsonl"
	: >"$calls"
	payload="$(jq -nc --arg sid sess-A --arg f "$calls" '{session_id:$sid,cwd:"/repo",calls_file:$f}')"
	printf '%s' "$payload" | bash "$APP"
	[ ! -f "$MANIFEST" ]
}

@test "an unreadable history drops a foreign manifest but keeps an owned one" {
	printf 'sess-old' >"$AEYE_DIR/images/4242-7.owner"
	printf '{"type":"image","path":"/x.png","source":"read","mtime":1}\n' >"$MANIFEST"
	payload="$(jq -nc --arg sid sess-A '{session_id:$sid,cwd:"/repo",calls_file:"/nope/calls.jsonl"}')"
	printf '%s' "$payload" | bash "$APP"
	[ ! -f "$MANIFEST" ]

	printf 'sess-A' >"$AEYE_DIR/images/4242-7.owner"
	printf '{"type":"image","path":"/x.png","source":"read","mtime":1}\n' >"$MANIFEST"
	printf '%s' "$payload" | bash "$APP"
	[ -f "$MANIFEST" ]
}

@test "no calls_file key is a clean no-op when no manifest exists" {
	payload="$(jq -nc '{session_id:"sess-A",cwd:"/repo"}')"
	run bash -c "printf '%s' '$payload' | bash '$APP'"
	[ "$status" -eq 0 ]
}
