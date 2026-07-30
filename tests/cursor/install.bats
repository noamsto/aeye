#!/usr/bin/env bats
# install.sh merges hooks into AEYE_CURSOR_HOME (never the real ~/.cursor in tests).

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	INSTALL="$ROOT/adapters/cursor/install.sh"
	ADAPTER_DIR="$ROOT/adapters/cursor"
	export AEYE_CURSOR_HOME="$BATS_TEST_TMPDIR/.cursor"
	HOOKS_FILE="$AEYE_CURSOR_HOME/hooks.json"
}

@test "fresh install creates hooks.json with all 5 absolute adapter commands" {
	run "$INSTALL"
	[ "$status" -eq 0 ]
	[ -f "$HOOKS_FILE" ]
	jq empty "$HOOKS_FILE"

	mapfile -t cmds < <(jq -r '.hooks | to_entries[] | .value[] | .command' "$HOOKS_FILE")
	[ "${#cmds[@]}" -eq 5 ]

	expected=(
		"$ADAPTER_DIR/scripts/diagram-guidance.sh"
		"$ADAPTER_DIR/scripts/session-reset.sh"
		"$ADAPTER_DIR/scripts/session-backfill.sh"
		"$ADAPTER_DIR/scripts/images.sh"
		"$ADAPTER_DIR/scripts/diagrams.sh"
	)
	for i in "${!expected[@]}"; do
		[[ ${cmds[$i]} == /* ]]
		[ "${cmds[$i]}" = "${expected[$i]}" ]
	done

	[ "$(jq -r '.hooks.postToolUse[0].matcher' "$HOOKS_FILE")" = "Read|Write|Shell" ]
	[ "$(jq -r '.hooks.postToolUse[1].matcher' "$HOOKS_FILE")" = "Write|Shell" ]
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
	images="$(jq '[.hooks.postToolUse[] | select(.command | endswith("/scripts/images.sh"))] | length' "$HOOKS_FILE")"
	[ "$images" -eq 1 ]
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
