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

@test "auto: non-numeric width -> side" {
	run env -u AEYE_SPLIT bash "$APP" --resolve-axis abc 50
	[ "$status" -eq 0 ]
	[ "$output" = side ]
}

@test "auto: non-numeric height -> side" {
	run env -u AEYE_SPLIT bash "$APP" --resolve-axis 50 xyz
	[ "$status" -eq 0 ]
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

wezterm_stub_setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	unset TMUX TMUX_PANE KITTY_LISTEN_ON AEYE_HOST
	export WEZTERM_PANE=3 CLAUDE_CODE_SESSION_ID=sess-wz
	echo '{"type":"image","path":"/x.png","source":"d2"}' >"$CLAUDE_STATUS_DIR/images/sess-wz.jsonl"
	STUB="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB"
	printf '#!/usr/bin/env bash\n:\n' >"$STUB/aeye"
	chmod +x "$STUB/aeye"
	export WEZTERM_LOG="$BATS_TEST_TMPDIR/wezterm.log"
	: >"$WEZTERM_LOG"
	export WEZTERM_DIMS="$BATS_TEST_TMPDIR/wdims"
	printf '200 50\n' >"$WEZTERM_DIMS"
	# `list` (bare) is the liveness probe (tabular, no live pane); `list --format
	# json` returns a real pane_id/size.cols/size.rows object so resolve_axis gets
	# driven from $WEZTERM_DIMS, same as the kitty/tmux stubs.
	cat >"$STUB/wezterm" <<'W'
#!/usr/bin/env bash
echo "$*" >>"$WEZTERM_LOG"
case "$2" in
list)
	if [[ "$*" == *"--format json"* ]]; then
		read -r c r <"$WEZTERM_DIMS"
		printf '[{"pane_id":3,"size":{"cols":%s,"rows":%s}}]\n' "$c" "$r"
	else
		printf 'WINID TABID PANEID\n'
	fi
	;;
split-pane) echo 42 ;;
*) : ;;
esac
W
	chmod +x "$STUB/wezterm"
	export PATH="$STUB:$PATH"
}

@test "wezterm: landscape window splits --right (side)" {
	wezterm_stub_setup
	printf '200 50\n' >"$WEZTERM_DIMS"
	run env -u AEYE_SPLIT bash "$APP"
	[ "$status" -eq 0 ]
	grep -q -- '--right' "$WEZTERM_LOG"
}

@test "wezterm: portrait window splits --bottom" {
	wezterm_stub_setup
	printf '90 50\n' >"$WEZTERM_DIMS"
	run env -u AEYE_SPLIT bash "$APP"
	[ "$status" -eq 0 ]
	grep -q -- '--bottom' "$WEZTERM_LOG"
}

iterm_stub_setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	unset TMUX TMUX_PANE KITTY_LISTEN_ON AEYE_HOST WEZTERM_PANE
	export TERM_PROGRAM=iTerm.app CLAUDE_CODE_SESSION_ID=sess-iterm
	echo '{"type":"image","path":"/x.png","source":"d2"}' >"$CLAUDE_STATUS_DIR/images/sess-iterm.jsonl"
	STUB="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB"
	printf '#!/usr/bin/env bash\n:\n' >"$STUB/aeye"
	chmod +x "$STUB/aeye"
	export OSASCRIPT_LOG="$BATS_TEST_TMPDIR/osascript.log"
	: >"$OSASCRIPT_LOG"
	export ITERM_DIMS="$BATS_TEST_TMPDIR/idims"
	printf '200 50\n' >"$ITERM_DIMS"
	# iterm_split's AppleScript source always contains the literal strings
	# "vertically" and "horizontally" (both branches of its if/else), so grepping
	# the -e text can't tell which branch actually ran. The runtime-selected verb
	# is instead passed as a plain argv element after "--" (iterm_split -- "$1"
	# "$2"), so scan for the arg immediately following "--" to observe it. A call
	# with no "--" at all is iterm_dims; answer it from $ITERM_DIMS.
	cat >"$STUB/osascript" <<'O'
#!/usr/bin/env bash
echo "$*" >>"$OSASCRIPT_LOG"
prev="" verb=""
for a in "$@"; do
	[[ $prev == -- ]] && { verb="$a"; break; }
	prev="$a"
done
case "$verb" in
vertically | horizontally) echo "sess-$verb" ;;
"") read -r c r <"$ITERM_DIMS"; echo "$c $r" ;;
*) : ;; # alive/close query; not exercised by these tests
esac
O
	chmod +x "$STUB/osascript"
	export PATH="$STUB:$PATH"
}

@test "iterm: landscape session splits vertically (side)" {
	iterm_stub_setup
	printf '200 50\n' >"$ITERM_DIMS"
	run env -u AEYE_SPLIT bash "$APP"
	[ "$status" -eq 0 ]
	grep -q -- '-- vertically' "$OSASCRIPT_LOG"
}

@test "iterm: portrait session splits horizontally (bottom)" {
	iterm_stub_setup
	printf '90 50\n' >"$ITERM_DIMS"
	run env -u AEYE_SPLIT bash "$APP"
	[ "$status" -eq 0 ]
	grep -q -- '-- horizontally' "$OSASCRIPT_LOG"
}
