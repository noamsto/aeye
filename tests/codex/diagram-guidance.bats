#!/usr/bin/env bats

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	APP="$ROOT/adapters/codex/plugin/scripts/diagram-guidance.sh"
	unset TMUX KITTY_LISTEN_ON
	# Stub the render deps on PATH so the preflight takes the available path; the
	# missing-dep cases below opt out via AEYE_BIN / AEYE_RESVG overrides.
	mkdir -p "$BATS_TEST_TMPDIR/bin"
	printf '#!/bin/sh\n' >"$BATS_TEST_TMPDIR/bin/aeye"
	cp "$BATS_TEST_TMPDIR/bin/aeye" "$BATS_TEST_TMPDIR/bin/resvg"
	chmod +x "$BATS_TEST_TMPDIR/bin/aeye" "$BATS_TEST_TMPDIR/bin/resvg"
	export PATH="$BATS_TEST_TMPDIR/bin:$PATH"
}

@test "inside tmux: emits guidance containing the scratch dir" {
	# shellcheck disable=SC2030
	export TMUX="/tmp/fake"
	run bash "$APP"
	[ "$status" -eq 0 ]
	# additionalContext mentions the resolved src dir and the .d2 instruction
	ctx="$(jq -r '.hookSpecificOutput.additionalContext' <<<"$output")"
	[[ $ctx == *"$AEYE_DIR/images/diagrams/src"* ]]
	[[ $ctx == *".d2"* ]]
}

@test "guidance warns against |md blocks" {
	# shellcheck disable=SC2030,SC2031
	export TMUX="/tmp/fake"
	run bash "$APP"
	[ "$status" -eq 0 ]
	ctx="$(jq -r '.hookSpecificOutput.additionalContext' <<<"$output")"
	[[ $ctx == *"|md"* ]]
}

@test "no host: emits nothing" {
	run bash "$APP"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "kitty remote control counts as a host" {
	export KITTY_LISTEN_ON="unix:/tmp/kitty"
	run bash "$APP"
	[ -n "$output" ]
}

@test "host present: creates the scratch dir" {
	# shellcheck disable=SC2030,SC2031
	export TMUX="/tmp/fake"
	run bash "$APP"
	[ "$status" -eq 0 ]
	[ -d "$AEYE_DIR/images/diagrams/src" ]
}

@test "aeye missing: warns instead of nudging, names the binary" {
	# shellcheck disable=SC2030,SC2031
	export TMUX="/tmp/fake"
	# shellcheck disable=SC2030,SC2031
	export AEYE_BIN="aeye-not-installed"
	run bash "$APP"
	[ "$status" -eq 0 ]
	ctx="$(jq -r '.hookSpecificOutput.additionalContext' <<<"$output")"
	[[ $ctx == *"unavailable"* ]]
	[[ $ctx == *"aeye-not-installed"* ]]
	# the full draw-diagrams nudge (its scratch-dir path) must NOT be present
	[[ $ctx != *"images/diagrams/src"* ]]
}

@test "resvg missing: warns and names resvg" {
	# shellcheck disable=SC2031
	export TMUX="/tmp/fake"
	# shellcheck disable=SC2030,SC2031
	export AEYE_RESVG="resvg-not-installed"
	run bash "$APP"
	[ "$status" -eq 0 ]
	ctx="$(jq -r '.hookSpecificOutput.additionalContext' <<<"$output")"
	[[ $ctx == *"unavailable"* ]]
	[[ $ctx == *"resvg-not-installed"* ]]
}

@test "no host: emits nothing even when deps missing" {
	# shellcheck disable=SC2030,SC2031
	export AEYE_BIN="aeye-not-installed"
	run bash "$APP"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}
