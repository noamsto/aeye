#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	export PLUGIN_ROOT="$ROOT/adapters/codex/plugin"
	APP="$PLUGIN_ROOT/scripts/session-backfill.sh"
	FIXTURES="$ROOT/tests/fixtures/codex"

	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%9"
	MANIFEST="$AEYE_DIR/images/9.jsonl"
	OWNER="$AEYE_DIR/images/9.owner"

	WORKDIR="$BATS_TEST_TMPDIR/work"
	mkdir -p "$WORKDIR"
	printf 'a -> b: hi\n' >"$WORKDIR/diagram.d2"
	printf 'a -> b\n' >"$WORKDIR/legacy.d2"
	printf 'x' >"$WORKDIR/sample.png"
	printf 'x' >"$WORKDIR/shot2.png"

	ROLLOUT="$BATS_TEST_TMPDIR/rollout.jsonl"
	sed -e "s#WORKDIR#$WORKDIR#g" -e "s#PNGPATH#$WORKDIR/sample.png#g" -e "s#SHOTPATH#$WORKDIR/shot2.png#g" \
		"$FIXTURES/rollout-basic.jsonl" >"$ROLLOUT"

	# Stub aeye render-diagram so .d2 backfill renders hermetically (mirrors
	# diagrams.bats / the Claude adapter's session-backfill.bats stub).
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
}

run_app() { # $1 = source
	jq -nc --arg s "$1" --arg t "$ROLLOUT" '{source:$s,transcript_path:$t,session_id:"fixture-sess-1"}' | bash "$APP"
}

@test "resume backfills the view_image path from the exec/JS transport" {
	run_app resume
	[ -f "$MANIFEST" ]
	run jq -rc 'select(.path=="'"$WORKDIR"'/sample.png") | .source' "$MANIFEST"
	[ "$output" = "view_image" ]
}

@test "resume backfills the apply_patch-added .d2 from the exec/JS transport as a rendered diagram" {
	run_app resume
	run jq -rc 'select(.name=="diagram") | .path' "$MANIFEST"
	[[ $output == *"/diagrams/"*.png ]]
	[ -f "$output" ]
}

@test "resume backfills the legacy direct custom_tool_call/apply_patch as a rendered diagram" {
	run_app resume
	run jq -rc 'select(.name=="legacy") | .path' "$MANIFEST"
	[[ $output == *"/diagrams/"*.png ]]
	[ -f "$output" ]
}

@test "resume backfills a screenshot path from the legacy function_call/exec_command paired output" {
	run_app resume
	run jq -rc 'select(.path=="'"$WORKDIR"'/shot2.png") | .source' "$MANIFEST"
	[ "$output" = "screenshot" ]
}

@test "resume produces exactly the four expected manifest lines, nothing from unrelated lines" {
	run_app resume
	run wc -l <"$MANIFEST"
	[ "$output" -eq 4 ]
	run grep -c 'not-a-real' "$MANIFEST"
	[ "$output" -eq 0 ]
	run grep -c 'out.png' "$MANIFEST"
	[ "$output" -eq 0 ]
	run grep -c 'truncated' "$MANIFEST"
	[ "$output" -eq 0 ]
	run grep -c 'aeye-spike-done' "$MANIFEST"
	[ "$output" -eq 0 ]
}

@test "an exec/JS tools.exec_command call (neither apply_patch nor view_image) is a no-op, not a crash" {
	run run_app resume
	[ "$status" -eq 0 ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 4 ]
}

@test "ts is taken from the transcript record (chronological)" {
	run_app resume
	run jq -r 'select(.path=="'"$WORKDIR"'/sample.png") | .ts' "$MANIFEST"
	[ "$output" = "2026-07-13T07:45:43.102Z" ]
}

@test "resume claims the manifest via the owner sidecar (codex_session_id)" {
	run_app resume
	run cat "$OWNER"
	[ "$output" = "fixture-sess-1" ]
}

@test "dedup against a pre-seeded manifest -> no double entry" {
	mkdir -p "$AEYE_DIR/images"
	printf '{"type":"image","path":"%s/sample.png","source":"view_image","ts":"old","mtime":0}\n' "$WORKDIR" >"$MANIFEST"
	run_app resume
	run grep -c "\"path\":\"$WORKDIR/sample.png\"" "$MANIFEST"
	[ "$output" -eq 1 ]
}

@test "resume drops a foreign entry not in the transcript (reused-pane bleed)" {
	mkdir -p "$AEYE_DIR/images"
	printf '{"type":"image","path":"/OLD-SESSION.png","source":"Read","ts":"old","mtime":0}\n' >"$MANIFEST"
	printf 'sess-old' >"$OWNER"
	run_app resume
	run grep -c 'OLD-SESSION' "$MANIFEST"
	[ "$output" -eq 0 ]
}

@test "resume with unreadable transcript clears a foreign manifest" {
	mkdir -p "$AEYE_DIR/images"
	printf '{"type":"image","path":"/OLD-SESSION.png"}\n' >"$MANIFEST"
	printf 'sess-old' >"$OWNER"
	run bash -c 'jq -nc "{source:\"resume\",transcript_path:\"/no/such/file\",session_id:\"fixture-sess-1\"}" | bash "'"$APP"'"'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}

@test "resume with unreadable transcript keeps a same-session manifest" {
	mkdir -p "$AEYE_DIR/images"
	printf '{"type":"image","path":"/MINE.png"}\n' >"$MANIFEST"
	printf 'fixture-sess-1' >"$OWNER"
	run bash -c 'jq -nc "{source:\"resume\",transcript_path:\"/no/such/file\",session_id:\"fixture-sess-1\"}" | bash "'"$APP"'"'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
}

@test "non-resume source is a no-op" {
	run_app startup
	[ ! -f "$MANIFEST" ]
}

@test "missing transcript_path -> clean exit 0, no manifest" {
	run bash -c 'jq -nc "{source:\"resume\",session_id:\"fixture-sess-1\"}" | bash "'"$APP"'"'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}

@test "a truncated/non-JSON final rollout line does not abort the backfill" {
	run run_app resume
	[ "$status" -eq 0 ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 4 ]
}
