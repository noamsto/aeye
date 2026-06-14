#!/usr/bin/env bash
# Rewrite d2's embedded @font-face family names in an SVG to an installed
# family, and inject weight/style so bold/italic labels pick the right face.
# resvg ignores @font-face, so without this every text label is dropped.
set -euo pipefail

svg="$1"
family="${AGENT_CAROUSEL_D2_FONT:-Noto Sans}"

# Family-remap runs first. The bold/italic injections anchor only on the
# rule's brace (d2 puts font-family on the next line, so a same-line anchor
# would miss real output); the optional already-injected token keeps a
# re-run idempotent. Plain font names only — & or \ in $family would need
# sed-escaping.
sed -E \
	-e "s/d2-[0-9]+-font-[a-z]+/${family}/g" \
	-e 's/(\.text-bold \{)(font-weight:bold;)?/\1font-weight:bold;/g' \
	-e 's/(\.text-italic \{)(font-style:italic;)?/\1font-style:italic;/g' \
	"$svg" >"$svg.tmp"
mv "$svg.tmp" "$svg"
