#!/usr/bin/env bash
# Rewrite d2's embedded @font-face family names in an SVG to an installed
# family, and inject weight/style so bold/italic labels pick the right face.
# resvg ignores @font-face, so without this every text label is dropped.
set -euo pipefail

svg="$1"
family="${AGENT_CAROUSEL_D2_FONT:-Noto Sans}"

# Family-remap runs first; the bold/italic injections anchor on the
# font-family declaration d2 emits, which makes them idempotent. Plain
# font names only — & or \ in $family would need sed-escaping.
sed -E \
	-e "s/d2-[0-9]+-font-[a-z]+/${family}/g" \
	-e 's/(\.text-bold \{)(font-family)/\1font-weight:bold;\2/g' \
	-e 's/(\.text-italic \{)(font-family)/\1font-style:italic;\2/g' \
	"$svg" >"$svg.tmp"
mv "$svg.tmp" "$svg"
