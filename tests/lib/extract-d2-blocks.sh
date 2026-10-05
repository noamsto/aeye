#!/usr/bin/env bash
# Print each ```d2 fenced code block from the given markdown files, separated by a NUL
# byte. Used by the skill-examples render test to render every example.
set -euo pipefail

awk '
  /^```d2[[:space:]]*$/ { inblock=1; next }
  /^```[[:space:]]*$/   { if (inblock) { printf "%s", "\0"; inblock=0 }; next }
  inblock               { print }
' "$@"
