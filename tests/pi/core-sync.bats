#!/usr/bin/env bats
# The pi adapter vendors its own copy of adapters/core/ so the adapter dir is
# self-contained when installed/shipped on its own. These tests fail loudly on
# drift so a change to core/ doesn't silently go stale in the vendored copy; run
# `just sync-pi-core` to resync.

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	CANONICAL="$ROOT/adapters/core"
	VENDORED="$ROOT/adapters/pi/scripts/core"
}

@test "vendored manifest-extract.sh matches adapters/core/manifest-extract.sh" {
	run diff -q "$CANONICAL/manifest-extract.sh" "$VENDORED/manifest-extract.sh"
	[ "$status" -eq 0 ] || {
		echo "vendored core/ has drifted from adapters/core/ — run 'just sync-pi-core'" >&2
		return 1
	}
}

@test "vendored manifest-lifecycle.sh matches adapters/core/manifest-lifecycle.sh" {
	run diff -q "$CANONICAL/manifest-lifecycle.sh" "$VENDORED/manifest-lifecycle.sh"
	[ "$status" -eq 0 ] || {
		echo "vendored core/ has drifted from adapters/core/ — run 'just sync-pi-core'" >&2
		return 1
	}
}
