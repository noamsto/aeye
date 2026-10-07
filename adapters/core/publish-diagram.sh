#!/usr/bin/env bash
# publish-diagram FILE.d2 [PANE] [OPEN]: render a .d2 into a pane's carousel
# without an agent turn — the CLI twin of the PostToolUse diagrams.sh hooks, on
# the same render, variant naming, entry format and manifest lock. Run by
# `aeye publish-diagram`, which embeds this file with its core/ siblings.
# Exit 0 quietly on success; render and input errors go to stderr, non-zero.
set -euo pipefail

# shellcheck source=manifest-lifecycle.sh disable=SC1091
source "$(dirname "${BASH_SOURCE[0]}")/manifest-lifecycle.sh"

file="$1" pane="${2:-}" open="${3:-}"

resolve_state_dirs
src_dir="$DIAGRAMS_DIR/src"

[[ -f $file ]] || {
	echo "aeye publish-diagram: $file: no such file" >&2
	exit 1
}
[[ $file == /* ]] || file="$PWD/$file"
# Only a canonical source under the diagrams src dir is published: the entry's
# name is the basename, so a scratch copy would file a second, permanent entry.
d2_source_adoptable "$file" "$src_dir" || {
	echo "aeye publish-diagram: $file must be a .d2 under $src_dir (not *-check.d2 / *-test.d2)" >&2
	exit 1
}

TMUX_PANE="${pane:-${TMUX_PANE:-}}"
[[ -n $TMUX_PANE ]] || {
	echo "aeye publish-diagram: no pane: pass --pane %<id> or run inside tmux" >&2
	exit 1
}
export TMUX_PANE
# The manifest key carries the tmux server pid. A caller outside tmux (a cron job,
# a systemd unit) has no $TMUX, so ask the server that owns the pane; a pane no
# server knows is an error rather than a manifest nothing reads.
if [[ -z ${TMUX:-} ]]; then
	srv="$(tmux display-message -p -t "$TMUX_PANE" '#{pid}' 2>/dev/null)" && [[ $srv =~ ^[0-9]+$ ]] || {
		echo "aeye publish-diagram: pane $TMUX_PANE not found: run inside tmux or point TMUX at its server" >&2
		exit 1
	}
	export TMUX=",$srv,"
fi
pane_file="$(resolve_pane_key "")"
valid_pane_file "$pane_file" || {
	echo "aeye publish-diagram: invalid pane '$TMUX_PANE'" >&2
	exit 1
}
manifest_paths "$pane_file"

# Serialize two publishes of one file (and the hooks' appends) so the render and
# the manifest rewrite below never interleave. Unlike the hooks this holds the
# lock across the render: a republish loop must never see a half-written png.
mkdir -p "$IMAGES_DIR"
_manifest_lock "$LOCK_FILE"

png="$(d2_png_for "$file" "$DIAGRAMS_DIR" dark)"
svg="${png%.png}.svg"
was_missing=1
[[ -f $png ]] && was_missing=0
if ! d2_render "$file" "$DIAGRAMS_DIR" >/dev/null; then
	echo "aeye publish-diagram: ${D2_RENDER_ERR:-render failed (aeye or resvg missing?)}" >&2
	exit 1
fi
# resvg cannot paint the <foreignObject> d2 emits for |md blocks; see diagrams.sh.
if [[ $was_missing -eq 1 ]] && grep -q '<foreignObject' "$svg"; then
	d2_rm_render_set "$png"
	echo "aeye publish-diagram: $file has |md blocks that render blank; use plain quoted labels" >&2
	exit 1
fi

printf -v now '%(%FT%T%z)T' -1
diagram_replace_entry "$MANIFEST" "$png" "$svg" "$(basename "$file" .d2)" "$now"

if [[ -n $open ]]; then
	"${AEYE_TOGGLE:-tmux-claude-images}" --ensure-open >/dev/null 2>&1 ||
		echo "aeye publish-diagram: --open failed: is tmux-claude-images on PATH?" >&2
fi
