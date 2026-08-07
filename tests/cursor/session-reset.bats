#!/usr/bin/env bats
# shellcheck disable=SC2030,SC2031  # bats wraps each @test in a subshell; export is intentional
# shellcheck disable=SC2016  # the tmux stub writes literal $vars into a script on purpose

# Cursor sessionStart payloads: no `source`, workspace_roots instead of cwd.
payload() { # $1=conversation_id (optional)
	local cid="${1:-}"
	if [[ -n $cid ]]; then
		jq -nc --arg c "$cid" \
			'{conversation_id:$c,session_id:$c,hook_event_name:"sessionStart",workspace_roots:["/tmp/ws"],transcript_path:null}'
	else
		jq -nc '{hook_event_name:"sessionStart",workspace_roots:["/tmp/ws"],transcript_path:null}'
	fi
}

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	export PLUGIN_ROOT="$ROOT/adapters/cursor"
	APP="$PLUGIN_ROOT/scripts/session-reset.sh"

	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	# Pin a tmux server pid: pane ids are per-server, so the manifest key carries
	# it. The socket is unreachable, so the GC sweep still gets no live-pane list
	# unless a test opts in with a tmux stub.
	export TMUX="fake,4242,0"
	MANIFEST="$AEYE_DIR/images/4242-7.jsonl"
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
	PATH="$stub:$PATH" TMUX="fake,4242,0" bash "$APP" <<<"$2"
}

@test "sessionStart removes the manifest" {
	run bash "$APP" <<<"$(payload)"
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}

@test "sessionStart also removes the owner sidecar" {
	owner="$AEYE_DIR/images/4242-7.owner"
	printf 'sess-A' >"$owner"
	run bash "$APP" <<<"$(payload)"
	[ "$status" -eq 0 ]
	[ ! -f "$owner" ]
}

@test "no key (no pane, no session) is a clean no-op" {
	unset TMUX_PANE
	run bash "$APP" <<<"$(payload)"
	[ "$status" -eq 0 ]
}

@test "outside tmux: keys by conversation id and removes that manifest" {
	unset TMUX_PANE
	sess_manifest="$AEYE_DIR/images/sess-abc.jsonl"
	printf '{"type":"image","path":"/y.png"}\n' >"$sess_manifest"
	run bash "$APP" <<<"$(payload sess-abc)"
	[ "$status" -eq 0 ]
	[ ! -f "$sess_manifest" ]
}

@test "missing manifest -> exit 0, no error" {
	rm -f "$MANIFEST"
	run bash "$APP" <<<"$(payload)"
	[ "$status" -eq 0 ]
}

@test "empty payload -> clean no-op" {
	run bash "$APP" <<<''
	[ "$status" -eq 0 ]
	[ -f "$MANIFEST" ]
}

@test "sessionStart stamps the owner with the conversation id" {
	run bash "$APP" <<<"$(payload sess-A)"
	[ "$status" -eq 0 ]
	[ "$(cat "$AEYE_DIR/images/4242-7.owner")" = "sess-A" ]
}

@test "sessionStart with a foreign owner clears the manifest and restamps" {
	printf 'sess-old' >"$AEYE_DIR/images/4242-7.owner"
	run bash "$APP" <<<"$(payload sess-new)"
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
	[ "$(cat "$AEYE_DIR/images/4242-7.owner")" = "sess-new" ]
}

@test "prefers conversation_id over session_id for the owner stamp" {
	run bash "$APP" <<<'{"conversation_id":"conv-1","session_id":"sess-other","hook_event_name":"sessionStart","workspace_roots":["/tmp/ws"]}'
	[ "$status" -eq 0 ]
	[ "$(cat "$AEYE_DIR/images/4242-7.owner")" = "conv-1" ]
}

@test "GC sweeps manifests for tmux panes that no longer exist" {
	printf 'sess-A' >"$AEYE_DIR/images/4242-7.owner"
	printf '{}\n' >"$AEYE_DIR/images/4242-8.jsonl" # dead pane
	printf '{}\n' >"$AEYE_DIR/images/4242-9.jsonl" # live pane
	run run_with_live_panes "7 9" "$(payload sess-A)"
	[ "$status" -eq 0 ]
	# Current pane always cleared (startup semantics; no resume branch).
	[ ! -f "$MANIFEST" ]
	[ "$(cat "$AEYE_DIR/images/4242-7.owner")" = "sess-A" ]
	[ -f "$AEYE_DIR/images/4242-9.jsonl" ]   # live, kept
	[ ! -f "$AEYE_DIR/images/4242-8.jsonl" ] # dead, swept
}

@test "GC ages out a stale session-keyed manifest but keeps a fresh one" {
	old="$AEYE_DIR/images/sess-old.jsonl"
	new="$AEYE_DIR/images/sess-fresh.jsonl"
	printf '{}\n' >"$old"
	printf '{}\n' >"$new"
	touch -d '8 days ago' "$old"
	run bash "$APP" <<<"$(payload)"
	[ "$status" -eq 0 ]
	[ ! -f "$old" ]
	[ -f "$new" ]
}

@test "GC sweeps an orphan owner sidecar for a dead pane (no matching jsonl)" {
	printf 'sess-A' >"$AEYE_DIR/images/4242-7.owner"
	printf 'sess-dead' >"$AEYE_DIR/images/4242-8.owner" # dead pane, no jsonl
	printf 'sess-live' >"$AEYE_DIR/images/4242-9.owner" # live pane, no jsonl
	run run_with_live_panes "7 9" "$(payload sess-A)"
	[ "$status" -eq 0 ]
	[ -f "$AEYE_DIR/images/4242-9.owner" ]   # live, kept
	[ ! -f "$AEYE_DIR/images/4242-8.owner" ] # dead, swept
	[ "$(cat "$AEYE_DIR/images/4242-7.owner")" = "sess-A" ]
}
