#!/usr/bin/env bash
# Merge aeye Cursor hooks into ~/.cursor/hooks.json and link skills.
set -euo pipefail

ADAPTER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CURSOR_HOME="${AEYE_CURSOR_HOME:-$HOME/.cursor}"
HOOKS_FILE="$CURSOR_HOME/hooks.json"
TEMPLATE="$ADAPTER_DIR/hooks.json"
MARKER='/adapters/cursor/scripts/'

if ! command -v jq >/dev/null 2>&1; then
	echo "install.sh: jq is required but not found on PATH" >&2
	exit 1
fi

if [[ ! -f $TEMPLATE ]]; then
	echo "install.sh: missing template $TEMPLATE" >&2
	exit 1
fi

rendered="$(jq --arg dir "$ADAPTER_DIR" '
	.hooks |= with_entries(
		.value |= map(
			if .command | type == "string" then
				.command |= gsub("<ADAPTER_DIR>"; $dir)
			else .
			end
		)
	)
' "$TEMPLATE")"

mkdir -p "$CURSOR_HOME"

if [[ -f $HOOKS_FILE ]]; then
	if ! jq empty "$HOOKS_FILE" >/dev/null 2>&1; then
		echo "install.sh: refusing to overwrite malformed $HOOKS_FILE (not valid JSON)" >&2
		exit 1
	fi
	existing="$(cat "$HOOKS_FILE")"
else
	existing='{}'
fi

merged="$(jq -n --argjson existing "$existing" --argjson template "$rendered" --arg marker "$MARKER" '
	def strip_aeye:
		map(select(
			(.command | type) != "string"
			or (.command | contains($marker) | not)
		));

	($existing.hooks // {}) as $eh |
	($template.hooks // {}) as $th |
	($eh | with_entries(.value |= strip_aeye)) as $cleaned |
	(
		reduce ($th | to_entries[]) as $ev ($cleaned;
			.[$ev.key] = ((.[$ev.key] // []) + $ev.value)
		)
	) as $hooks |
	$existing + {hooks: $hooks} |
	if has("version") then . else .version = ($template.version // 1) end
')"

tmp="$(mktemp)"
printf '%s\n' "$merged" >"$tmp"
mv "$tmp" "$HOOKS_FILE"

skills_dir="$CURSOR_HOME/skills"
mkdir -p "$skills_dir"
ln -sfn "$ADAPTER_DIR/skills/diagrams" "$skills_dir/aeye-diagrams"
ln -sfn "$ADAPTER_DIR/skills/image-gallery" "$skills_dir/aeye-image-gallery"

echo "aeye cursor adapter installed:"
echo "  hooks:  $HOOKS_FILE"
echo "  skills: $skills_dir/aeye-diagrams -> $ADAPTER_DIR/skills/diagrams"
echo "  skills: $skills_dir/aeye-image-gallery -> $ADAPTER_DIR/skills/image-gallery"
