#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	export PLUGIN_ROOT="$ROOT/adapters/codex/plugin"
	APP="$PLUGIN_ROOT/scripts/images.sh"
	FIXTURES="$ROOT/tests/fixtures/codex"

	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	unset CODEX_SESSION_ID 2>/dev/null || true
	MANIFEST="$AEYE_DIR/images/7.jsonl"

	# The toggle is only invoked by diagrams.sh, but stub it anyway so a stray
	# --ensure-open call in either script never touches the real tmux state.
	export AEYE_TOGGLE=true
}

run_app() { # $1 = fixture name, remaining args are sed "s#FROM#TO#g" pairs
	local fixture="$1"
	shift
	local expr=()
	while [[ $# -gt 0 ]]; do
		expr+=(-e "s#$1#$2#g")
		shift 2
	done
	sed "${expr[@]}" "$FIXTURES/$fixture" | bash "$APP"
}

@test "apply_patch adding a real fixture png appends one image line" {
	PNG="$FIXTURES/bar.png"
	run_app apply-patch-png.json PNGPATH "$PNG"
	[ -f "$MANIFEST" ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	run jq -r '.path' "$MANIFEST"
	[ "$output" = "$PNG" ]
	run jq -r '.type' "$MANIFEST"
	[ "$output" = "image" ]
	run jq -r '.source' "$MANIFEST"
	[ "$output" = "apply_patch" ]
}

@test "view_image of a real fixture png appends one image line" {
	PNG="$FIXTURES/shot.png"
	run_app view-image.json PNGPATH "$PNG"
	[ -f "$MANIFEST" ]
	run jq -r '.path' "$MANIFEST"
	[ "$output" = "$PNG" ]
	run jq -r '.source' "$MANIFEST"
	[ "$output" = "view_image" ]
}

@test "apply_patch adding a .d2 appends nothing (that's diagrams.sh's job)" {
	D2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$D2"
	run_app apply-patch-d2.json D2PATH "$D2"
	[ ! -f "$MANIFEST" ]
}

@test "no touched paths -> clean no-op, no state dir created" {
	run_app apply-patch-d2.json D2PATH "$BATS_TEST_TMPDIR/nope.d2"
	[ ! -f "$MANIFEST" ]
	[ ! -e "$AEYE_DIR/images/7.lock" ]
	[ ! -e "$AEYE_DIR/images/7.owner" ]
}

@test "keyed by TMUX_PANE, not the codex session id" {
	PNG="$FIXTURES/bar.png"
	payload="$(sed "s#PNGPATH#$PNG#g" "$FIXTURES/apply-patch-png.json")"
	payload="$(jq -c '. + {session_id:"sess-abc"}' <<<"$payload")"
	echo "$payload" | bash "$APP"
	[ -f "$MANIFEST" ]
	[ ! -f "$AEYE_DIR/images/sess-abc.jsonl" ]
}
