#!/usr/bin/env bats
# Every ```d2 example in SKILL.md must compile, render text (0 "No match"),
# and be single-board. Skips when d2/resvg are unavailable.

setup() {
	ROOT="$(dirname "$BATS_TEST_DIRNAME")"
	SKILL="$ROOT/adapters/claude-code/plugin/skills/diagrams/SKILL.md"
	FIX="$ROOT/adapters/claude-code/plugin/scripts/d2-fix-fonts.sh"
	EXTRACT="$ROOT/tests/lib/extract-d2-blocks.sh"
}

@test "SKILL.md d2 examples use no |md / |markdown blocks (they render blank in resvg)" {
	mapfile -d '' -t blocks < <(bash "$EXTRACT" "$SKILL")
	for block in "${blocks[@]}"; do
		[[ $block != *"|md"* ]] || {
			echo "a SKILL.md d2 example contains a |md/|markdown block: $block"
			return 1
		}
	done
}

@test "every d2 example in SKILL.md compiles, renders text, and is single-board" {
	command -v d2 >/dev/null || skip "d2 not installed"
	command -v resvg >/dev/null || skip "resvg not installed"

	mapfile -d '' -t blocks < <(bash "$EXTRACT" "$SKILL")
	[ "${#blocks[@]}" -ge 1 ] || {
		echo "no d2 examples found in SKILL.md"
		return 1
	}

	local i=0
	for block in "${blocks[@]}"; do
		i=$((i + 1))
		local d2f="$BATS_TEST_TMPDIR/ex$i.d2" out="$BATS_TEST_TMPDIR/ex$i.svg"
		printf '%s' "$block" >"$d2f"

		run d2 "$d2f" "$out"
		[ "$status" -eq 0 ] || {
			echo "example $i failed to compile: $output"
			return 1
		}
		[ -f "$out" ] || {
			echo "example $i produced multiple boards (not single-board): $d2f"
			return 1
		}

		bash "$FIX" "$out"
		args=()
		[[ -n ${AEYE_D2_FONT_DIR:-} ]] && args=(--skip-system-fonts --use-fonts-dir "$AEYE_D2_FONT_DIR")
		run bash -c 'resvg "$@" 2>&1' _ "${args[@]}" "$out" "$BATS_TEST_TMPDIR/ex$i.png"
		[ "$status" -eq 0 ] || {
			echo "example $i resvg failed: $output"
			return 1
		}
		[[ $output != *"No match for font-family"* ]] || {
			echo "example $i has unresolved fonts: $output"
			return 1
		}
	done
}
