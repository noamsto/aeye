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
printf '<svg/>' >"${@: -1}"
STUB
	cat >"$STUB_BIN/resvg" <<'STUB'
#!/usr/bin/env bash
printf 'PNG' >"${@: -1}"
STUB
	cat >"$STUB_BIN/tmux-claude-images" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$TOGGLE_LOG"
STUB
	# Stub aeye so the svg-contrast pass is recorded, not run for real — keeps the
	# suite hermetic against any aeye on PATH (a real one would treat svg-contrast
	# as a manifest key and try to open the TUI).
	cat >"$STUB_BIN/aeye" <<'STUB'
#!/usr/bin/env bash
[[ ${1:-} == svg-contrast ]] && printf '%s\n' "$*" >>"$CONTRAST_LOG"
exit 0
STUB
	chmod +x "$STUB_BIN/d2" "$STUB_BIN/resvg" "$STUB_BIN/tmux-claude-images" "$STUB_BIN/aeye"
	export TOGGLE_LOG="$BATS_TEST_TMPDIR/toggle.log"
	export CONTRAST_LOG="$BATS_TEST_TMPDIR/contrast.log"
	: >"$TOGGLE_LOG"
	: >"$CONTRAST_LOG"
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

@test "a markdown node (<foreignObject> in the rendered svg) warns the agent, logs it, and still renders" {
	# d2 stub mimicking the real tool: markdown source -> an SVG <foreignObject>.
	cat >"$STUB_BIN/d2" <<'STUB'
#!/usr/bin/env bash
src=""
for a in "$@"; do [[ -f $a && $a == *.d2 ]] && src="$a"; done
if grep -q '|md' "$src" 2>/dev/null; then
	printf '<svg><foreignObject><div>md</div></foreignObject></svg>' >"${@: -1}"
else
	printf '<svg/>' >"${@: -1}"
fi
STUB
	chmod +x "$STUB_BIN/d2"
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
	# the diagram still renders — the other nodes are fine
	[ -f "$MANIFEST" ]
}

@test "no markdown (no <foreignObject>) -> no warning on stdout" {
	run run_app hook-write-d2.json
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "renderers absent -> no markdown warning (silent no-op)" {
	# md source, but d2 missing: the whole feature is off, so nothing is emitted.
	printf 'b: |md\n  blank\n|\n' >"$DOTD2"
	# shellcheck disable=SC2030,SC2031
	export AEYE_D2=__aeye_absent_d2__
	run run_app hook-write-d2.json
	[ "$status" -eq 0 ]
	[ -z "$output" ]
	[ ! -f "$DIAGRAMS/render-errors.log" ]
}

@test "d2 not available -> clean no-op" {
	# Point at a command that cannot exist, so command -v fails deterministically
	# regardless of any real d2 on PATH (e.g. from the devShell).
	# shellcheck disable=SC2031
	export AEYE_D2=__aeye_absent_d2__
	run run_app hook-write-d2.json
	[ "$status" -eq 0 ]
	[ ! -f "$MANIFEST" ]
}

@test "resvg not available -> clean no-op" {
	export AEYE_RESVG=__aeye_absent_resvg__
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

@test "hook passes theme + sketch to d2" {
	cat >"$STUB_BIN/d2" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$D2_ARGLOG"
printf '<svg/>' >"${@: -1}"
STUB
	chmod +x "$STUB_BIN/d2"
	# shellcheck disable=SC2030
	export D2_ARGLOG="$BATS_TEST_TMPDIR/d2args.log"
	export AEYE_D2_THEME=200
	run_app hook-write-d2.json
	run cat "$D2_ARGLOG"
	[[ $output == *"-t 200"* ]] || {
		echo "missing -t 200 in: $output" >&2
		return 1
	}
	[[ $output == *"--sketch"* ]] || {
		echo "missing --sketch in: $output" >&2
		return 1
	}
}

@test "AEYE_D2_SKETCH=0 disables sketch" {
	cat >"$STUB_BIN/d2" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$D2_ARGLOG"
printf '<svg/>' >"${@: -1}"
STUB
	chmod +x "$STUB_BIN/d2"
	# shellcheck disable=SC2031
	export D2_ARGLOG="$BATS_TEST_TMPDIR/d2args.log"
	export AEYE_D2_SKETCH=0
	run_app hook-write-d2.json
	run cat "$D2_ARGLOG"
	[[ $output != *"--sketch"* ]] || {
		echo "unexpected --sketch in: $output" >&2
		return 1
	}
}

@test "hook runs svg-contrast on the rendered svg" {
	run_app hook-write-d2.json
	run grep -cE 'svg-contrast .*/diagrams/[0-9a-f]+\.svg' "$CONTRAST_LOG"
	[ "$output" -eq 1 ]
}

@test "missing contrast binary -> still renders" {
	export AEYE_BIN=__aeye_absent_contrast__
	run_app hook-write-d2.json
	[ -f "$MANIFEST" ]
	run wc -l <"$MANIFEST"
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

@test "font dir set -> resvg gets --skip-system-fonts --use-fonts-dir" {
	cat >"$STUB_BIN/resvg" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$RESVG_ARGLOG"
printf 'PNG' >"${@: -1}"
STUB
	chmod +x "$STUB_BIN/resvg"
	export RESVG_ARGLOG="$BATS_TEST_TMPDIR/resvgargs.log"
	export AEYE_D2_FONT_DIR="$BATS_TEST_TMPDIR/fonts"
	mkdir -p "$AEYE_D2_FONT_DIR"
	run_app hook-write-d2.json
	run cat "$RESVG_ARGLOG"
	[[ $output == *"--skip-system-fonts"* ]] || {
		echo "missing --skip-system-fonts in: $output" >&2
		return 1
	}
	[[ $output == *"--use-fonts-dir $AEYE_D2_FONT_DIR"* ]] || {
		echo "missing --use-fonts-dir in: $output" >&2
		return 1
	}
}
