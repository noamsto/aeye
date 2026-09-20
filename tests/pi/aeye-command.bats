#!/usr/bin/env bats
# shellcheck disable=SC2016  # the injection-attempt fixture's literal $(...) must not expand

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	APP="$ROOT/adapters/pi/commands/aeye.sh"
}

@test "re-exports HOOKYARD_SESSION_ID as AEYE_SESSION_ID for the spawned toggle" {
	# pi's bridge spawns a command exec as a subprocess, which cannot mutate
	# pi's own process env — so this re-export is the only way the toggle sees
	# the session id gallery_render.go's ownerCheck needs. Losing this line
	# silently makes the stale-pane guard inert, with no error anywhere.
	run env HOOKYARD_SESSION_ID="live-session-42" AEYE_TOGGLE="env" bash "$APP"
	[ "$status" -eq 0 ]
	[[ $output == *"AEYE_SESSION_ID=live-session-42"* ]]
}

@test "no HOOKYARD_SESSION_ID -> AEYE_SESSION_ID is exported empty, not unset" {
	run env -u HOOKYARD_SESSION_ID AEYE_TOGGLE="env" bash "$APP"
	[ "$status" -eq 0 ]
	aeye_line="$(grep '^AEYE_SESSION_ID=' <<<"$output")"
	[ "$aeye_line" = "AEYE_SESSION_ID=" ]
}

@test "a session id outside the safe charset degrades to empty rather than reaching AEYE_SESSION_ID" {
	# HOOKYARD_SESSION_ID itself stays in the inherited env unchanged (this
	# script only sanitizes the copy it re-exports as AEYE_SESSION_ID), so
	# assert on that one line specifically rather than the whole `env` dump.
	run env HOOKYARD_SESSION_ID='$(touch /tmp/pwned); evil' AEYE_TOGGLE="env" bash "$APP"
	[ "$status" -eq 0 ]
	aeye_line="$(grep '^AEYE_SESSION_ID=' <<<"$output")"
	[ "$aeye_line" = "AEYE_SESSION_ID=" ]
}

@test "a UUID-shaped session id passes through unchanged" {
	run env HOOKYARD_SESSION_ID="01a0b059-29ee-7135-9365-a41db4239eb6" AEYE_TOGGLE="env" bash "$APP"
	[ "$status" -eq 0 ]
	[[ $output == *"AEYE_SESSION_ID=01a0b059-29ee-7135-9365-a41db4239eb6"* ]]
}

@test "defaults to tmux-claude-images when AEYE_TOGGLE is unset" {
	# Put a stub named tmux-claude-images first on PATH so the default
	# resolves to it, rather than assuming the real toggle is absent.
	stub_dir="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$stub_dir"
	printf '#!/usr/bin/env bash\necho stub-toggle-ran\n' >"$stub_dir/tmux-claude-images"
	chmod +x "$stub_dir/tmux-claude-images"

	run env -u AEYE_TOGGLE PATH="$stub_dir:$PATH" HOOKYARD_SESSION_ID="s1" bash "$APP"
	[ "$status" -eq 0 ]
	[[ $output == *"stub-toggle-ran"* ]]
}
