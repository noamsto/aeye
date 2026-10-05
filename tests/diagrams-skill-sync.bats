#!/usr/bin/env bats
# Each adapter's diagrams skill is generated from adapters/core/skills/diagrams
# (plugin installs copy their own dir, so they can't symlink into core/). Fail
# loudly when a copy is hand-edited or the source changed without a re-sync.

setup() {
	ROOT="$(dirname "$BATS_TEST_DIRNAME")"
	# A scratch repo: the script derives its root from its own location.
	SCRATCH="$BATS_TEST_TMPDIR/repo"
	mkdir -p "$SCRATCH/scripts" "$SCRATCH/adapters"
	cp "$ROOT/scripts/sync-diagrams-skill.sh" "$SCRATCH/scripts/"
	cp -r "$ROOT/adapters/." "$SCRATCH/adapters/"
	SYNC="$SCRATCH/scripts/sync-diagrams-skill.sh"
}

@test "every adapter's diagrams skill matches adapters/core/skills/diagrams" {
	run "$ROOT/scripts/sync-diagrams-skill.sh" --check
	[ "$status" -eq 0 ] || {
		echo "$output" >&2
		return 1
	}
}

@test "--check fails on a hand-edited copy, passes again after a sync" {
	echo hand-edit >>"$SCRATCH/adapters/codex/plugin/skills/diagrams/SKILL.md"
	run "$SYNC" --check
	[ "$status" -eq 1 ]
	[[ $output == *"just sync-diagrams-skill"* ]]
	run "$SYNC"
	[ "$status" -eq 0 ]
	run "$SYNC" --check
	[ "$status" -eq 0 ]
}

@test "--check fails on a stale extra file and sync removes it" {
	touch "$SCRATCH/adapters/pi/skills/diagrams/references/stale.md" "$SCRATCH/adapters/pi/skills/diagrams/old-top.md"
	run "$SYNC" --check
	[ "$status" -eq 1 ]
	run "$SYNC"
	run "$SYNC" --check
	[ "$status" -eq 0 ]
}

@test "--check fails on a missing reference" {
	rm "$SCRATCH/adapters/cursor/skills/diagrams/references/github-embedding.md"
	run "$SYNC" --check
	[ "$status" -eq 1 ]
}

@test "an unreplaced placeholder fails the sync" {
	echo '{{INTR0}}' >>"$SCRATCH/adapters/core/skills/diagrams/SKILL.md.tmpl"
	run "$SYNC"
	[ "$status" -eq 1 ]
	[[ $output == *"unreplaced"* ]]
}

@test "an unknown argument is rejected, not treated as write mode" {
	run "$SYNC" --bogus
	[ "$status" -eq 2 ]
}

@test "the always-loaded SKILL.md stays slim (<= 11000 bytes)" {
	for f in "$ROOT"/adapters/{cursor,pi}/skills/diagrams/SKILL.md "$ROOT"/adapters/*/plugin/skills/diagrams/SKILL.md; do
		size="$(wc -c <"$f")"
		[ "$size" -le 11000 ] || {
			echo "$f is $size bytes — move rarely-needed detail into references/" >&2
			return 1
		}
	done
}
