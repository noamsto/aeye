#!/usr/bin/env bats
bats_require_minimum_version 1.5.0

setup() {
	APP="$(dirname "$BATS_TEST_DIRNAME")/scripts/tmux-claude-images.sh"
}

@test "auto: landscape window (cols > 2*rows) -> side" {
	run env -u AEYE_SPLIT bash "$APP" --resolve-axis 200 50
	[ "$status" -eq 0 ]
	[ "$output" = side ]
}

@test "auto: portrait-ish window (cols <= 2*rows) -> bottom" {
	run env -u AEYE_SPLIT bash "$APP" --resolve-axis 90 50
	[ "$output" = bottom ]
}

@test "auto: boundary cols == 2*rows -> bottom" {
	run env -u AEYE_SPLIT bash "$APP" --resolve-axis 100 50
	[ "$output" = bottom ]
}

@test "auto: unreadable dims -> side (no regression)" {
	run env -u AEYE_SPLIT bash "$APP" --resolve-axis "" ""
	[ "$output" = side ]
}

@test "auto: zero dims -> side" {
	run env -u AEYE_SPLIT bash "$APP" --resolve-axis 0 0
	[ "$output" = side ]
}

@test "AEYE_SPLIT=side forces side on a portrait window" {
	AEYE_SPLIT=side run bash "$APP" --resolve-axis 90 50
	[ "$output" = side ]
}

@test "AEYE_SPLIT=bottom forces bottom on a landscape window" {
	AEYE_SPLIT=bottom run bash "$APP" --resolve-axis 200 50
	[ "$output" = bottom ]
}

@test "AEYE_SPLIT=garbage falls back to auto" {
	AEYE_SPLIT=garbage run bash "$APP" --resolve-axis 200 50
	[ "$output" = side ]
}
