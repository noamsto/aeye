#!/usr/bin/env bats
# Real d2 + resvg. Proves text fonts resolve (zero "No match for font-family").
# Skips when binaries or a usable font are unavailable.

setup() {
	FIX="$(dirname "$BATS_TEST_DIRNAME")/adapters/claude-code/plugin/scripts/d2-fix-fonts.sh"
	D2D="$BATS_TEST_TMPDIR/in.d2"
	SVG="$BATS_TEST_TMPDIR/in.svg"
	printf 'a: **bold** label\nb: _italic_ label\na -> b: edge\n' >"$D2D"
}

@test "real render resolves every font (no 'No match for font-family')" {
	command -v d2 >/dev/null || skip "d2 not installed"
	command -v resvg >/dev/null || skip "resvg not installed"

	d2 "$D2D" "$SVG"
	bash "$FIX" "$SVG"

	# Prefer the hermetic bundle when the env points at one; else system fonts.
	args=()
	if [[ -n ${AGENT_CAROUSEL_D2_FONT_DIR:-} ]]; then
		args=(--skip-system-fonts --use-fonts-dir "$AGENT_CAROUSEL_D2_FONT_DIR")
	fi
	run bash -c 'resvg "$@" "'"$SVG"'" "'"$BATS_TEST_TMPDIR"'/out.png" 2>&1' _ "${args[@]}"
	[[ $output != *"No match for font-family"* ]]
}
