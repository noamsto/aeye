#!/usr/bin/env bats

setup() {
	APP="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/scripts/d2-fix-fonts.sh"
	SVG="$BATS_TEST_TMPDIR/s.svg"
	cp "$BATS_TEST_DIRNAME/fixtures/d2-fonts-sample.svg" "$SVG"
	unset AGENT_CAROUSEL_D2_FONT
}

@test "removes every synthetic d2 font-family name" {
	bash "$APP" "$SVG"
	run grep -cE 'd2-[0-9]+-font-[a-z]+' "$SVG"
	[ "$output" -eq 0 ]
}

@test "remaps to the default family (Noto Sans) when no override" {
	bash "$APP" "$SVG"
	run grep -c 'Noto Sans' "$SVG"
	[ "$output" -ge 1 ]
}

@test "honors AGENT_CAROUSEL_D2_FONT override" {
	AGENT_CAROUSEL_D2_FONT="Source Sans 3" bash "$APP" "$SVG"
	run grep -c 'Source Sans 3' "$SVG"
	[ "$output" -ge 1 ]
}

@test "injects font-weight on the bold rule and font-style on the italic rule" {
	bash "$APP" "$SVG"
	run grep -c 'font-weight:bold' "$SVG"
	[ "$output" -eq 1 ]
	run grep -c 'font-style:italic' "$SVG"
	[ "$output" -eq 1 ]
}

@test "running twice is idempotent (no doubled declarations)" {
	bash "$APP" "$SVG"
	bash "$APP" "$SVG"
	run grep -c 'font-weight:bold' "$SVG"
	[ "$output" -eq 1 ]
	run grep -c 'font-style:italic' "$SVG"
	[ "$output" -eq 1 ]
}
