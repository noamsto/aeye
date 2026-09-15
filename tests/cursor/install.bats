#!/usr/bin/env bats
# install.sh merges hooks into AEYE_CURSOR_HOME (never the real ~/.cursor in tests).

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	INSTALL="$ROOT/adapters/cursor/install.sh"
	ADAPTER_DIR="$ROOT/adapters/cursor"
	export AEYE_CURSOR_HOME="$BATS_TEST_TMPDIR/.cursor"
	HOOKS_FILE="$AEYE_CURSOR_HOME/hooks.json"
}

@test "fresh install creates hooks.json with all 3 absolute session-start commands" {
	run "$INSTALL"
	[ "$status" -eq 0 ]
	[ -f "$HOOKS_FILE" ]
	jq empty "$HOOKS_FILE"

	mapfile -t cmds < <(jq -r '.hooks | to_entries[] | .value[] | .command' "$HOOKS_FILE")
	[ "${#cmds[@]}" -eq 3 ]

	expected=(
		"$ADAPTER_DIR/scripts/diagram-guidance.sh"
		"$ADAPTER_DIR/scripts/session-reset.sh"
		"$ADAPTER_DIR/scripts/session-backfill.sh"
	)
	for i in "${!expected[@]}"; do
		[[ ${cmds[$i]} == /* ]]
		[ "${cmds[$i]}" = "${expected[$i]}" ]
	done

	[ "$(jq -r '.hooks.postToolUse // empty' "$HOOKS_FILE")" = "" ]
}

@test "install is idempotent" {
	"$INSTALL"
	cp "$HOOKS_FILE" "$BATS_TEST_TMPDIR/after-first.json"
	"$INSTALL"
	run diff -q "$BATS_TEST_TMPDIR/after-first.json" "$HOOKS_FILE"
	[ "$status" -eq 0 ]
}

@test "install preserves unrelated existing hook entries" {
	mkdir -p "$AEYE_CURSOR_HOME"
	cat >"$HOOKS_FILE" <<'EOF'
{
  "version": 1,
  "hooks": {
    "sessionStart": [
      { "command": "cat > /tmp/keep.json" }
    ]
  },
  "customKey": true
}
EOF
	run "$INSTALL"
	[ "$status" -eq 0 ]

	[ "$(jq -r '.customKey' "$HOOKS_FILE")" = "true" ]
	[ "$(jq -r '.hooks.sessionStart[0].command' "$HOOKS_FILE")" = "cat > /tmp/keep.json" ]

	count="$(jq '[.hooks.sessionStart[] | select(.command | contains("/adapters/cursor/scripts/"))] | length' "$HOOKS_FILE")"
	[ "$count" -eq 3 ]
	[ "$(jq -r '.hooks.postToolUse[0].command' "$HOOKS_FILE")" = "null" ]
}

@test "install removes stale aeye hooks and preserves unrelated post-tool hooks" {
	mkdir -p "$AEYE_CURSOR_HOME"
	cat >"$HOOKS_FILE" <<'EOF'
{
  "version": 1,
  "hooks": {
    "postToolUse": [
      { "command": "/old/aeye/adapters/cursor/scripts/images.sh" },
      { "command": "printf keep" }
    ]
  }
}
EOF
	run "$INSTALL"
	[ "$status" -eq 0 ]
	[ "$(jq -r '.hooks.postToolUse | length' "$HOOKS_FILE")" -eq 1 ]
	[ "$(jq -r '.hooks.postToolUse[0].command' "$HOOKS_FILE")" = "printf keep" ]
}

@test "malformed existing hooks.json exits non-zero and leaves file untouched" {
	mkdir -p "$AEYE_CURSOR_HOME"
	printf 'not-json{\n' >"$HOOKS_FILE"
	cp "$HOOKS_FILE" "$BATS_TEST_TMPDIR/before.json"
	run "$INSTALL"
	[ "$status" -ne 0 ]
	run diff -q "$BATS_TEST_TMPDIR/before.json" "$HOOKS_FILE"
	[ "$status" -eq 0 ]
}

@test "skill symlinks point at adapter skill dirs" {
	run "$INSTALL"
	[ "$status" -eq 0 ]
	[ -L "$AEYE_CURSOR_HOME/skills/aeye-diagrams" ]
	[ -L "$AEYE_CURSOR_HOME/skills/aeye-image-gallery" ]
	[ "$(readlink "$AEYE_CURSOR_HOME/skills/aeye-diagrams")" = "$ADAPTER_DIR/skills/diagrams" ]
	[ "$(readlink "$AEYE_CURSOR_HOME/skills/aeye-image-gallery")" = "$ADAPTER_DIR/skills/image-gallery" ]
}
