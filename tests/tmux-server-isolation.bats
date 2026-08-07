#!/usr/bin/env bats
# Pane ids are per tmux server — every server numbers its panes from %0 — while
# IMAGES_DIR is shared machine-wide. Two servers must therefore never land on the
# same manifest, or one live session's images get clobbered or swept by the
# other's SessionStart. Regression for #185.
#
# Asserted behaviourally (discover the manifest each hook writes) rather than by
# hardcoding the key format, so these stay honest if the key changes again.

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7" # the same pane number on every server below
	unset CLAUDE_CODE_SESSION_ID
	IMAGES="$CLAUDE_STATUS_DIR/images"
	IMG="$BATS_TEST_TMPDIR/pic.png"
	printf 'x' >"$IMG"
	SCRIPTS="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/scripts"
	mkdir -p "$IMAGES"
	# A `tmux list-panes` stub reporting %7 live, so the GC sweep trusts its list.
	STUB="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB"
	printf '#!/usr/bin/env bash\necho "%%7"\n' >"$STUB/tmux"
	chmod +x "$STUB/tmux"
}

# capture SERVER_PID -> record one image for pane %7 as seen from that server.
capture() {
	local payload
	payload="$(sed -e "s#IMGPATH#$IMG#g" -e "s#CWDPATH#$BATS_TEST_TMPDIR#g" \
		"$BATS_TEST_DIRNAME/fixtures/hook-read-image.json")"
	PATH="$STUB:$PATH" TMUX="fake,$1,0" CLAUDE_CODE_SESSION_ID="sess-$1" \
		bash "$SCRIPTS/images.sh" <<<"$payload"
}

# start_session SERVER_PID -> a fresh SessionStart on pane %7 of that server.
start_session() {
	PATH="$STUB:$PATH" TMUX="fake,$1,0" CLAUDE_CODE_SESSION_ID="sess-$1" \
		bash "$SCRIPTS/session-reset.sh" <<<'{"source":"startup"}'
}

@test "a session starting on another tmux server does not wipe this one's manifest" {
	# $$ (this bats process) stands in for a second, still-running tmux server.
	capture "$$"
	local mine
	mine="$(ls "$IMAGES"/*.jsonl)"
	[ "$(wc -l <"$mine")" -eq 1 ]

	start_session 4242 # same pane number, different server

	[ -f "$mine" ]
	[ "$(wc -l <"$mine")" -eq 1 ]
}

@test "each tmux server keeps its own manifest for the same pane number" {
	capture 4242
	capture 5555
	[ "$(ls "$IMAGES"/*.jsonl | wc -l)" -eq 2 ]
}

@test "the GC sweep reaps a manifest whose tmux server is gone" {
	sleep 60 &
	local dead=$!
	kill "$dead"
	wait "$dead" 2>/dev/null || true
	capture "$dead"
	local stale
	stale="$(ls "$IMAGES"/*.jsonl)"

	start_session 4242

	[ ! -f "$stale" ]
}
