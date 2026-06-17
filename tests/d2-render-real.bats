#!/usr/bin/env bats
# Real end-to-end render via `aeye render-diagram` (embedded d2 + resvg): a
# diagram with bold/italic text compiles and rasterizes to a PNG without error.
# Skips when go or resvg is unavailable. The font rewrite itself is unit-tested
# in Go (TestFixFonts).

setup() {
	ROOT="$(dirname "$BATS_TEST_DIRNAME")"
	D2D="$BATS_TEST_TMPDIR/in.d2"
	printf 'a: **bold** label\nb: _italic_ label\na -> b: edge\n' >"$D2D"
}

@test "aeye render-diagram compiles + rasterizes a real diagram to a png" {
	command -v go >/dev/null || skip "go not installed"
	command -v resvg >/dev/null || skip "resvg not installed"
	AEYE="$BATS_TEST_TMPDIR/aeye"
	go build -C "$ROOT" -o "$AEYE" . || skip "aeye build failed"

	run "$AEYE" render-diagram "$D2D" "$BATS_TEST_TMPDIR/out.png"
	[ "$status" -eq 0 ] || {
		echo "render-diagram failed (status=$status): $output" >&2
		return 1
	}
	[ -s "$BATS_TEST_TMPDIR/out.png" ] # png written
	[ -s "$BATS_TEST_TMPDIR/out.svg" ] # vector sibling written
}
