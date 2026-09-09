#!/usr/bin/env bats
# shellcheck disable=SC2030,SC2031  # bats wraps each @test in a subshell; export is intentional
# shellcheck disable=SC2016  # the tmux stub writes literal $vars into a script on purpose

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	unset CLAUDE_CODE_SESSION_ID
	# Pin a tmux server pid: pane ids are per-server, so the manifest key carries
	# it. The socket is unreachable, so the GC sweep still gets no live-pane list
	# unless a test opts in with a tmux stub.
	export TMUX="fake,4242,0"
	APP="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/scripts/session-reset.sh"
	MANIFEST="$CLAUDE_STATUS_DIR/images/4242-7.jsonl"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
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
	PATH="$stub:$PATH" TMUX="fake,4242,0" bash "$APP" <<<"$2"
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
	owner="$CLAUDE_STATUS_DIR/images/4242-7.owner"
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
	export CLAUDE_CODE_SESSION_ID="sess-abc"
	sess_manifest="$CLAUDE_STATUS_DIR/images/sess-abc.jsonl"
	printf '{"type":"image","path":"/y.png"}\n' >"$sess_manifest"
	run bash "$APP" <<<'{"source":"startup"}'
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
	export CLAUDE_CODE_SESSION_ID="sess-new"
	printf 'sess-old' >"$CLAUDE_STATUS_DIR/images/4242-7.owner"
	run bash "$APP" <<<'{"source":"resume"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
	# owner is left for backfill to re-stamp, not touched here
	[ "$(cat "$CLAUDE_STATUS_DIR/images/4242-7.owner")" = "sess-old" ]
}

@test "resume with no recorded owner leaves the manifest and stamps no owner" {
	export CLAUDE_CODE_SESSION_ID="sess-A"
	run bash "$APP" <<<'{"source":"resume"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
	[ ! -f "$CLAUDE_STATUS_DIR/images/4242-7.owner" ]
}

@test "compact keeps the manifest and refreshes ownership (same session)" {
	export CLAUDE_CODE_SESSION_ID="sess-A"
	printf 'sess-A' >"$CLAUDE_STATUS_DIR/images/4242-7.owner"
	run bash "$APP" <<<'{"source":"compact"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
	[ "$(cat "$CLAUDE_STATUS_DIR/images/4242-7.owner")" = "sess-A" ]
}

@test "startup stamps the owner for this session" {
	export CLAUDE_CODE_SESSION_ID="sess-A"
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
	[ "$(cat "$CLAUDE_STATUS_DIR/images/4242-7.owner")" = "sess-A" ]
}

@test "GC sweeps manifests for tmux panes that no longer exist" {
	export CLAUDE_CODE_SESSION_ID="sess-A"
	printf 'sess-A' >"$CLAUDE_STATUS_DIR/images/4242-7.owner" # keep the current pane
	printf '{}\n' >"$CLAUDE_STATUS_DIR/images/4242-8.jsonl"   # dead pane
	printf '{}\n' >"$CLAUDE_STATUS_DIR/images/4242-9.jsonl"   # live pane
	run run_with_live_panes "7 9" '{"source":"resume"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]                                # current pane, kept
	[ -f "$CLAUDE_STATUS_DIR/images/4242-9.jsonl" ]   # live, kept
	[ ! -f "$CLAUDE_STATUS_DIR/images/4242-8.jsonl" ] # dead, swept
}

@test "GC ages out a stale session-keyed manifest but keeps a fresh one" {
	old="$CLAUDE_STATUS_DIR/images/sess-old.jsonl"
	new="$CLAUDE_STATUS_DIR/images/sess-fresh.jsonl"
	printf '{}\n' >"$old"
	printf '{}\n' >"$new"
	touch -d '8 days ago' "$old"
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
	[ ! -f "$old" ]
	[ -f "$new" ]
}

@test "GC sweeps an orphan owner sidecar for a dead pane (no matching jsonl)" {
	export CLAUDE_CODE_SESSION_ID="sess-A"
	printf 'sess-A' >"$CLAUDE_STATUS_DIR/images/4242-7.owner"    # keep the current pane
	printf 'sess-dead' >"$CLAUDE_STATUS_DIR/images/4242-8.owner" # dead pane, no jsonl
	printf 'sess-live' >"$CLAUDE_STATUS_DIR/images/4242-9.owner" # live pane, no jsonl
	run run_with_live_panes "7 9" '{"source":"resume"}'
	[ "$status" -eq 0 ]
	[ -f "$CLAUDE_STATUS_DIR/images/4242-9.owner" ]   # live, kept
	[ ! -f "$CLAUDE_STATUS_DIR/images/4242-8.owner" ] # dead, swept
}

# --- Nested-agent guard (#234) ---
# A nested `claude` in the same pane resolves the SAME manifest key (it inherits
# $TMUX_PANE) and fires SessionStart(startup). Without a liveness signal the
# clear below wipes the outer session's carousel. `.ownerpid` supplies it.

@test "startup leaves a LIVE owner's manifest and ownership alone (nested session)" {
	export CLAUDE_CODE_SESSION_ID="sess-nested"
	printf 'sess-outer' >"$CLAUDE_STATUS_DIR/images/4242-7.owner"
	printf '%s' "$$" >"$CLAUDE_STATUS_DIR/images/4242-7.ownerpid" # bats pid: alive
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
	[ "$(cat "$CLAUDE_STATUS_DIR/images/4242-7.owner")" = "sess-outer" ]
}

@test "startup clears when the recorded owner pid is dead (pane genuinely reused)" {
	export CLAUDE_CODE_SESSION_ID="sess-new"
	printf 'sess-gone' >"$CLAUDE_STATUS_DIR/images/4242-7.owner"
	printf '999999999' >"$CLAUDE_STATUS_DIR/images/4242-7.ownerpid"
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
	[ "$(cat "$CLAUDE_STATUS_DIR/images/4242-7.owner")" = "sess-new" ]
}

@test "startup clears when no owner pid was ever recorded (pre-upgrade state)" {
	export CLAUDE_CODE_SESSION_ID="sess-new"
	printf 'sess-old' >"$CLAUDE_STATUS_DIR/images/4242-7.owner"
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}

# Stub `tmux display-message -p ... '#{pane_pid}'` with a pid that really is an
# ancestor of the hook, so owner_pid_self's walk has something to find. The
# stand-in pane shell is this test process's own parent, which makes the walk
# return the test process — a real ancestor that outlives the hook, so the
# recorded pid is still checkable with kill -0 after the run.
stub_tmux_pane_shell() { # $1 = pid to report as the pane shell
	local stub="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$stub"
	{
		echo '#!/usr/bin/env bash'
		echo '[[ $1 == display-message ]] || exit 1'
		echo "printf '%s' '$1'"
	} >"$stub/tmux"
	chmod +x "$stub/tmux"
	printf '%s' "$stub"
}

@test "startup records a LIVE owner pid when it claims the pane" {
	export CLAUDE_CODE_SESSION_ID="sess-A"
	stub="$(stub_tmux_pane_shell "$PPID")"
	PATH="$stub:$PATH" run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
	pid="$(cat "$CLAUDE_STATUS_DIR/images/4242-7.ownerpid")"
	kill -0 "$pid"
}

@test "startup records no owner pid when the pane shell cannot be resolved" {
	# Unreachable tmux socket (the setup default): the guard must degrade to the
	# unguarded clear rather than record a pid it could not verify.
	export CLAUDE_CODE_SESSION_ID="sess-A"
	run bash "$APP" <<<'{"source":"startup"}'
	[ "$status" -eq 0 ]
	[ ! -f "$CLAUDE_STATUS_DIR/images/4242-7.ownerpid" ]
}

@test "clear_pane drops the owner pid too, so the next claim is unguarded" {
	export CLAUDE_CODE_SESSION_ID="sess-new"
	printf 'sess-gone' >"$CLAUDE_STATUS_DIR/images/4242-7.owner"
	printf '999999999' >"$CLAUDE_STATUS_DIR/images/4242-7.ownerpid"
	run bash "$APP" <<<'{"source":"clear"}'
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
	[ "$(cat "$CLAUDE_STATUS_DIR/images/4242-7.ownerpid")" != "999999999" ]
}
