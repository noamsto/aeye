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

	# The hook now shells to a single `aeye render-diagram IN OUT`; the whole
	# d2-compile / fix-fonts / contrast / resvg pipeline lives inside the binary
	# (covered by Go tests). Stub aeye to write a fake png + sibling svg, log its
	# args, honor AEYE_RENDER_FAIL for the failure path, and emit a
	# <foreignObject> svg for |md sources so the hook's markdown guard can fire.
	STUB_BIN="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB_BIN"
	cat >"$STUB_BIN/aeye" <<'STUB'
#!/usr/bin/env bash
[[ ${1:-} == render-diagram ]] || exit 0
echo "$*" >>"$RENDER_LOG"
[[ -n ${AEYE_RENDER_FAIL:-} ]] && {
	echo "render boom" >&2
	exit 1
}
in="$2"
out="$3"
if grep -q '|md' "$in" 2>/dev/null; then
	printf '<svg><foreignObject><div>md</div></foreignObject></svg>' >"${out%.png}.svg"
else
	printf '<svg/>' >"${out%.png}.svg"
fi
printf 'PNG' >"$out"
STUB
	cat >"$STUB_BIN/tmux-claude-images" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$TOGGLE_LOG"
STUB
	chmod +x "$STUB_BIN/aeye" "$STUB_BIN/tmux-claude-images"
	export TOGGLE_LOG="$BATS_TEST_TMPDIR/toggle.log"
	export RENDER_LOG="$BATS_TEST_TMPDIR/render.log"
	: >"$TOGGLE_LOG"
	: >"$RENDER_LOG"
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

@test "the hook invokes aeye render-diagram for both theme variants" {
	run_app hook-write-d2.json
	png="$(jq -r '.path' "$MANIFEST")"
	# both themes render off one source; the manifest records the dark variant
	run grep -cx "render-diagram $DOTD2 ${png%-dark.png}-light.png" "$RENDER_LOG"
	[ "$output" -eq 1 ]
	run grep -cx "render-diagram $DOTD2 $png" "$RENDER_LOG"
	[ "$output" -eq 1 ]
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

@test "editing the .d2 supersedes the prior render: one line, old files pruned" {
	run_app hook-write-d2.json
	old="$(jq -r '.path' "$MANIFEST")"
	printf 'a -> b -> c\n' >"$DOTD2"
	run_app hook-edit-d2.json
	new="$(jq -r '.path' "$MANIFEST")"
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	[ "$old" != "$new" ]
	# the new render is shown; the superseded one is gone from disk (both themes)
	[ -f "$new" ]
	[ ! -f "$old" ]
	[ ! -f "${old%-dark.png}-light.png" ]
}

@test "a render still referenced by another pane's manifest survives pruning" {
	run_app hook-write-d2.json
	old="$(jq -r '.path' "$MANIFEST")"
	# a second pane's manifest references the same render
	printf '{"type":"image","path":"%s"}\n' "$old" >"$CLAUDE_STATUS_DIR/images/9.jsonl"
	printf 'a -> b -> c\n' >"$DOTD2"
	run_app hook-edit-d2.json
	# pruned from this pane's manifest, but the file stays — pane 9 still shows it
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	[ -f "$old" ]
}

@test "a non-.d2 file_path is ignored (fast-bail)" {
	# reuse the image fixture: file_path is a .png, not a .d2
	IMG="$BATS_TEST_TMPDIR/pic.png"
	printf 'x' >"$IMG"
	sed "s#IMGPATH#$IMG#g" "$BATS_TEST_DIRNAME/fixtures/hook-write-image.json" | bash "$APP"
	[ ! -f "$MANIFEST" ]
}

@test "render-diagram failure -> skip, log to render-errors.log, no manifest line" {
	# shellcheck disable=SC2030
	export AEYE_RENDER_FAIL=1
	run run_app hook-write-d2.json
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
	[ -f "$DIAGRAMS/render-errors.log" ]
}

@test "aeye binary absent -> clean no-op (no manifest, no warning)" {
	# md source too: with no binary the whole feature is off, nothing emitted.
	printf 'b: |md\n  blank\n|\n' >"$DOTD2"
	export AEYE_BIN=__aeye_absent__
	run run_app hook-write-d2.json
	[ "$status" -eq 0 ]
	[ -z "$output" ]
	[ ! -f "$MANIFEST" ]
	[ ! -f "$DIAGRAMS/render-errors.log" ]
}

@test "a markdown node (<foreignObject>) is suppressed: warned, logged, not shown" {
	printf 'a: "ok"\nb: |md\n  blank\n|\na -> b\n' >"$DOTD2"
	run run_app hook-write-d2.json
	[ "$status" -eq 0 ]
	# the agent is told (additionalContext) that the markdown node renders blank
	ctx="$(jq -r '.hookSpecificOutput.additionalContext' <<<"$output")"
	[[ $ctx == *"|md"* ]]
	[[ $ctx == *BLANK* ]]
	# logged for diagnostics
	run grep -c 'WARN markdown' "$DIAGRAMS/render-errors.log"
	[ "$output" -ge 1 ]
	# the blank render is NOT added to the manifest, the carousel is NOT opened,
	# and the blank files are swept from disk
	[ ! -f "$MANIFEST" ]
	[ ! -s "$TOGGLE_LOG" ]
	[ -z "$(ls "$DIAGRAMS"/*.png 2>/dev/null)" ]
}

@test "fixing the |md to a plain label renders and appears" {
	printf 'b: |md\n  blank\n|\n' >"$DOTD2"
	run_app hook-write-d2.json
	[ ! -f "$MANIFEST" ]
	# rewrite the body as a plain quoted label
	printf 'b: "blank"\n' >"$DOTD2"
	run_app hook-edit-d2.json
	[ -f "$MANIFEST" ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
}

@test "no markdown (no <foreignObject>) -> no warning on stdout" {
	run run_app hook-write-d2.json
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "a new diagram opens the carousel" {
	run_app hook-write-d2.json
	run grep -c -- '--ensure-open' "$TOGGLE_LOG"
	[ "$output" -eq 1 ]
}

@test "every new diagram re-opens the carousel" {
	run_app hook-write-d2.json
	printf 'a -> b -> c\n' >"$DOTD2"
	run_app hook-edit-d2.json
	run grep -c -- '--ensure-open' "$TOGGLE_LOG"
	[ "$output" -eq 2 ]
}

@test "an unchanged diagram does not re-open the carousel" {
	run_app hook-write-d2.json
	run_app hook-write-d2.json
	run grep -c -- '--ensure-open' "$TOGGLE_LOG"
	[ "$output" -eq 1 ]
}

@test "manifest vector field points at svg that still exists on disk" {
	run_app hook-write-d2.json
	[ -f "$MANIFEST" ]
	png="$(jq -r '.path' "$MANIFEST")"
	vector="$(jq -r '.vector' "$MANIFEST")"
	[ -n "$vector" ]
	[ -f "$vector" ]
	[ "$vector" = "${png%.png}.svg" ]
}
