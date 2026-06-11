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
	cat >"$STUB_BIN/tmux-claude-images" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$TOGGLE_LOG"
STUB
	chmod +x "$STUB_BIN/d2" "$STUB_BIN/resvg" "$STUB_BIN/tmux-claude-images"
	export TOGGLE_LOG="$BATS_TEST_TMPDIR/toggle.log"
	: >"$TOGGLE_LOG"
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

@test "duplicate write of identical .d2 -> one manifest line" {
	run_app hook-write-d2.json
	run_app hook-write-d2.json
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
}

@test "editing the .d2 (new content) -> a second manifest line" {
	run_app hook-write-d2.json
	printf 'a -> b -> c\n' >"$DOTD2"
	run_app hook-edit-d2.json
	run wc -l <"$MANIFEST"
	[ "$output" -eq 2 ]
}

@test "a non-.d2 file_path is ignored (fast-bail)" {
	# reuse the image fixture: file_path is a .png, not a .d2
	IMG="$BATS_TEST_TMPDIR/pic.png"
	printf 'x' >"$IMG"
	sed "s#IMGPATH#$IMG#g" "$BATS_TEST_DIRNAME/fixtures/hook-write-image.json" | bash "$APP"
	[ ! -f "$MANIFEST" ]
}

@test "malformed d2 -> skip, log to render-errors.log, no manifest line" {
	# d2 stub that fails
	cat >"$STUB_BIN/d2" <<'STUB'
#!/usr/bin/env bash
echo "parse error" >&2
exit 1
STUB
	chmod +x "$STUB_BIN/d2"
	run run_app hook-write-d2.json
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
	[ -f "$DIAGRAMS/render-errors.log" ]
}

@test "d2 not on PATH -> clean no-op" {
	rm -f "$STUB_BIN/d2"
	run run_app hook-write-d2.json
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}

@test "resvg failure -> skip, log to render-errors.log, no manifest line" {
	cat >"$STUB_BIN/resvg" <<'STUB'
#!/usr/bin/env bash
echo "resvg boom" >&2
exit 1
STUB
	chmod +x "$STUB_BIN/resvg"
	run run_app hook-write-d2.json
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
	[ -f "$DIAGRAMS/render-errors.log" ]
}

@test "first diagram of a session opens the carousel once" {
	run_app hook-write-d2.json
	run grep -c -- '--ensure-open' "$TOGGLE_LOG"
	[ "$output" -eq 1 ]
	[ -f "$CLAUDE_STATUS_DIR/images/7.opened" ]
}

@test "second (new) diagram does NOT reopen the carousel" {
	run_app hook-write-d2.json
	printf 'a -> b -> c\n' >"$DOTD2"
	run_app hook-edit-d2.json
	run grep -c -- '--ensure-open' "$TOGGLE_LOG"
	[ "$output" -eq 1 ]
}
