#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	export PLUGIN_ROOT="$ROOT/adapters/codex/plugin"
	APP="$PLUGIN_ROOT/scripts/diagrams.sh"
	FIXTURES="$ROOT/tests/fixtures/codex"

	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	export TMUX_PANE="%7"
	MANIFEST="$AEYE_DIR/images/7.jsonl"
	DIAGRAMS="$AEYE_DIR/images/diagrams"
	DOTD2="$BATS_TEST_TMPDIR/flow.d2"
	printf 'a -> b\n' >"$DOTD2"

	# Stub aeye the same way the Claude adapter's diagrams.bats does: the whole
	# d2-compile/fix-fonts/contrast/resvg pipeline lives inside the real binary
	# (covered by Go tests), so fake it here — write a png + sibling svg, log
	# args, honor AEYE_RENDER_FAIL, and emit a <foreignObject> svg for |md
	# sources so the hook's markdown guard can fire.
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
	sed "s#D2PATH#$DOTD2#g" "$FIXTURES/$1" | bash "$APP"
}

@test "apply_patch adding a .d2 renders a png and appends one manifest line" {
	run_app apply-patch-d2.json
	[ -f "$MANIFEST" ]
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	run jq -r '.source' "$MANIFEST"
	[ "$output" = "d2" ]
	png="$(jq -r '.path' "$MANIFEST")"
	[ -f "$png" ]
	[[ $png == "$DIAGRAMS"/*.png ]]
}

@test "renders both theme variants" {
	run_app apply-patch-d2.json
	png="$(jq -r '.path' "$MANIFEST")"
	run grep -cx "render-diagram $DOTD2 ${png%-dark.png}-light.png" "$RENDER_LOG"
	[ "$output" -eq 1 ]
	run grep -cx "render-diagram $DOTD2 $png" "$RENDER_LOG"
	[ "$output" -eq 1 ]
}

@test "duplicate apply_patch of identical .d2 -> one manifest line" {
	run_app apply-patch-d2.json
	run_app apply-patch-d2.json
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
}

@test "editing the .d2 supersedes the prior render: one line, old files pruned" {
	run_app apply-patch-d2.json
	old="$(jq -r '.path' "$MANIFEST")"
	printf 'a -> b -> c\n' >"$DOTD2"
	run_app apply-patch-d2.json
	new="$(jq -r '.path' "$MANIFEST")"
	run wc -l <"$MANIFEST"
	[ "$output" -eq 1 ]
	[ "$old" != "$new" ]
	[ -f "$new" ]
	[ ! -f "$old" ]
}

@test "an apply_patch of a .png is ignored (that's images.sh's job)" {
	PNG="$BATS_TEST_TMPDIR/pic.png"
	printf 'x' >"$PNG"
	sed "s#D2PATH#$PNG#g" "$FIXTURES/apply-patch-d2.json" | bash "$APP"
	[ ! -f "$MANIFEST" ]
}

@test "render-diagram failure -> skip, log to render-errors.log, no manifest line" {
	export AEYE_RENDER_FAIL=1
	run run_app apply-patch-d2.json
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
	[ -f "$DIAGRAMS/render-errors.log" ]
}

@test "a markdown node (<foreignObject>) is suppressed: warned, logged, not shown" {
	printf 'a: "ok"\nb: |md\n  blank\n|\na -> b\n' >"$DOTD2"
	run run_app apply-patch-d2.json
	[ "$status" -eq 0 ]
	ctx="$(jq -r '.hookSpecificOutput.additionalContext' <<<"$output")"
	[[ $ctx == *"|md"* ]]
	[[ $ctx == *BLANK* ]]
	run grep -c 'WARN markdown' "$DIAGRAMS/render-errors.log"
	[ "$output" -ge 1 ]
	[ ! -f "$MANIFEST" ]
	[ ! -s "$TOGGLE_LOG" ]
	[ -z "$(ls "$DIAGRAMS"/*.png 2>/dev/null)" ]
}

@test "a new diagram opens the carousel via AEYE_TOGGLE" {
	run_app apply-patch-d2.json
	run grep -c -- '--ensure-open' "$TOGGLE_LOG"
	[ "$output" -eq 1 ]
}

@test "an unchanged diagram does not re-open the carousel" {
	run_app apply-patch-d2.json
	run_app apply-patch-d2.json
	run grep -c -- '--ensure-open' "$TOGGLE_LOG"
	[ "$output" -eq 1 ]
}
