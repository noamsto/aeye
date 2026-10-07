#!/usr/bin/env bats

# Manifest locking where the flock(1) command is missing (macOS): the shell
# helpers fall back to `aeye flock-fd`, which locks an fd inherited from the shell.

setup_file() {
	command -v go >/dev/null || skip "go not on PATH"
	ROOT="$(dirname "$BATS_TEST_DIRNAME")"
	(cd "$ROOT" && go build -o "$BATS_FILE_TMPDIR/aeye" .)
}

setup() {
	ROOT="$(dirname "$BATS_TEST_DIRNAME")"
	LIB="$ROOT/adapters/core/manifest-extract.sh"
	# A PATH with aeye and the basics but no flock, whatever the host has.
	NOFLOCK="$BATS_TEST_TMPDIR/noflock"
	mkdir -p "$NOFLOCK"
	cp "$BATS_FILE_TMPDIR/aeye" "$NOFLOCK/aeye"
	for t in bash sleep cat mkdir rm ls wc; do
		ln -s "$(command -v "$t")" "$NOFLOCK/$t"
	done
	LOCK="$BATS_TEST_TMPDIR/m.lock"
	COUNTER="$BATS_TEST_TMPDIR/counter"
}

@test "flock-fd: lock outlives the process until the holding fd closes" {
	run bash -c '
		exec 9>"$1"; "$2" flock-fd 9 || exit 10
		exec 8>"$1"
		"$2" flock-fd 8 -n; echo "second=$?"
		exec 9>&-
		"$2" flock-fd 8 -n; echo "after_close=$?"
	' _ "$LOCK" "$NOFLOCK/aeye"
	[ "$status" -eq 0 ]
	[[ $output == *"second=1"* ]]
	[[ $output == *"after_close=0"* ]]
}

@test "flock-fd: bad fd exits 2 with a message" {
	run "$NOFLOCK/aeye" flock-fd 99
	[ "$status" -eq 2 ]
	[[ $output == *"flock-fd"* ]]
}

# Two writers each do read -> sleep -> write+1 on one counter file.
race() { # $1 = 1 to take _manifest_lock first
	rm -f "$COUNTER" "$COUNTER".ready.*
	printf 0 >"$COUNTER"
	for _ in 1 2; do
		PATH="$NOFLOCK" bash -c '
			source "$1"
			[[ $4 == 1 ]] && _manifest_lock "$2"
			n=$(cat "$3"); : >"$3.ready.$$"
			until [[ $(ls "$3".ready.* 2>/dev/null | wc -l) -ge 2 || $4 == 1 ]]; do sleep 0.05; done
			sleep 0.3; printf %s $((n + 1)) >"$3"
		' _ "$LIB" "$LOCK" "$COUNTER" "$1" &
	done
	wait
	cat "$COUNTER"
}

@test "_manifest_lock without flock: lost update reproduces unlocked" {
	[ "$(race 0)" = 1 ]
}

@test "_manifest_lock without flock: aeye flock-fd serializes two writers" {
	run env PATH="$NOFLOCK" bash -c "command -v flock"
	[ "$status" -ne 0 ]
	[ "$(race 1)" = 2 ]
}

@test "_manifest_lock without flock or aeye stays lock-free and succeeds" {
	rm "$NOFLOCK/aeye"
	# shellcheck disable=SC2016
	run env PATH="$NOFLOCK" bash -c 'source "$1"; _manifest_lock "$2"' _ "$LIB" "$LOCK"
	[ "$status" -eq 0 ]
}
