#!/usr/bin/env bats
# The Codex plugin vendors its own copy of adapters/core/ (codex plugin add
# copies the plugin dir only, not its core/ sibling — see adapters/codex/plugin/scripts/core/).
# These tests fail loudly on drift so a change to core/ doesn't silently go
# stale in the vendored copy; run `just sync-codex-core` to resync.

setup() {
	ROOT="$(dirname "$(dirname "$BATS_TEST_DIRNAME")")"
	CANONICAL="$ROOT/adapters/core"
	VENDORED="$ROOT/adapters/codex/plugin/scripts/core"
}

@test "vendored manifest-extract.sh matches adapters/core/manifest-extract.sh" {
	run diff -q "$CANONICAL/manifest-extract.sh" "$VENDORED/manifest-extract.sh"
	[ "$status" -eq 0 ] || {
		echo "vendored core/ has drifted from adapters/core/ — run 'just sync-codex-core'" >&2
		return 1
	}
}

@test "vendored manifest-lifecycle.sh matches adapters/core/manifest-lifecycle.sh" {
	run diff -q "$CANONICAL/manifest-lifecycle.sh" "$VENDORED/manifest-lifecycle.sh"
	[ "$status" -eq 0 ] || {
		echo "vendored core/ has drifted from adapters/core/ — run 'just sync-codex-core'" >&2
		return 1
	}
}
