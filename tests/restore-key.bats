#!/usr/bin/env bats
# shellcheck disable=SC2030,SC2031  # bats runs each @test in a subshell; export is intentional
# A tmux server restore changes the manifest key (new server pid, new pane id)
# but not the agent session id, so the restored pane must find the manifest the
# previous server left behind (#402).

setup() {
	CORE="$(dirname "$BATS_TEST_DIRNAME")/adapters/core"
	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	IMAGES="$AEYE_DIR/images"
	mkdir -p "$IMAGES"
	# shellcheck source=/dev/null
	source "$CORE/manifest-lifecycle.sh"
	resolve_state_dirs
	sleep 60 &
	DEAD=$!
	kill "$DEAD"
	wait "$DEAD" 2>/dev/null || true
}

# seed KEY SESSION [PID] -> a prior manifest for KEY owned by SESSION.
seed() {
	echo '{"type":"image","path":"/x.png","source":"d2"}' >"$IMAGES/$1.jsonl"
	printf '%s' "$2" >"$IMAGES/$1.owner"
	[[ -n ${3:-} ]] && printf '%s' "$3" >"$IMAGES/$1.ownerpid"
	return 0
}

@test "a restored pane under a new server pid adopts the previous manifest" {
	seed 111-5 sess-A "$DEAD"
	export TMUX="x,222,0" TMUX_PANE="%9"
	key="$(resolve_pane_key sess-A)"
	[ "$key" = 222-9 ]

	owner_selfheal "$key" sess-A

	[ -s "$IMAGES/222-9.jsonl" ]
	[ "$(<"$IMAGES/222-9.owner")" = sess-A ]
	[ ! -e "$IMAGES/111-5.jsonl" ]
	[ ! -e "$IMAGES/111-5.owner" ]
}

@test "adoption works when the old key recorded no owner pid" {
	seed 111-5 sess-A
	owner_selfheal 222-9 sess-A
	[ -s "$IMAGES/222-9.jsonl" ]
}

@test "a manifest of a still-running owner is not adopted" {
	seed 111-5 sess-A "$$"
	owner_selfheal 222-9 sess-A
	[ ! -e "$IMAGES/222-9.jsonl" ]
	[ -s "$IMAGES/111-5.jsonl" ]
}

@test "another session's manifest is not adopted" {
	seed 111-5 sess-B "$DEAD"
	owner_selfheal 222-9 sess-A
	[ ! -e "$IMAGES/222-9.jsonl" ]
	[ -s "$IMAGES/111-5.jsonl" ]
}

@test "an existing manifest on the new key is left alone" {
	seed 111-5 sess-A "$DEAD"
	seed 222-9 sess-A
	echo '{"type":"image","path":"/y.png","source":"d2"}' >>"$IMAGES/222-9.jsonl"
	owner_selfheal 222-9 sess-A
	[ "$(wc -l <"$IMAGES/222-9.jsonl")" -eq 2 ]
	[ -s "$IMAGES/111-5.jsonl" ]
}

@test "the freshest of several old keys is adopted" {
	seed 111-5 sess-A "$DEAD"
	seed 111-6 sess-A "$DEAD"
	touch -d '2 hours ago' "$IMAGES/111-5.jsonl"
	owner_selfheal 222-9 sess-A
	[ -s "$IMAGES/222-9.jsonl" ]
	[ -s "$IMAGES/111-5.jsonl" ]
	[ ! -e "$IMAGES/111-6.jsonl" ]
}

@test "gc_sweep keeps a dead server's key under the TTL and drops it past the TTL" {
	export TMUX="x,222,0"
	seed 111-5 sess-A "$DEAD"
	gc_sweep 222-9 "9"
	[ -s "$IMAGES/111-5.jsonl" ]

	touch -d '8 days ago' "$IMAGES/111-5.jsonl" "$IMAGES/111-5.owner"
	gc_sweep 222-9 "9"
	[ ! -e "$IMAGES/111-5.jsonl" ]
	[ ! -e "$IMAGES/111-5.owner" ]
}
