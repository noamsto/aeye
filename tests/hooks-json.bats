#!/usr/bin/env bats

setup() {
	HOOKS="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/hooks/hooks.json"
}

@test "hooks.json is valid JSON" {
	run jq -e . "$HOOKS"
	[ "$status" -eq 0 ]
}

@test "two PostToolUse hooks: images.sh and diagrams.sh" {
	run jq -e '.hooks.PostToolUse | length == 2' "$HOOKS"
	[ "$status" -eq 0 ]
	run jq -e '[.hooks.PostToolUse[].hooks[].command] | any(test("diagrams.sh"))' "$HOOKS"
	[ "$status" -eq 0 ]
	run jq -e '[.hooks.PostToolUse[].hooks[].command] | any(test("images.sh"))' "$HOOKS"
	[ "$status" -eq 0 ]
}

@test "SessionStart runs diagram-guidance.sh" {
	run jq -e '[.hooks.SessionStart[].hooks[].command] | any(test("diagram-guidance.sh"))' "$HOOKS"
	[ "$status" -eq 0 ]
}
