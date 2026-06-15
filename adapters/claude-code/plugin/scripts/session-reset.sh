#!/usr/bin/env bash
# SessionStart hook: clear this pane/session's image manifest on a fresh start, so
# the carousel never shows images a previous session captured in a reused tmux
# pane id. Resets on a genuinely fresh start (source=startup|clear) and keeps the
# manifest when work continues (resume|compact). Reads the hook JSON on stdin.
set -euo pipefail

payload="$(cat)"
[[ -n $payload ]] || exit 0
case "$(jq -r '.source // empty' <<<"$payload" 2>/dev/null)" in
startup | clear) ;;
*) exit 0 ;;
esac

STATE_DIR="${AEYE_DIR:-${CLAUDE_STATUS_DIR:-/tmp/claude-status}}"
IMAGES_DIR="$STATE_DIR/images"

# Same keying as images.sh/diagrams.sh so we clear the right manifest.
pane_id="${TMUX_PANE:-${CLAUDE_CODE_SESSION_ID:-}}"
[[ -n $pane_id ]] || exit 0
pane_file="${pane_id#%}"
[[ $pane_file =~ ^[A-Za-z0-9_@:.-]+$ ]] || exit 0

rm -f "$IMAGES_DIR/$pane_file.jsonl"
