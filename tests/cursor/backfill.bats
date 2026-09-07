#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	PLUGIN_ROOT="$ROOT/adapters/cursor"
	export PLUGIN_ROOT
	APP="$PLUGIN_ROOT/scripts/session-backfill.sh"
	FIXTURES="$ROOT/tests/fixtures/cursor"

	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	export AEYE_CURSOR_HOME="$BATS_TEST_TMPDIR/cursorhome"
	export TMUX_PANE="%9"
	export TMUX="fake,4242,0" # pane ids are per server; pin one so the key is stable
	MANIFEST="$AEYE_DIR/images/4242-9.jsonl"
	OWNER="$AEYE_DIR/images/4242-9.owner"

	CONV_ID="fixture-conv-1"

	WORKDIR="$BATS_TEST_TMPDIR/work"
	mkdir -p "$WORKDIR"
	printf 'a -> b: hi\n' >"$WORKDIR/diagram.d2"
	printf 'x' >"$WORKDIR/sample.png"
	printf 'x' >"$WORKDIR/shot2.png"

	# D2ARTPATH: a generated d2 theme-variant path, created unconditionally (not
	# only inside the one test that asserts the skip) so cursor_extract_touched_paths'
	# existence check passes and is_d2_render_artifact's skip logic actually fires.
	D2ART="$AEYE_DIR/images/diagrams/0123456789abcdef-light.png"
	mkdir -p "$(dirname "$D2ART")"
	printf 'x' >"$D2ART"

	TRANSCRIPT_DIR="$AEYE_CURSOR_HOME/projects/some-slug/agent-transcripts/$CONV_ID"
	mkdir -p "$TRANSCRIPT_DIR"
	TRANSCRIPT="$TRANSCRIPT_DIR/$CONV_ID.jsonl"
	sed -e "s#WORKDIR#$WORKDIR#g" -e "s#PNGPATH#$WORKDIR/sample.png#g" \
		-e "s#SHOTPATH#$WORKDIR/shot2.png#g" -e "s#D2ARTPATH#$D2ART#g" \
		"$FIXTURES/transcript-basic.jsonl" >"$TRANSCRIPT"

	# Stub aeye render-diagram so .d2 backfill renders hermetically (mirrors
	# diagrams.bats / the Codex adapter's session-backfill.bats stub).
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

run_app() {
	jq -nc --arg c "$CONV_ID" '{conversation_id:$c,session_id:$c}' | bash "$APP"
}

@test "backfills the .d2 Write as a rendered diagram" {
	run_app
	run jq -rc 'select(.name=="diagram") | .path' "$MANIFEST"
	[[ $output == *"/diagrams/"*.png ]]
	[ -f "$output" ]
}

@test "backfills the Read png path with source=Read" {
	run_app
	run jq -rc 'select(.path=="'"$WORKDIR"'/sample.png") | .source' "$MANIFEST"
	[ "$output" = "Read" ]
}

@test "backfills the Shell screenshot path with source=Shell" {
	run_app
	run jq -rc 'select(.path=="'"$WORKDIR"'/shot2.png") | .source' "$MANIFEST"
	[ "$output" = "Shell" ]
}

@test "does not backfill the generated d2 theme-variant path as a plain image" {
	run_app
	run jq -s --arg p "$D2ART" '[.[] | select(.path == $p)] | length' "$MANIFEST"
	[ "$output" -eq 0 ]
	run jq -s '[.[] | select(.source == "d2")] | length' "$MANIFEST"
	[ "$output" -eq 1 ]
}

@test "produces exactly the three expected manifest lines, nothing from unrelated lines" {
	run_app
	run wc -l <"$MANIFEST"
	[ "$output" -eq 3 ]
	run grep -c 'decoy-marker' "$MANIFEST"
	[ "$output" -eq 0 ]
	run grep -c 'truncated-tail-marker' "$MANIFEST"
	[ "$output" -eq 0 ]
}

@test "a truncated/non-JSON final transcript line does not abort the backfill" {
	run run_app
	[ "$status" -eq 0 ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 3 ]
}

@test "ts is taken from the transcript record (chronological)" {
	run_app
	run jq -r 'select(.path=="'"$WORKDIR"'/sample.png") | .ts' "$MANIFEST"
	[ "$output" = "2026-09-07T10:00:02.000Z" ]
}

@test "claims the manifest via the owner sidecar (cursor_session_id)" {
	run_app
	run cat "$OWNER"
	[ "$output" = "$CONV_ID" ]
}

@test "dedup against a pre-seeded manifest entry -> no double entry" {
	mkdir -p "$AEYE_DIR/images"
	printf '{"type":"image","path":"%s/sample.png","source":"Read","ts":"old","mtime":0}\n' "$WORKDIR" >"$MANIFEST"
	run_app
	run grep -c "\"path\":\"$WORKDIR/sample.png\"" "$MANIFEST"
	[ "$output" -eq 1 ]
}

@test "drops a foreign entry not in the transcript (reused-pane bleed)" {
	mkdir -p "$AEYE_DIR/images"
	printf '{"type":"image","path":"/OLD-SESSION.png","source":"Read","ts":"old","mtime":0}\n' >"$MANIFEST"
	printf 'sess-old' >"$OWNER"
	run_app
	run grep -c 'OLD-SESSION' "$MANIFEST"
	[ "$output" -eq 0 ]
}

@test "zero-match glob (no transcript directory for this conversation id) -> clean exit 0, no manifest" {
	run bash -c 'jq -nc "{conversation_id:\"no-such-conv\",session_id:\"no-such-conv\"}" | bash "'"$APP"'"'
	[ "$status" -eq 0 ]
	[ ! -f "$AEYE_DIR/images/4242-9.jsonl" ]
}

@test "empty payload -> clean exit 0, pre-seeded manifest left byte-for-byte unchanged" {
	mkdir -p "$AEYE_DIR/images"
	printf '{"type":"image","path":"/PRESEED.png","source":"Read","ts":"old","mtime":0}\n' >"$MANIFEST"
	before="$(cat "$MANIFEST")"
	run bash -c 'printf "" | bash "'"$APP"'"'
	[ "$status" -eq 0 ]
	[ "$(cat "$MANIFEST")" = "$before" ]
}

@test "no key at all (no tmux pane, no session id) -> clean exit 0, no crash" {
	unset TMUX_PANE
	run bash -c 'jq -nc "{}" | bash "'"$APP"'"'
	[ "$status" -eq 0 ]
}

@test "outside tmux, session-id-keyed path" {
	unset TMUX_PANE
	run run_app
	[ "$status" -eq 0 ]
	sess_manifest="$AEYE_DIR/images/$CONV_ID.jsonl"
	[ -f "$sess_manifest" ]
	run wc -l <"$sess_manifest"
	[ "$output" -eq 3 ]
}
