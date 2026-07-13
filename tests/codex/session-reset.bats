#!/usr/bin/env bats
# shellcheck disable=SC2030,SC2031  # bats wraps each @test in a subshell; export is intentional
# shellcheck disable=SC2016  # the tmux stub writes literal $vars into a script on purpose

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	export PLUGIN_ROOT="$ROOT/adapters/codex/plugin"
	APP="$PLUGIN_ROOT/scripts/session-reset.sh"

	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	# Detach from any real tmux server the test runner sits in, so the GC sweep
	# has no live-pane list unless a test opts in with a tmux stub.
	unset TMUX
	MANIFEST="$AEYE_DIR/images/7.jsonl"
	mkdir -p "$AEYE_DIR/images"
	printf '{"type":"image","path":"/x.png"}\n' >"$MANIFEST"
}

# Run the hook with a stubbed `tmux list-panes` reporting LIVE (a space list of
# bare pane numbers) and $TMUX set, so the GC sweep trusts the live list.
run_with_live_panes() { # $1=live nums  $2=stdin json
	local stub="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$stub"
	{
		echo '#!/usr/bin/env bash'
		echo 'for p in '"$1"'; do echo "%$p"; done'
	} >"$stub/tmux"
	chmod +x "$stub/tmux"
	PATH="$stub:$PATH" TMUX="fake,1,0" bash "$APP" <<<"$2"
}

@test "source=startup removes the manifest" {
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}

@test "source=clear removes the manifest" {
	run bash "$APP" <<<'{"source":"clear"}'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}

@test "source=startup also removes the owner sidecar" {
	owner="$AEYE_DIR/images/7.owner"
	printf 'sess-A' >"$owner"
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
	[ ! -f "$owner" ]
}

@test "source=resume keeps the manifest" {
	run bash "$APP" <<<'{"source":"resume"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
}

@test "source=compact keeps the manifest" {
	run bash "$APP" <<<'{"source":"compact"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
}

@test "no key (no pane, no session) is a clean no-op" {
	unset TMUX_PANE
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
}

@test "outside tmux: keys by session id and removes that manifest" {
	unset TMUX_PANE
	sess_manifest="$AEYE_DIR/images/sess-abc.jsonl"
	printf '{"type":"image","path":"/y.png"}\n' >"$sess_manifest"
	run bash "$APP" <<<'{"source":"startup","session_id":"sess-abc"}'
	[ "$status" -eq 0 ]
	[ ! -f "$sess_manifest" ]
}

@test "missing manifest -> exit 0, no error" {
	rm -f "$MANIFEST"
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
}

@test "empty payload -> clean no-op" {
	run bash "$APP" <<<''
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
}

@test "resume leaves the per-pane manifest untouched (backfill is the sole writer)" {
	# SessionStart hooks run in parallel, so resume is handled entirely by
	# session-backfill — reset must not race it by clearing or re-stamping here.
	printf 'sess-old' >"$AEYE_DIR/images/7.owner"
	run bash "$APP" <<<'{"source":"resume","session_id":"sess-new"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
	# owner is left for backfill to re-stamp, not touched here
	[ "$(cat "$AEYE_DIR/images/7.owner")" = "sess-old" ]
}

@test "resume with no recorded owner leaves the manifest and stamps no owner" {
	run bash "$APP" <<<'{"source":"resume","session_id":"sess-A"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
	[ ! -f "$AEYE_DIR/images/7.owner" ]
}

@test "compact keeps the manifest and refreshes ownership (same session)" {
	printf 'sess-A' >"$AEYE_DIR/images/7.owner"
	run bash "$APP" <<<'{"source":"compact","session_id":"sess-A"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
	[ "$(cat "$AEYE_DIR/images/7.owner")" = "sess-A" ]
}

@test "compact with a foreign owner clears the manifest and restamps ownership" {
	printf 'sess-old' >"$AEYE_DIR/images/7.owner"
	run bash "$APP" <<<'{"source":"compact","session_id":"sess-new"}'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
	[ "$(cat "$AEYE_DIR/images/7.owner")" = "sess-new" ]
}

@test "startup stamps the owner for this session" {
	run bash "$APP" <<<'{"source":"startup","session_id":"sess-A"}'
	[ "$status" -eq 0 ]
	[ "$(cat "$AEYE_DIR/images/7.owner")" = "sess-A" ]
}

@test "startup with a foreign owner clears the manifest and restamps with the codex session id" {
	printf 'sess-old' >"$AEYE_DIR/images/7.owner"
	run bash "$APP" <<<'{"source":"startup","session_id":"sess-new"}'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
	[ "$(cat "$AEYE_DIR/images/7.owner")" = "sess-new" ]
}

@test "GC sweeps manifests for tmux panes that no longer exist" {
	printf 'sess-A' >"$AEYE_DIR/images/7.owner" # keep the current pane
	printf '{}\n' >"$AEYE_DIR/images/8.jsonl"   # dead pane
	printf '{}\n' >"$AEYE_DIR/images/9.jsonl"   # live pane
	run run_with_live_panes "7 9" '{"source":"resume","session_id":"sess-A"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]                  # current pane, kept
	[ -f "$AEYE_DIR/images/9.jsonl" ]   # live, kept
	[ ! -f "$AEYE_DIR/images/8.jsonl" ] # dead, swept
}

@test "GC ages out a stale session-keyed manifest but keeps a fresh one" {
	old="$AEYE_DIR/images/sess-old.jsonl"
	new="$AEYE_DIR/images/sess-fresh.jsonl"
	printf '{}\n' >"$old"
	printf '{}\n' >"$new"
	touch -d '8 days ago' "$old"
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
	[ ! -f "$old" ]
	[ -f "$new" ]
}

@test "GC sweeps an orphan owner sidecar for a dead pane (no matching jsonl)" {
	printf 'sess-A' >"$AEYE_DIR/images/7.owner"    # keep the current pane
	printf 'sess-dead' >"$AEYE_DIR/images/8.owner" # dead pane, no jsonl
	printf 'sess-live' >"$AEYE_DIR/images/9.owner" # live pane, no jsonl
	run run_with_live_panes "7 9" '{"source":"resume","session_id":"sess-A"}'
	[ "$status" -eq 0 ]
	[ -f "$AEYE_DIR/images/9.owner" ]   # live, kept
	[ ! -f "$AEYE_DIR/images/8.owner" ] # dead, swept
}
