#!/usr/bin/env bats
# shellcheck disable=SC2030,SC2031  # bats wraps each @test in a subshell; export is intentional
# shellcheck disable=SC2016  # the JSON payload's literal $0/backslash-escapes are intentional, not expansions

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	PLUGIN_ROOT="$ROOT/adapters/pi"

	# This mostly symlinked package tree lets tests replace a sub-script without
	# touching the real scripts/. session-backfill.sh is copied because its stub
	# records the payload written to it.
	PKG="$BATS_TEST_TMPDIR/pkg"
	mkdir -p "$PKG/handlers" "$PKG/scripts"
	cp "$PLUGIN_ROOT/handlers/session-start.sh" "$PKG/handlers/session-start.sh"
	cp "$PLUGIN_ROOT/handlers/backfill-calls.sh" "$PKG/handlers/backfill-calls.sh"
	cp "$PLUGIN_ROOT/scripts/session-backfill.sh" "$PKG/scripts/session-backfill.sh"
	ln -s "$PLUGIN_ROOT/scripts/diagram-guidance.sh" "$PKG/scripts/diagram-guidance.sh"
	ln -s "$PLUGIN_ROOT/scripts/core" "$PKG/scripts/core"
	ln -s "$PLUGIN_ROOT/scripts/lib" "$PKG/scripts/lib"

	APP="$PKG/handlers/session-start.sh"
	RESET_RAN="$BATS_TEST_TMPDIR/reset-ran"
}

# stub_reset REPLY -> replaces scripts/session-reset.sh with one that just
# touches RESET_RAN and prints REPLY, so a test can prove it ran without
# depending on session-reset.sh's own manifest side effects (a no-op on
# resume/fork regardless of whether this handler's guarantee holds).
stub_reset() {
	cat >"$PKG/scripts/session-reset.sh" <<EOF
#!/usr/bin/env bash
touch "$RESET_RAN"
$1
EOF
	chmod +x "$PKG/scripts/session-reset.sh"
}

stub_backfill() {
	cat >"$PKG/scripts/session-backfill.sh" <<EOF
#!/usr/bin/env bash
payload="\$(cat)"
printf '%s' "\$payload" >"$BATS_TEST_TMPDIR/backfill-payload"
calls_file="\$(jq -r '.calls_file // empty' <<<"\$payload")"
cp "\$calls_file" "$BATS_TEST_TMPDIR/backfill-calls"
EOF
	chmod +x "$PKG/scripts/session-backfill.sh"
}

write_transcript_call() {
	local transcript="$1"
	printf '%s\n' \
		'{"id":"root","type":"session"}' \
		'{"id":"assistant","parentId":"root","type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"call-1","name":"read","arguments":{"path":"/tmp/image.png"}}]}}' \
		>"$transcript"
}

@test "session-reset.sh runs even when diagram-guidance.sh exits non-zero" {
	stub_reset ""
	rm "$PKG/scripts/diagram-guidance.sh"
	printf '#!/usr/bin/env bash\nexit 1\n' >"$PKG/scripts/diagram-guidance.sh"
	chmod +x "$PKG/scripts/diagram-guidance.sh"

	run bash -c 'echo "{\"session_id\":\"s1\",\"cwd\":\"/tmp\",\"native\":{\"reason\":\"new\"}}" | "$0"' "$APP"
	[ "$status" -eq 0 ]
	[ -f "$RESET_RAN" ]
}

@test "session-reset.sh runs even when mktemp fails on a resume payload" {
	stub_reset ""
	ro_tmp="$BATS_TEST_TMPDIR/readonly"
	mkdir -p "$ro_tmp"
	chmod 000 "$ro_tmp"

	run env TMPDIR="$ro_tmp" bash -c \
		'echo "{\"session_id\":\"s1\",\"cwd\":\"/tmp\",\"native\":{\"reason\":\"resume\",\"session_file\":\"/nonexistent.jsonl\"}}" | "$0"' "$APP"
	chmod 755 "$ro_tmp" # restore before bats' own tmpdir cleanup runs
	[ "$status" -eq 0 ]
	[ -f "$RESET_RAN" ]
}

@test "guidance text still reaches the reply when reset also runs" {
	stub_reset ""
	run env TMUX=1 bash -c 'echo "{\"session_id\":\"s1\",\"cwd\":\"/tmp\",\"native\":{\"reason\":\"new\"}}" | "$0"' "$APP"
	[ "$status" -eq 0 ]
	[ -f "$RESET_RAN" ]
	[[ $output == *hookSpecificOutput* ]]
	[[ $output == *additionalContext* ]]
}

@test "empty payload is a clean no-op (no reset, no crash)" {
	stub_reset ""
	run bash -c 'printf "" | "$0"' "$APP"
	[ "$status" -eq 0 ]
	[ ! -f "$RESET_RAN" ]
}

@test "startup with transcript history backfills (pi -c continuation)" {
	stub_reset ""
	stub_backfill
	transcript="$BATS_TEST_TMPDIR/history.jsonl"
	write_transcript_call "$transcript"

	run bash -c 'jq -nc --arg sf "$1" '\''{session_id: "s1", cwd: "/tmp", native: {reason: "startup", session_file: $sf}}'\'' | "$0"' "$APP" "$transcript"
	[ "$status" -eq 0 ]
	[ -f "$BATS_TEST_TMPDIR/backfill-payload" ]
	run jq -e '.source == "startup" and .calls_file != ""' "$BATS_TEST_TMPDIR/backfill-payload"
	[ "$status" -eq 0 ]
	run jq -e '.tool_name == "read" and .tool_input == {path: "/tmp/image.png"} and .tool_response == {content: [""]} and .cwd == "/tmp" and .session_id == "s1"' "$BATS_TEST_TMPDIR/backfill-calls"
	[ "$status" -eq 0 ]
}

@test "empty transcript does not invoke backfill" {
	stub_reset ""
	stub_backfill
	transcript="$BATS_TEST_TMPDIR/empty.jsonl"
	: >"$transcript"

	run bash -c 'jq -nc --arg sf "$1" '\''{session_id: "s1", cwd: "/tmp", native: {reason: "new", session_file: $sf}}'\'' | "$0"' "$APP" "$transcript"
	[ "$status" -eq 0 ]
	[ ! -f "$BATS_TEST_TMPDIR/backfill-payload" ]
}

@test "resume and fork with transcript history backfill" {
	transcript="$BATS_TEST_TMPDIR/history.jsonl"
	write_transcript_call "$transcript"

	for reason in resume fork; do
		stub_reset ""
		stub_backfill
		rm -f "$BATS_TEST_TMPDIR/backfill-payload"
		run bash -c 'jq -nc --arg reason "$1" --arg sf "$2" '\''{session_id: "s1", cwd: "/tmp", native: {reason: $reason, session_file: $sf}}'\'' | "$0"' "$APP" "$reason" "$transcript"
		[ "$status" -eq 0 ]
		run jq -e '.source == "resume"' "$BATS_TEST_TMPDIR/backfill-payload"
		[ "$status" -eq 0 ]
	done
}
