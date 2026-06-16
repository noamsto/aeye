#!/usr/bin/env bats

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	export CLAUDE_CODE_SESSION_ID="sess-resume"
	MANIFEST="$CLAUDE_STATUS_DIR/images/7.jsonl"
	OWNER="$CLAUDE_STATUS_DIR/images/7.owner"
	APP="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/scripts/session-backfill.sh"

	IMG="$BATS_TEST_TMPDIR/pic.png"
	printf 'x' >"$IMG"
	DOTD2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$DOTD2"

	TRANSCRIPT="$BATS_TEST_TMPDIR/transcript.jsonl"
	sed -e "s#IMGPATH#$IMG#g" -e "s#DOTD2#$DOTD2#g" \
		"$BATS_TEST_DIRNAME/fixtures/transcript-basic.jsonl" >"$TRANSCRIPT"

	# Stub d2/resvg so .d2 backfill renders hermetically.
	STUB="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB"
	cat >"$STUB/d2" <<'STUB'
#!/usr/bin/env bash
printf '<svg/>' >"${@: -1}"
STUB
	cat >"$STUB/resvg" <<'STUB'
#!/usr/bin/env bash
printf 'PNG' >"${@: -1}"
STUB
	chmod +x "$STUB/d2" "$STUB/resvg"
	export PATH="$STUB:$PATH"
}

run_app() { # $1 = source
	jq -nc --arg s "$1" --arg t "$TRANSCRIPT" '{source:$s,transcript_path:$t}' | bash "$APP"
}

@test "resume backfills the image (deduped to one line)" {
	run_app resume
	[ -f "$MANIFEST" ]
	run grep -c "\"path\":\"$IMG\"" "$MANIFEST"
	[ "$output" -eq 1 ]
}

@test "resume backfills the rendered diagram" {
	run_app resume
	run jq -rc 'select(.source=="d2") | .path' "$MANIFEST"
	[[ $output == *"/diagrams/"*.png ]]
	[ -f "$output" ]
}

@test "ts is taken from the transcript (chronological)" {
	run_app resume
	run jq -r 'select(.path=="'"$IMG"'") | .ts' "$MANIFEST"
	[ "$output" = "2026-06-16T10:00:02.000Z" ]
}

@test "resume claims the manifest via the owner sidecar" {
	run_app resume
	run cat "$OWNER"
	[ "$output" = "sess-resume" ]
}

@test "dedup against a pre-seeded manifest -> no double entry" {
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	printf '{"type":"image","path":"%s","source":"Read","ts":"old","mtime":0}\n' "$IMG" >"$MANIFEST"
	run_app resume
	run grep -c "\"path\":\"$IMG\"" "$MANIFEST"
	[ "$output" -eq 1 ]
}

@test "non-resume source is a no-op" {
	run_app startup
	[ ! -f "$MANIFEST" ]
}

@test "missing transcript_path -> clean exit 0, no manifest" {
	run bash -c 'printf "%s" "{\"source\":\"resume\"}" | bash "'"$APP"'"'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}

@test "a truncated/non-JSON transcript line does not abort the backfill" {
	# A crashed prior session can leave a partial final line that still matches the
	# grep fast-bail. It must be skipped, not abort the whole replay.
	printf 'truncated junk with /x.png and {"oops\n' >>"$TRANSCRIPT"
	run run_app resume
	[ "$status" -eq 0 ]
	run grep -c "\"path\":\"$IMG\"" "$MANIFEST"
	[ "$output" -eq 1 ]
}

@test "backfill does not open the carousel" {
	cat >"$STUB/tmux-claude-images" <<STUB
#!/usr/bin/env bash
echo called >>"$BATS_TEST_TMPDIR/toggle.log"
STUB
	chmod +x "$STUB/tmux-claude-images"
	run_app resume
	[ ! -f "$BATS_TEST_TMPDIR/toggle.log" ]
}
