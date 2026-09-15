#!/usr/bin/env bats

setup() {
	HOOKS="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/hooks/hooks.json"
}

@test "hooks.json is valid JSON" {
	run jq -e . "$HOOKS"
	[ "$status" -eq 0 ]
}

@test "does not register PostToolUse hooks" {
	run jq -e '(.hooks | has("PostToolUse")) | not' "$HOOKS"
	[ "$status" -eq 0 ]
}

@test "SessionStart runs diagram-guidance.sh" {
	run jq -e '[.hooks.SessionStart[].hooks[].command] | any(test("diagram-guidance.sh"))' "$HOOKS"
	[ "$status" -eq 0 ]
}

@test "SessionStart runs session-reset.sh" {
	run jq -e '[.hooks.SessionStart[].hooks[].command] | any(test("session-reset.sh"))' "$HOOKS"
	[ "$status" -eq 0 ]
}

@test "SessionStart runs session-backfill.sh" {
	run jq -e '[.hooks.SessionStart[].hooks[].command] | any(test("session-backfill.sh"))' "$HOOKS"
	[ "$status" -eq 0 ]
}
