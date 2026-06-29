#!/usr/bin/env bats
# shellcheck disable=SC2030,SC2031  # bats wraps each @test in a subshell; export is intentional
bats_require_minimum_version 1.5.0

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	APP="$(dirname "$BATS_TEST_DIRNAME")/scripts/tmux-claude-images.sh"
	export AEYE_HOST=kitty TMUX_PANE='%9'
	# Non-empty manifest for %9 so launch gets past the "no images" guard.
	echo '{"type":"image","path":"/x.png","source":"d2"}' >"$CLAUDE_STATUS_DIR/images/9.jsonl"

	STUB="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB"
	export KITTY_LOG="$BATS_TEST_TMPDIR/kitty.log"
	: >"$KITTY_LOG"
	export VISIBLE_ROWS="$BATS_TEST_TMPDIR/visible"
	# Viewer stub so VIEWER_BIN resolves on PATH.
	printf '#!/usr/bin/env bash\n:\n' >"$STUB/aeye"
	chmod +x "$STUB/aeye"
	# kitty stub: bare `@ ls` = reachable (echo []); `@ ls --match ...` = no match
	# (exit 1) so launch_kitty proceeds; launch/other subcommands are logged.
	cat >"$STUB/kitty" <<'K'
#!/usr/bin/env bash
shift            # drop '@'
sub="$1"; shift  # drop subcommand
case "$sub" in
ls) [[ "${1:-}" == "--match" ]] && exit 1; echo '[]' ;;
launch) printf 'launch %s\n' "$*" >>"$KITTY_LOG" ;;
*) printf '%s %s\n' "$sub" "$*" >>"$KITTY_LOG" ;;
esac
K
	# tmux stub: list-panes emits the configured rows (ignores -a/-F shape).
	cat >"$STUB/tmux" <<'T'
#!/usr/bin/env bash
[[ "${1:-}" == list-panes ]] && cat "$VISIBLE_ROWS" 2>/dev/null
exit 0
T
	chmod +x "$STUB/kitty" "$STUB/tmux"
	export PATH="$STUB:$PATH"
}

@test "ensure-open launches stashed (and never steals focus) when off-screen" {
	printf '%%9 0 1\n' >"$VISIBLE_ROWS" # %9 present but window_active=0
	run bash "$APP" --ensure-open
	[ "$status" -eq 0 ]
	grep -q 'launch --type=window --match var:aeye_stash=1 .*--keep-focus.*claude_img_src=%9' "$KITTY_LOG"
}

@test "ensure-open launches a visible vsplit (keep-focus) when on-screen" {
	printf '%%9 1 1\n' >"$VISIBLE_ROWS" # %9 is the active window of an attached session
	run bash "$APP" --ensure-open
	[ "$status" -eq 0 ]
	grep -q 'launch --type=window .*--location=vsplit.*--keep-focus.*claude_img_src=%9' "$KITTY_LOG"
	run ! grep -q 'launch --type=window --match var:aeye_stash=1 .*claude_img_src=%9' "$KITTY_LOG"
}
