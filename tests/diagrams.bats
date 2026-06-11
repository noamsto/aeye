#!/usr/bin/env bats

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	unset CLAUDE_CODE_SESSION_ID
	MANIFEST="$CLAUDE_STATUS_DIR/images/7.jsonl"
	DIAGRAMS="$CLAUDE_STATUS_DIR/images/diagrams"
	DOTD2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$DOTD2"
	APP="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/scripts/diagrams.sh"

	# Stub d2 + resvg so tests are hermetic and fast.
	# d2 <in> <out.svg> writes a fake svg; resvg <in.svg> <out.png> writes a fake png.
	STUB_BIN="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB_BIN"
	cat >"$STUB_BIN/d2" <<'STUB'
#!/usr/bin/env bash
printf '<svg/>' >"$2"
STUB
	cat >"$STUB_BIN/resvg" <<'STUB'
#!/usr/bin/env bash
printf 'PNG' >"$2"
STUB
	chmod +x "$STUB_BIN/d2" "$STUB_BIN/resvg"
	export PATH="$STUB_BIN:$PATH"
}

run_app() { # $1 = fixture name
	sed "s#DOTD2#$DOTD2#g" "$BATS_TEST_DIRNAME/fixtures/$1" | bash "$APP"
}

@test "a .d2 Write renders a png and appends one manifest line" {
	run_app hook-write-d2.json
	[ -f "$MANIFEST" ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	run jq -r '.source' "$MANIFEST"
	[ "$output" = "d2" ]
	# manifest path points at a rendered png under images/diagrams/
	png="$(jq -r '.path' "$MANIFEST")"
	[ -f "$png" ]
	[[ $png == "$DIAGRAMS"/*.png ]]
}

@test "a relative .d2 file_path is resolved against cwd" {
	mkdir -p "$BATS_TEST_TMPDIR/proj/sub"
	printf 'a -> b\n' >"$BATS_TEST_TMPDIR/proj/sub/flow.d2"
	sed "s#CWD#$BATS_TEST_TMPDIR/proj#g" "$BATS_TEST_DIRNAME/fixtures/hook-write-d2-relative.json" | bash "$APP"
	[ -f "$MANIFEST" ]
	run jq -r '.source' "$MANIFEST"
	[ "$output" = "d2" ]
}
