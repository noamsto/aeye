#!/usr/bin/env bats
# shellcheck disable=SC2030,SC2031  # bats wraps each @test in a subshell; export is intentional

setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	mkdir -p "$CLAUDE_STATUS_DIR"
	APP="$(dirname "$BATS_TEST_DIRNAME")/scripts/tmux-claude-images.sh"

	# Stub kitty: `@ ls` prints the fixture; everything else is logged for asserts.
	STUB="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$STUB"
	cat >"$STUB/kitty" <<'K'
#!/usr/bin/env bash
shift                       # drop leading '@'
cmd="${1:-}"; shift || true
case "$cmd" in
ls) cat "$KITTY_LS_JSON" 2>/dev/null || echo '[]' ;;
*)  printf '%s\n' "$cmd $*" >>"$KITTY_LOG" ;;
esac
K
	# Stub tmux: only list-panes is used by reconcile.
	cat >"$STUB/tmux" <<'T'
#!/usr/bin/env bash
case "${1:-}" in
list-panes) cat "$VISIBLE_PANES" 2>/dev/null ;;
*) : ;;
esac
T
	chmod +x "$STUB/kitty" "$STUB/tmux"
	export PATH="$STUB:$PATH"
	export KITTY_LOG="$BATS_TEST_TMPDIR/kitty.log"
	export VISIBLE_PANES="$BATS_TEST_TMPDIR/visible"
	export KITTY_LS_JSON="$BATS_TEST_TMPDIR/ls.json"
	: >"$KITTY_LOG"
	FIX="$BATS_TEST_DIRNAME/fixtures"
}

@test "reconcile is a no-op when neither AEYE_HOST nor a kitty socket is present" {
	unset AEYE_HOST KITTY_LISTEN_ON
	run bash "$APP" --reconcile
	[ "$status" -eq 0 ]
	[ ! -s "$KITTY_LOG" ] # never touched kitty
}

@test "reconcile engages via KITTY_LISTEN_ON when AEYE_HOST is unset" {
	unset AEYE_HOST
	export KITTY_LISTEN_ON=unix:/tmp/kitty-test
	cp "$FIX/kitty-ls-active-9.json" "$KITTY_LS_JSON" # %9 shown, no stash tab
	printf '%%5\n' >"$VISIBLE_PANES"                  # visible window has %5, not %9
	run bash "$APP" --reconcile
	[ "$status" -eq 0 ]
	grep -q 'detach-window --match var:claude_img_src=%9 --target-tab var:aeye_stash=1' "$KITTY_LOG"
}

@test "a carousel whose pane is off-screen is stashed" {
	export AEYE_HOST=kitty
	cp "$FIX/kitty-ls-active-9.json" "$KITTY_LS_JSON" # %9 shown, no stash tab
	printf '%%5\n' >"$VISIBLE_PANES"                  # visible window has %5, not %9
	run bash "$APP" --reconcile
	[ "$status" -eq 0 ]
	grep -q 'detach-window --match var:claude_img_src=%9 --target-tab var:aeye_stash=1' "$KITTY_LOG"
}

@test "stashing lazily creates the stash tab when none exists" {
	export AEYE_HOST=kitty
	cp "$FIX/kitty-ls-active-9.json" "$KITTY_LS_JSON"
	printf '%%5\n' >"$VISIBLE_PANES"
	run bash "$APP" --reconcile
	grep -q 'launch --type=tab .*aeye_stash=1' "$KITTY_LOG"
}

@test "a stashed carousel whose pane is on-screen is brought back beside the host" {
	export AEYE_HOST=kitty
	cp "$FIX/kitty-ls-stashed-9.json" "$KITTY_LS_JSON" # %9 parked in the stash tab
	printf '%%9\n' >"$VISIBLE_PANES"                   # visible window has %9
	run bash "$APP" --reconcile
	[ "$status" -eq 0 ]
	grep -q 'goto-layout --match id:1 splits' "$KITTY_LOG" # host tab -> splits
	grep -q 'detach-window --match var:claude_img_src=%9 --target-tab id:1' "$KITTY_LOG"
}

@test "a steady state mutates nothing (idempotent)" {
	export AEYE_HOST=kitty
	cp "$FIX/kitty-ls-active-9.json" "$KITTY_LS_JSON" # %9 already shown, not stashed
	printf '%%9\n' >"$VISIBLE_PANES"                  # and %9 is on-screen
	run bash "$APP" --reconcile
	[ "$status" -eq 0 ]
	run grep -q 'detach-window' "$KITTY_LOG"
	[ "$status" -ne 0 ] # no window moved
	run grep -q 'launch --type=tab' "$KITTY_LOG"
	[ "$status" -ne 0 ] # no stash tab created
}
