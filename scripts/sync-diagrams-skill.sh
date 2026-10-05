#!/usr/bin/env bash
# Render the canonical diagrams skill (adapters/core/skills/diagrams) into each
# adapter's skills dir. Plugins are installed by copying their own dir, so
# symlinks back into core/ would dangle — the copies are generated instead.
#   sync-diagrams-skill.sh           write the copies
#   sync-diagrams-skill.sh --check   exit 1 if any copy differs from the source
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
src="$root/adapters/core/skills/diagrams"

declare -A dest=(
	["claude-code"]="$root/adapters/claude-code/plugin/skills/diagrams"
	["codex"]="$root/adapters/codex/plugin/skills/diagrams"
	["cursor"]="$root/adapters/cursor/skills/diagrams"
	["pi"]="$root/adapters/pi/skills/diagrams"
)

case "${1:-}" in
"") mode="write" ;;
--check) mode="--check" ;;
*)
	echo "usage: ${0##*/} [--check]" >&2
	exit 2
	;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# render ENGINE OUTDIR -> SKILL.md with the engine's snippets substituted for
# the {{INTRO}} / {{WRITE_CALLS}} lines, plus the references.
render() {
	local engine="$1" out="$2" line
	mkdir -p "$out"
	while IFS= read -r line || [[ -n $line ]]; do
		case "$line" in
		"{{INTRO}}") cat "$src/engines/$engine/intro.md" ;;
		"{{WRITE_CALLS}}") cat "$src/engines/$engine/write-calls.md" ;;
		*) printf '%s\n' "$line" ;;
		esac
	done <"$src/SKILL.md.tmpl" >"$out/SKILL.md"
	if grep -q '{{[A-Za-z0-9_]*}}' "$out/SKILL.md"; then
		echo "$engine: unreplaced {{placeholder}} in SKILL.md.tmpl" >&2
		exit 1
	fi
	cp -r "$src/references" "$out/references"
}

rc=0
for engine in "${!dest[@]}"; do
	render "$engine" "$tmp/$engine"
	if [[ $mode == --check ]]; then
		if ! diff -r "$tmp/$engine" "${dest[$engine]}" >&2; then
			echo "$engine diagrams skill has drifted from adapters/core/skills/diagrams — run 'just sync-diagrams-skill'" >&2
			rc=1
		fi
	else
		rm -rf "${dest[$engine]}"
		cp -r "$tmp/$engine" "${dest[$engine]}"
	fi
done
exit "$rc"
