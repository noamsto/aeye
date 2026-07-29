#!/usr/bin/env bats
# tmux discards DCS passthrough written by a pane no client is displaying, so a
# store issued from a hidden window never reaches the terminal while its
# placeholder cells survive in tmux's screen buffer — blank boxes (#133). The
# only honest check is a real tmux server with a real attached client, because
# the drop happens inside tmux and a stub can't reproduce it. Unit tests cover
# the decision; this covers the plumbing that feeds it (an untargeted
# display-message reports window_active=1 from anywhere, which silently made an
# earlier cut of the fix inert).
bats_require_minimum_version 1.5.0

# 1×1 PNG — loadManifest drops entries that don't decode.
TINY_PNG_B64='iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=='

setup() {
	command -v tmux >/dev/null || skip "needs a real tmux server"
	command -v script >/dev/null || skip "needs util-linux script for a pty"
	command -v tic >/dev/null || skip "needs ncurses tic to synthesize a terminfo entry"

	# The viewer only reaches its kitty store path when #{client_termname} looks
	# like kitty, and tmux refuses to attach a client whose TERM has no terminfo
	# entry — which a bare CI runner has no reason to carry. Alias xterm-256color
	# under the name the viewer matches on; only the name is load-bearing.
	export TERMINFO="$BATS_TEST_TMPDIR/terminfo"
	mkdir -p "$TERMINFO"
	printf 'xterm-kitty|kitty stand-in for tests,\n\tuse=xterm-256color,\n' >"$BATS_TEST_TMPDIR/kitty.ti"
	tic -x -o "$TERMINFO" "$BATS_TEST_TMPDIR/kitty.ti"

	export SOCK="aeye-bats-$$"
	export CAP="$BATS_TEST_TMPDIR/client.raw"
	export AEYE_DIR="$BATS_TEST_TMPDIR/state"
	mkdir -p "$AEYE_DIR/images"

	local png="$BATS_TEST_TMPDIR/tiny.png"
	base64 -d <<<"$TINY_PNG_B64" >"$png"
	printf '{"type":"image","path":"%s"}\n' "$png" >"$AEYE_DIR/images/9.jsonl"

	BIN="$BATS_TEST_TMPDIR/aeye"
	(cd "$(dirname "$BATS_TEST_DIRNAME")" && go build -o "$BIN" .)
}

teardown() {
	[ -n "${SCRIPT_PID:-}" ] && kill "$SCRIPT_PID" 2>/dev/null
	[ -n "${SOCK:-}" ] && tmux -L "$SOCK" kill-server 2>/dev/null
	return 0
}

@test "viewer re-stores its images once its tmux window becomes visible" {
	tmux -L "$SOCK" -f /dev/null new-session -d -s A -n w1 -x 120 -y 40 'sleep 60'
	tmux -L "$SOCK" set-option -g allow-passthrough on
	# Off, so this exercises the tick poll rather than the FocusMsg fast path.
	tmux -L "$SOCK" set-option -g focus-events off

	# The viewer picks its backend from #{client_termname}; the captured client
	# has to look like kitty for the kitty store path to run at all.
	TERM=xterm-kitty script -q -f -c "tmux -L $SOCK attach -t A" "$CAP" </dev/null >/dev/null 2>&1 &
	SCRIPT_PID=$!
	sleep 1

	# Start the viewer in a window the client is NOT displaying.
	tmux -L "$SOCK" new-window -d -t A -n w2 "AEYE_DIR=$AEYE_DIR $BIN 9; sleep 60"
	sleep 3
	run ! grep -aq 'a=T' "$CAP" # tmux dropped every store issued while hidden

	tmux -L "$SOCK" select-window -t A:w2
	sleep 4 # one 1.5s tick, with margin
	grep -aq 'a=T' "$CAP"
}
