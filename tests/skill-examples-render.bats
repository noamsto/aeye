#!/usr/bin/env bats
# Every ```d2 example in SKILL.md and its references/*.md must be single-board and render via
# `aeye render-diagram` (embedded d2 + resvg) without error. Skips when go or
# resvg is unavailable.

setup() {
	ROOT="$(dirname "$BATS_TEST_DIRNAME")"
	SKILL_DIR="$ROOT/adapters/claude-code/plugin/skills/diagrams"
	FILES=("$SKILL_DIR/SKILL.md" "$SKILL_DIR"/references/*.md)
	EXTRACT="$ROOT/tests/lib/extract-d2-blocks.sh"
}

@test "skill d2 examples use no |md / |markdown blocks (they render blank in resvg)" {
	mapfile -d '' -t blocks < <(bash "$EXTRACT" "${FILES[@]}")
	for block in "${blocks[@]}"; do
		[[ $block != *"|md"* ]] || {
			echo "a skill d2 example contains a |md/|markdown block: $block"
			return 1
		}
	done
}

@test "every d2 example in SKILL.md and references/ renders via aeye render-diagram" {
	command -v go >/dev/null || skip "go not installed"
	command -v resvg >/dev/null || skip "resvg not installed"
	AEYE="$BATS_TEST_TMPDIR/aeye"
	go build -C "$ROOT" -o "$AEYE" . || skip "aeye build failed"

	mapfile -d '' -t blocks < <(bash "$EXTRACT" "${FILES[@]}")
	[ "${#blocks[@]}" -ge 1 ] || {
		echo "no d2 examples found in the skill"
		return 1
	}

	local i=0
	for block in "${blocks[@]}"; do
		i=$((i + 1))
		local d2f="$BATS_TEST_TMPDIR/ex$i.d2" png="$BATS_TEST_TMPDIR/ex$i.png"
		printf '%s' "$block" >"$d2f"
		run "$AEYE" render-diagram "$d2f" "$png"
		[ "$status" -eq 0 ] || {
			echo "example $i failed to render: $output"
			return 1
		}
		[ -s "$png" ] || {
			echo "example $i produced no png"
			return 1
		}
	done
}
