#!/usr/bin/env bats

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	APP="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/scripts/diagram-guidance.sh"
	unset TMUX KITTY_LISTEN_ON
}

@test "inside tmux: emits guidance containing the scratch dir" {
	# shellcheck disable=SC2030
	export TMUX="/tmp/fake"
	run bash "$APP"
	[ "$status" -eq 0 ]
	# additionalContext mentions the resolved src dir and the .d2 instruction
	ctx="$(jq -r '.hookSpecificOutput.additionalContext' <<<"$output")"
	[[ $ctx == *"$CLAUDE_STATUS_DIR/images/diagrams/src"* ]]
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
	# shellcheck disable=SC2031
	export TMUX="/tmp/fake"
	run bash "$APP"
	[ "$status" -eq 0 ]
	[ -d "$CLAUDE_STATUS_DIR/images/diagrams/src" ]
}
