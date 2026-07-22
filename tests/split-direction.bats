#!/usr/bin/env bats
bats_require_minimum_version 1.5.0

setup() {
	APP="$(dirname "$BATS_TEST_DIRNAME")/scripts/tmux-claude-images.sh"
}

@test "auto: landscape window (cols > 2*rows) -> side" {
	run env -u AEYE_SPLIT bash "$APP" --resolve-axis 200 50
	[ "$status" -eq 0 ]
	[ "$output" = side ]
}

@test "auto: portrait-ish window (cols <= 2*rows) -> bottom" {
	run env -u AEYE_SPLIT bash "$APP" --resolve-axis 90 50
	[ "$output" = bottom ]
}

@test "auto: boundary cols == 2*rows -> bottom" {
	run env -u AEYE_SPLIT bash "$APP" --resolve-axis 100 50
	[ "$output" = bottom ]
}

@test "auto: unreadable dims -> side (no regression)" {
	run env -u AEYE_SPLIT bash "$APP" --resolve-axis "" ""
	[ "$output" = side ]
}

@test "auto: zero dims -> side" {
	run env -u AEYE_SPLIT bash "$APP" --resolve-axis 0 0
	[ "$output" = side ]
}

@test "AEYE_SPLIT=side forces side on a portrait window" {
	AEYE_SPLIT=side run bash "$APP" --resolve-axis 90 50
	[ "$output" = side ]
}

@test "AEYE_SPLIT=bottom forces bottom on a landscape window" {
	AEYE_SPLIT=bottom run bash "$APP" --resolve-axis 200 50
	[ "$output" = bottom ]
}

@test "AEYE_SPLIT=garbage falls back to auto" {
	AEYE_SPLIT=garbage run bash "$APP" --resolve-axis 200 50
	[ "$output" = side ]
}

tmux_stub_setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	export TMUX_PANE='%9' TMUX='/tmp/fake-tmux,123,0'
	echo '{"type":"image","path":"/x.png","source":"d2"}' >"$CLAUDE_STATUS_DIR/images/9.jsonl"
	STUB="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB"
	printf '#!/usr/bin/env bash\n:\n' >"$STUB/aeye"
	chmod +x "$STUB/aeye"
	export SPLIT_LOG="$BATS_TEST_TMPDIR/split.log"
	: >"$SPLIT_LOG"
	export WIN_DIMS="$BATS_TEST_TMPDIR/dims"
	printf '200 50\n' >"$WIN_DIMS"
	cat >"$STUB/tmux" <<'T'
#!/usr/bin/env bash
case "${1:-}" in
list-panes) : ;;                                            # no existing viewer
display-message) [[ "$*" == *window_width* ]] && cat "$WIN_DIMS" ;;
split-window) printf 'split-window %s\n' "$*" >>"$SPLIT_LOG"; echo '%77' ;;
set-option) printf 'set-option %s\n' "$*" >>"$SPLIT_LOG" ;;
esac
exit 0
T
	chmod +x "$STUB/tmux"
	export PATH="$STUB:$PATH"
}

@test "tmux: landscape window opens a side split (-h) and records axis=side" {
	tmux_stub_setup
	printf '200 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT bash "$APP"
	[ "$status" -eq 0 ]
	grep -q 'split-window -h ' "$SPLIT_LOG"
	grep -q 'set-option .*@claude_img_axis side' "$SPLIT_LOG"
}

@test "tmux: portrait window opens a bottom split (-v) and records axis=bottom" {
	tmux_stub_setup
	printf '90 50\n' >"$WIN_DIMS"
	run env -u AEYE_SPLIT bash "$APP"
	[ "$status" -eq 0 ]
	grep -q 'split-window -v ' "$SPLIT_LOG"
	grep -q 'set-option .*@claude_img_axis bottom' "$SPLIT_LOG"
}

@test "tmux: AEYE_SPLIT=bottom forces -v on a landscape window" {
	tmux_stub_setup
	printf '200 50\n' >"$WIN_DIMS"
	AEYE_SPLIT=bottom run bash "$APP"
	grep -q 'split-window -v ' "$SPLIT_LOG"
}

kitty_stub_setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	export AEYE_HOST=kitty TMUX_PANE='%9' TMUX='/tmp/fake-tmux,123,0'
	unset KITTY_WINDOW_ID
	echo '{"type":"image","path":"/x.png","source":"d2"}' >"$CLAUDE_STATUS_DIR/images/9.jsonl"
	STUB="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB"
	printf '#!/usr/bin/env bash\n:\n' >"$STUB/aeye"
	chmod +x "$STUB/aeye"
	export KITTY_LOG="$BATS_TEST_TMPDIR/kitty.log"
	: >"$KITTY_LOG"
	export KITTY_DIMS="$BATS_TEST_TMPDIR/kdims"
	printf '90 50\n' >"$KITTY_DIMS"
	# `@ ls` (bare) returns one os-window whose active window has the configured
	# columns/lines; `@ ls --match ...` = no match (exit 1); launch is logged.
	cat >"$STUB/kitty" <<'K'
#!/usr/bin/env bash
shift; sub="$1"; shift
case "$sub" in
ls)
	[[ "${1:-}" == "--match" ]] && exit 1
	read -r c l <"$KITTY_DIMS"
	printf '[{"tabs":[{"id":1,"is_focused":true,"windows":[{"id":1,"is_focused":true,"columns":%s,"lines":%s}]}]}]\n' "$c" "$l"
	;;
goto-layout) : ;;
launch) printf 'launch %s\n' "$*" >>"$KITTY_LOG" ;;
*) printf '%s %s\n' "$sub" "$*" >>"$KITTY_LOG" ;;
esac
K
	chmod +x "$STUB/kitty"
	# tmux stub: _key_on_screen must report %9 on the active attached window.
	cat >"$STUB/tmux" <<'T'
#!/usr/bin/env bash
[[ "${1:-}" == list-panes ]] && printf '%%9 1 1\n'
exit 0
T
	chmod +x "$STUB/tmux"
	export PATH="$STUB:$PATH"
}

@test "kitty: portrait window uses hsplit (bottom)" {
	kitty_stub_setup
	printf '90 50\n' >"$KITTY_DIMS"
	run env -u AEYE_SPLIT bash "$APP"
	[ "$status" -eq 0 ]
	grep -q 'location=hsplit' "$KITTY_LOG"
}

@test "kitty: landscape window uses vsplit (side)" {
	kitty_stub_setup
	printf '200 50\n' >"$KITTY_DIMS"
	run env -u AEYE_SPLIT bash "$APP"
	grep -q 'location=vsplit' "$KITTY_LOG"
}

@test "kitty: matched window missing columns/lines falls back to vsplit, no crash" {
	kitty_stub_setup
	export KITTY_WINDOW_ID=1
	# Matched window (id 1) has no columns/lines at all — regression check for
	# the jq // 0 default: without it this interpolates the literal string
	# "null null" and resolve_axis's (( )) arithmetic aborts under set -u.
	cat >"$STUB/kitty" <<'K'
#!/usr/bin/env bash
shift; sub="$1"; shift
case "$sub" in
ls)
	[[ "${1:-}" == "--match" ]] && exit 1
	printf '[{"tabs":[{"id":1,"is_focused":true,"windows":[{"id":1,"is_focused":true}]}]}]\n'
	;;
goto-layout) : ;;
launch) printf 'launch %s\n' "$*" >>"$KITTY_LOG" ;;
*) printf '%s %s\n' "$sub" "$*" >>"$KITTY_LOG" ;;
esac
K
	chmod +x "$STUB/kitty"
	run env -u AEYE_SPLIT bash "$APP"
	[ "$status" -eq 0 ]
	grep -q 'location=vsplit' "$KITTY_LOG"
}
