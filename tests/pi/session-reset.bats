#!/usr/bin/env bats
# shellcheck disable=SC2030,SC2031  # bats wraps each @test in a subshell; export is intentional
# shellcheck disable=SC2016  # the tmux stub writes literal $vars into a script on purpose

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	export PLUGIN_ROOT="$ROOT/adapters/pi"
	APP="$PLUGIN_ROOT/scripts/session-reset.sh"

	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	export TMUX="fake,4242,0" # pin a tmux server pid so the key carries it
	MANIFEST="$AEYE_DIR/images/4242-7.jsonl"
	mkdir -p "$AEYE_DIR/images"
	printf '{"type":"image","path":"/x.png"}\n' >"$MANIFEST"
}

# Run the hook with a stubbed `tmux list-panes` reporting LIVE bare pane numbers.
run_with_live_panes() { # $1=live nums  $2=stdin json
	local stub="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$stub"
	{
		echo '#!/usr/bin/env bash'
		echo 'for p in '"$1"'; do echo "%$p"; done'
	} >"$stub/tmux"
	chmod +x "$stub/tmux"
	PATH="$stub:$PATH" TMUX="fake,4242,0" bash "$APP" <<<"$2"
}

@test "startup with no owner clears the pane manifest and stamps ownership" {
	run bash "$APP" <<<'{"source":"startup","session_id":"sess-A"}'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
	[ "$(cat "$AEYE_DIR/images/4242-7.owner")" = "sess-A" ]
}

@test "startup with a foreign owner clears the manifest and restamps" {
	printf 'sess-old' >"$AEYE_DIR/images/4242-7.owner"
	run bash "$APP" <<<'{"source":"startup","session_id":"sess-new"}'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
	[ "$(cat "$AEYE_DIR/images/4242-7.owner")" = "sess-new" ]
}

@test "startup with the same owner keeps the manifest (pi -c continuation)" {
	printf 'sess-A' >"$AEYE_DIR/images/4242-7.owner"
	run bash "$APP" <<<'{"source":"startup","session_id":"sess-A"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
	[ "$(cat "$AEYE_DIR/images/4242-7.owner")" = "sess-A" ]
}

@test "reload keeps the manifest (same session)" {
	printf 'sess-A' >"$AEYE_DIR/images/4242-7.owner"
	run bash "$APP" <<<'{"source":"reload","session_id":"sess-A"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
}

@test "resume leaves the manifest and owner untouched (backfill is the sole writer)" {
	printf 'sess-old' >"$AEYE_DIR/images/4242-7.owner"
	run bash "$APP" <<<'{"source":"resume","session_id":"sess-new"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
	[ "$(cat "$AEYE_DIR/images/4242-7.owner")" = "sess-old" ]
}

@test "fork leaves the manifest and owner untouched (backfill is the sole writer)" {
	printf 'sess-old' >"$AEYE_DIR/images/4242-7.owner"
	run bash "$APP" <<<'{"source":"fork","session_id":"sess-new"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
	[ "$(cat "$AEYE_DIR/images/4242-7.owner")" = "sess-old" ]
}

@test "no key (no pane, no session) is a clean no-op" {
	unset TMUX_PANE
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
}

@test "outside tmux: keys by session id and clears that manifest" {
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

@test "GC sweeps manifests for tmux panes that no longer exist" {
	printf 'sess-A' >"$AEYE_DIR/images/4242-7.owner" # keep the current pane
	printf '{}\n' >"$AEYE_DIR/images/4242-8.jsonl"   # dead pane
	printf '{}\n' >"$AEYE_DIR/images/4242-9.jsonl"   # live pane
	run run_with_live_panes "7 9" '{"source":"resume","session_id":"sess-A"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]                       # current pane, kept
	[ -f "$AEYE_DIR/images/4242-9.jsonl" ]   # live, kept
	[ ! -f "$AEYE_DIR/images/4242-8.jsonl" ] # dead, swept
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
