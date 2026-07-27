# Width-aware carousel split direction — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Open the image carousel split along the terminal window's longer axis (side vs. bottom) instead of always side, with an `AEYE_SPLIT` override and a tmux-only `s` live toggle.

**Architecture:** The launch-time axis decision lives in one bash resolver (`resolve_axis`) in `scripts/tmux-claude-images.sh`; each backend measures its host window via an API it already calls, then maps the semantic axis to its own split flag. The viewer records the chosen axis in a `@claude_img_axis` tmux pane option; pressing `s` flips it in place with `tmux move-pane`, so the viewer process survives and repaints on the resize.

**Tech Stack:** Bash (POSIX arithmetic, bats tests), Go (Bubble Tea viewer, `go test`), tmux/kitty/wezterm/iTerm2 CLIs.

Spec: `docs/superpowers/specs/2026-07-22-carousel-split-direction-design.md` · Issue [#144](https://github.com/noamsto/aeye/issues/144)

## Global Constraints

- **Semantic axis mapping** (used verbatim everywhere):
  - **SIDE** (left │ right): tmux `-h`, kitty `vsplit`, wezterm `--right`, iterm `split vertically`
  - **BOTTOM** (top / bottom): tmux `-v`, kitty `hsplit`, wezterm `--bottom`, iterm `split horizontally`
- **Cell-aspect constant** `CELL_ASPECT=2` — a terminal cell is ~2× taller than wide. Auto rule: `cols > 2·rows` → SIDE, otherwise → BOTTOM. Boundary `cols == 2·rows` → BOTTOM.
- **Fallback:** unreadable / non-positive dims → SIDE (today's behavior; no regression).
- **`AEYE_SPLIT`** values: `auto` (default, unset ⇒ auto), `side`, `bottom`. Any other value is treated as `auto`.
- **Live `s` toggle is tmux-only in v1**; a no-op on every other backend. kitty/wezterm/iterm live toggle is a deferred fast-follow.
- **ghostty is out of scope** — it opens a separate window, not a split, so no axis applies. Do not modify `launch_ghostty`.
- **bash must stay 3.2-safe** (macOS system bash): no `${x^^}`, no associative arrays; `printf -v`, indexed arrays, and `(( ))` are fine.
- **Every edited `.sh` must pass `shellcheck`** (pre-commit runs it).
- **No new Go dependencies** — `os`, `exec`, `strings`, `fmt` are already imported in `gallery.go`; `go.mod` does not change, so no nix `vendorHash` refresh.
- **Out-of-tmux dimension reads** use each terminal's own API (`kitty @ ls`, `wezterm cli list`, iTerm AppleScript) — never `tput`/`$COLUMNS`/`$LINES`.

All commands below run inside the direnv devShell of the worktree
`~/Data/git/.worktrees/noamsto/aeye/feat-144-carousel-split-direction`
(`direnv exec . <cmd>`, or after `direnv allow .`).

---

### Task 1: `resolve_axis` heuristic + test seam

The pure axis decision and a `--resolve-axis` seam so bats can exercise it directly (mirrors the existing `--resolve` seam).

**Files:**
- Modify: `scripts/tmux-claude-images.sh` (add `resolve_axis` near the top after `IMAGES_DIR`; add the `--resolve-axis` seam in `main`)
- Test: `tests/split-direction.bats` (create)

**Interfaces:**
- Produces: `resolve_axis WIDTH HEIGHT` → prints `side` or `bottom`. Honors `AEYE_SPLIT`. Consumed by every `launch_*` in Tasks 2–4.
- Produces: `bash tmux-claude-images.sh --resolve-axis W H` → prints the resolved axis and exits 0.

- [ ] **Step 1: Write the failing bats tests**

Create `tests/split-direction.bats`:

```bash
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `direnv exec . bats tests/split-direction.bats`
Expected: FAIL — the `--resolve-axis` seam doesn't exist, so `main` runs the normal path (exits before printing an axis).

- [ ] **Step 3: Add `resolve_axis` and the seam**

In `scripts/tmux-claude-images.sh`, after the `ENSURE_OPEN=""` line (currently `:19`) add:

```bash
# A terminal cell is roughly twice as tall as it is wide, so the window's pixel
# aspect ratio is cols : (CELL_ASPECT * rows). Split the longer pixel axis so
# both panes stay usable: a landscape window splits SIDE, a portrait one BOTTOM.
readonly CELL_ASPECT=2

# resolve_axis WIDTH HEIGHT -> "side" | "bottom".
# AEYE_SPLIT=side|bottom forces the axis; unset/auto/any-other-value measures.
# Empty or non-positive dims fall back to "side" (today's behavior; no regression).
resolve_axis() {
	case "${AEYE_SPLIT:-auto}" in
	side | bottom)
		printf '%s\n' "$AEYE_SPLIT"
		return
		;;
	esac
	local w=${1:-0} h=${2:-0}
	if ((w > 0 && h > 0 && w <= CELL_ASPECT * h)); then
		printf 'bottom\n'
	else
		printf 'side\n'
	fi
}
```

In `main()`, immediately after the `--reconcile` block (currently `:414-417`) add:

```bash
	if [[ ${1:-} == --resolve-axis ]]; then # test seam: resolve axis from given dims
		resolve_axis "${2:-}" "${3:-}"
		return
	fi
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `direnv exec . bats tests/split-direction.bats`
Expected: PASS (8 tests).

- [ ] **Step 5: shellcheck**

Run: `direnv exec . shellcheck scripts/tmux-claude-images.sh`
Expected: no output (clean).

- [ ] **Step 6: Commit**

```bash
git add scripts/tmux-claude-images.sh tests/split-direction.bats
git commit -m "feat(carousel): add resolve_axis split heuristic + test seam (#144)"
```

---

### Task 2: tmux launch honors the axis and records it

Measure the host window, pick `-h`/`-v`, and store the chosen axis in a `@claude_img_axis` pane option so the viewer's `s` toggle (Task 5) knows the current state.

**Files:**
- Modify: `scripts/tmux-claude-images.sh` — `launch_tmux()` (currently `:69-101`)
- Test: `tests/split-direction.bats` (append)

**Interfaces:**
- Consumes: `resolve_axis` (Task 1).
- Produces: viewer pane carries option `@claude_img_axis = side|bottom` (read by `tmuxPaneAxis` in Task 5).

- [ ] **Step 1: Write the failing e2e tests**

Append to `tests/split-direction.bats`:

```bash
tmux_stub_setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	export TMUX_PANE='%9' TMUX='/tmp/fake-tmux,123,0'
	echo '{"type":"image","path":"/x.png","source":"d2"}' >"$CLAUDE_STATUS_DIR/images/9.jsonl"
	STUB="$BATS_TEST_TMPDIR/bin"; mkdir -p "$STUB"
	printf '#!/usr/bin/env bash\n:\n' >"$STUB/aeye"; chmod +x "$STUB/aeye"
	export SPLIT_LOG="$BATS_TEST_TMPDIR/split.log"; : >"$SPLIT_LOG"
	export WIN_DIMS="$BATS_TEST_TMPDIR/dims"; printf '200 50\n' >"$WIN_DIMS"
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
```

- [ ] **Step 2: Run to verify they fail**

Run: `direnv exec . bats tests/split-direction.bats`
Expected: the three new tests FAIL — `split-window -h ` is emitted unconditionally today and no `@claude_img_axis` option is set.

- [ ] **Step 3: Wire the axis into `launch_tmux`**

In `launch_tmux()`, replace the split + option lines (currently `:96-100`):

```bash
	local cmd
	printf -v cmd '%q %q' "$VIEWER_BIN" "$KEY"
	viewer="$(tmux split-window -h "${detach[@]}" ${env_args[@]+"${env_args[@]}"} -t "$KEY" -P -F '#{pane_id}' "$cmd")"
	tmux set-option -p -t "$viewer" @claude_img_src "$KEY"
```

with:

```bash
	local cmd
	printf -v cmd '%q %q' "$VIEWER_BIN" "$KEY"
	# Split along the host window's longer axis (overridable via AEYE_SPLIT); record
	# the choice as a pane option so the viewer's `s` toggle knows the current state.
	local w h axis flag
	read -r w h < <(tmux display-message -p -t "$KEY" '#{window_width} #{window_height}' 2>/dev/null || true)
	axis="$(resolve_axis "$w" "$h")"
	[[ $axis == bottom ]] && flag=-v || flag=-h
	viewer="$(tmux split-window "$flag" "${detach[@]}" ${env_args[@]+"${env_args[@]}"} -t "$KEY" -P -F '#{pane_id}' "$cmd")"
	tmux set-option -p -t "$viewer" @claude_img_src "$KEY"
	tmux set-option -p -t "$viewer" @claude_img_axis "$axis"
```

- [ ] **Step 4: Run to verify they pass**

Run: `direnv exec . bats tests/split-direction.bats`
Expected: PASS (11 tests total).

- [ ] **Step 5: shellcheck**

Run: `direnv exec . shellcheck scripts/tmux-claude-images.sh`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add scripts/tmux-claude-images.sh tests/split-direction.bats
git commit -m "feat(carousel): tmux splits along longer axis, records @claude_img_axis (#144)"
```

---

### Task 3: kitty launch honors the axis

Measure the host kitty window from the `kitty @ ls` call `kitty_place_args` already makes, and choose `vsplit`/`hsplit`. Because `kitty_place_args` is shared with the reconcile/stash path, launch and reconcile stay consistent for free.

**Files:**
- Modify: `scripts/tmux-claude-images.sh` — `kitty_place_args()` (currently `:113-127`)
- Test: `tests/split-direction.bats` (append)

**Interfaces:**
- Consumes: `resolve_axis` (Task 1).
- Produces: kitty placement carries `--location=vsplit` (SIDE) or `--location=hsplit` (BOTTOM).

- [ ] **Step 1: Write the failing test**

Append to `tests/split-direction.bats`:

```bash
kitty_stub_setup() {
	export CLAUDE_STATUS_DIR="$BATS_TEST_TMPDIR/state"
	mkdir -p "$CLAUDE_STATUS_DIR/images"
	export AEYE_HOST=kitty TMUX_PANE='%9' TMUX='/tmp/fake-tmux,123,0'
	unset KITTY_WINDOW_ID
	echo '{"type":"image","path":"/x.png","source":"d2"}' >"$CLAUDE_STATUS_DIR/images/9.jsonl"
	STUB="$BATS_TEST_TMPDIR/bin"; mkdir -p "$STUB"
	printf '#!/usr/bin/env bash\n:\n' >"$STUB/aeye"; chmod +x "$STUB/aeye"
	export KITTY_LOG="$BATS_TEST_TMPDIR/kitty.log"; : >"$KITTY_LOG"
	export KITTY_DIMS="$BATS_TEST_TMPDIR/kdims"; printf '90 50\n' >"$KITTY_DIMS"
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `direnv exec . bats tests/split-direction.bats`
Expected: the portrait case FAILs — placement is hardcoded to `vsplit` today.

- [ ] **Step 3: Wire the axis into `kitty_place_args`**

Replace `kitty_place_args()` (currently `:113-127`) with:

```bash
kitty_place_args() {
	local tab="" win_dims="" w="" h="" axis loc
	if [[ -n ${KITTY_WINDOW_ID:-} ]]; then
		tab="$(kitty @ ls 2>/dev/null |
			jq -r --argjson w "$KITTY_WINDOW_ID" \
				'first(.[].tabs[] | select(any(.windows[]; .id == $w)) | .id) // empty')"
		win_dims="$(kitty @ ls 2>/dev/null |
			jq -r --argjson w "$KITTY_WINDOW_ID" \
				'first(.[].tabs[].windows[] | select(.id == $w) | "\(.columns) \(.lines)") // empty')"
	fi
	# Fall back to the focused window's geometry when the host id is stale/unset.
	[[ -z $win_dims ]] && win_dims="$(kitty @ ls 2>/dev/null |
		jq -r 'first(.[].tabs[] | select(.is_focused) | .windows[] | select(.is_focused) |
			"\(.columns) \(.lines)") // empty')"
	read -r w h <<<"$win_dims"
	axis="$(resolve_axis "$w" "$h")"
	[[ $axis == bottom ]] && loc=hsplit || loc=vsplit
	if [[ -n $tab ]]; then
		kitty @ goto-layout --match "id:$tab" splits >/dev/null 2>&1 || true
		printf '%s\0' --match "id:$tab" --location="$loc" --next-to "id:$KITTY_WINDOW_ID" --keep-focus
	else
		kitty @ goto-layout splits >/dev/null 2>&1 || true
		printf '%s\0' --location="$loc" --keep-focus
	fi
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `direnv exec . bats tests/split-direction.bats`
Expected: PASS (13 tests total).

- [ ] **Step 5: Confirm the existing kitty suite still passes**

Run: `direnv exec . bats tests/launch-hidden.bats`
Expected: PASS — those tests set no dims, so `resolve_axis "" ""` falls back to `side` ⇒ `vsplit`, matching their `--location=vsplit` assertions.

- [ ] **Step 6: shellcheck + commit**

Run: `direnv exec . shellcheck scripts/tmux-claude-images.sh`

```bash
git add scripts/tmux-claude-images.sh tests/split-direction.bats
git commit -m "feat(carousel): kitty splits along longer axis (vsplit/hsplit) (#144)"
```

---

### Task 4: wezterm and iTerm2 launches honor the axis

Both measure via an API they already touch; no bats stubs exist for these backends in the suite, so this task is verified by `shellcheck` plus the manual checks at the end of the plan.

**Files:**
- Modify: `scripts/tmux-claude-images.sh` — `launch_wezterm()` (`:193-217`), `iterm_split()` (`:250-261`), `launch_iterm()` (`:300-319`)

**Interfaces:**
- Consumes: `resolve_axis` (Task 1).

- [ ] **Step 1: wezterm — measure host pane, pick `--right`/`--bottom`**

In `launch_wezterm()`, replace the split line (currently `:214-215`):

```bash
	pane="$(wezterm cli split-pane --right --percent 40 --cwd "$STATE_DIR" -- \
		env ${dbg[@]+"${dbg[@]}"} AEYE_DIR="$STATE_DIR" CLAUDE_STATUS_DIR="$STATE_DIR" "$VIEWER_BIN" "$KEY")"
```

with:

```bash
	# Host pane size in cells: `wezterm cli list` prints SIZE as "COLSxROWS" for the
	# $WEZTERM_PANE row. Split its longer axis (overridable via AEYE_SPLIT).
	local w h axis dir
	read -r w h < <(wezterm cli list --format json 2>/dev/null |
		jq -r --argjson p "${WEZTERM_PANE:-0}" \
			'first(.[] | select(.pane_id == $p) | "\(.size.cols) \(.size.rows)") // empty')
	axis="$(resolve_axis "$w" "$h")"
	[[ $axis == bottom ]] && dir=--bottom || dir=--right
	pane="$(wezterm cli split-pane "$dir" --percent 40 --cwd "$STATE_DIR" -- \
		env ${dbg[@]+"${dbg[@]}"} AEYE_DIR="$STATE_DIR" CLAUDE_STATUS_DIR="$STATE_DIR" "$VIEWER_BIN" "$KEY")"
```

- [ ] **Step 2: iTerm2 — add a dims query and a directional split verb**

`iterm_split` currently hardcodes `split horizontally`. Make the verb a parameter and add a dims helper. Replace `iterm_split()` (`:250-261`) with:

```bash
# iterm_dims -> "COLUMNS ROWS" of the current session (empty on failure).
iterm_dims() {
	osascript \
		-e 'tell application "iTerm2"' \
		-e 'tell current session of current window' \
		-e 'return (columns as text) & " " & (rows as text)' \
		-e 'end tell' \
		-e 'end tell' 2>/dev/null || true
}

# iterm_split VERB COMMAND -> new session id. VERB is "vertically" (SIDE) or
# "horizontally" (BOTTOM).
iterm_split() {
	osascript \
		-e 'on run argv' \
		-e 'tell application "iTerm2"' \
		-e 'tell current session of current window' \
		-e 'if (item 1 of argv) is "vertically" then' \
		-e 'set s to (split vertically with default profile command (item 2 of argv))' \
		-e 'else' \
		-e 'set s to (split horizontally with default profile command (item 2 of argv))' \
		-e 'end if' \
		-e 'end tell' \
		-e 'return id of s' \
		-e 'end tell' \
		-e 'end run' \
		-- "$1" "$2"
}
```

In `launch_iterm()`, replace the split call (currently `:317`):

```bash
	session="$(iterm_split "$cmd")"
```

with:

```bash
	local w h axis verb
	read -r w h < <(iterm_dims)
	axis="$(resolve_axis "$w" "$h")"
	[[ $axis == bottom ]] && verb=horizontally || verb=vertically
	session="$(iterm_split "$verb" "$cmd")"
```

- [ ] **Step 3: shellcheck**

Run: `direnv exec . shellcheck scripts/tmux-claude-images.sh`
Expected: clean. (If it flags the `read -r w h < <(...)` process-substitution word-splitting, that is intentional and correct here.)

- [ ] **Step 4: Regression-check the bash test suites**

Run: `direnv exec . bats tests/`
Expected: PASS — no wezterm/iterm tests exist, and the tmux/kitty tests are unaffected.

- [ ] **Step 5: Commit**

```bash
git add scripts/tmux-claude-images.sh
git commit -m "feat(carousel): wezterm/iterm split along longer axis (#144)"
```

---

### Task 5: live `s` toggle (tmux), key hint, and docs

The viewer flips its split axis in place via `tmux move-pane`, reads the launcher's recorded axis at startup, updates the hint row, and the README documents both `s` and `AEYE_SPLIT`.

**Files:**
- Create: `gallery_split.go`
- Create: `gallery_split_test.go`
- Modify: `gallery.go` — `galleryModel` struct (`:98`), `Update` key switch (`:415`), `runGallery` model init (`:946`), `actionRow` hint (`:760`)
- Modify: `README.md` — keybinding table (~`:136`) and env-var section (~`:195`)

**Interfaces:**
- Consumes: `@claude_img_axis` pane option (Task 2); `m.pane` (host pane id in tmux mode).
- Produces: `flipAxis(cur string) (next, tmuxFlag string)`; `tmuxPaneAxis() string`; `(*galleryModel).toggleSplitAxis()`.

- [ ] **Step 1: Write the failing Go test**

Create `gallery_split_test.go`:

```go
package main

import "testing"

func TestFlipAxis(t *testing.T) {
	cases := []struct{ cur, wantNext, wantFlag string }{
		{"side", "bottom", "-v"},
		{"bottom", "side", "-h"},
		{"", "bottom", "-v"}, // unset defaults to a side layout, so it flips to bottom
		{"garbage", "bottom", "-v"},
	}
	for _, c := range cases {
		next, flag := flipAxis(c.cur)
		if next != c.wantNext || flag != c.wantFlag {
			t.Errorf("flipAxis(%q) = (%q,%q); want (%q,%q)", c.cur, next, flag, c.wantNext, c.wantFlag)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `direnv exec . go test ./... -run TestFlipAxis`
Expected: FAIL — `flipAxis` undefined (build error).

- [ ] **Step 3: Create `gallery_split.go`**

```go
package main

import (
	"os"
	"os/exec"
	"strings"
)

// flipAxis returns the opposite split axis and the tmux split-window flag that
// produces it via move-pane. Anything other than "bottom" is treated as a side
// layout, so the first press always lands on "bottom".
func flipAxis(cur string) (next, tmuxFlag string) {
	if cur == "bottom" {
		return "side", "-h"
	}
	return "bottom", "-v"
}

// tmuxPaneAxis reads the @claude_img_axis pane option the launcher recorded, or
// "side" when unset or off-tmux (the historical default).
func tmuxPaneAxis() string {
	out, err := exec.Command("tmux", "show-options", "-p", "-qv", "@claude_img_axis").Output()
	if err != nil {
		return "side"
	}
	if strings.TrimSpace(string(out)) == "bottom" {
		return "bottom"
	}
	return "side"
}

// toggleSplitAxis flips the carousel between a side (left|right) and bottom
// (top/bottom) split of its tmux host. tmux-only: move-pane re-splits the host
// in place, so the viewer process survives and repaints on the resize it
// receives. A no-op off-tmux — there m.pane is a session id, and the other
// backends have no in-place axis flip in v1.
func (m *galleryModel) toggleSplitAxis() {
	if os.Getenv("TMUX") == "" {
		return
	}
	next, flag := flipAxis(m.splitAxis)
	out, err := exec.Command("tmux", "display-message", "-p", "#{pane_id}").Output()
	if err != nil {
		return
	}
	self := strings.TrimSpace(string(out))
	if err := exec.Command("tmux", "move-pane", flag, "-s", self, "-t", m.pane).Run(); err != nil {
		return
	}
	m.splitAxis = next
	_ = exec.Command("tmux", "set-option", "-p", "-t", self, "@claude_img_axis", next).Run()
}
```

- [ ] **Step 4: Run to verify the test passes**

Run: `direnv exec . go test ./... -run TestFlipAxis`
Expected: PASS.

- [ ] **Step 5: Add the `splitAxis` field**

In `gallery.go`, in the `galleryModel` struct (`:98`), add after `pane string` (`:99`):

```go
	splitAxis    string // "side" | "bottom"; current tmux split, toggled by `s`
```

- [ ] **Step 6: Initialize it in `runGallery`**

In `runGallery` (`:946`), add to the `galleryModel{...}` literal (next to `pane: pane,`):

```go
		splitAxis:    tmuxPaneAxis(),
```

- [ ] **Step 7: Handle the `s` key**

In `Update`, in the `switch msg.String()` block, add a case (e.g. after the `case "u":` block near `:508`):

```go
		case "s":
			m.toggleSplitAxis()
			return m, nil
```

- [ ] **Step 8: Add `s` to the hint row**

In `actionRow` (`:760`), change:

```go
	actionKeys := "↵ open · O folder · y copy · d drag · x del · r reload · q quit"
```

to:

```go
	actionKeys := "↵ open · O folder · y copy · d drag · x del · s split · r reload · q quit"
```

- [ ] **Step 9: Build and run the full Go suite**

Run: `direnv exec . go build ./... && direnv exec . go test ./...`
Expected: build succeeds; all tests PASS.

- [ ] **Step 10: Document `s` and `AEYE_SPLIT` in the README**

In the keybinding table (~`:136`), add a row (match the existing table's column style):

```markdown
| `s` | Toggle the carousel between a side and bottom split (tmux only) |
```

In the environment/config section (~`:195`, near the `AEYE_HOST` docs), add:

```markdown
- `AEYE_SPLIT` — carousel split direction: `auto` (default) splits along the
  window's longer axis, `side` forces a left/right split, `bottom` forces a
  top/bottom split. In tmux, press `s` in the viewer to toggle it live.
```

- [ ] **Step 11: Commit**

```bash
git add gallery_split.go gallery_split_test.go gallery.go README.md
git commit -m "feat(carousel): live 's' toggle for split axis in tmux (#144)"
```

---

## Manual verification (after all tasks)

Rebuild the viewer and launcher into the test bridge, then in a tmux session:

- [ ] **Narrow window auto-opens BOTTOM:** shrink the tmux window so it's taller than ~2× its width, trigger the carousel — it opens as a top/bottom split.
- [ ] **Wide window auto-opens SIDE:** widen it, re-trigger — left/right split.
- [ ] **`s` flips in place:** press `s` — the split flips axis, the viewer keeps running and repaints (no flicker to a blank pane), press `s` again to flip back.
- [ ] **`AEYE_SPLIT=side` / `=bottom`** force the axis regardless of window shape.
- [ ] **Focus is not stolen** on the automatic ensure-open path (`--ensure-open` still uses `-d`).
- [ ] **(macOS)** repeat the auto + `AEYE_SPLIT` checks under tmux; and, if available, confirm iTerm2 opens a `split vertically` (side) when wide and the current `split horizontally` (bottom) when narrow.

## Self-review notes

- **Deviation from spec's testing section:** the spec floated a Go `chooseAxis(cols, rows)` helper "so the heuristic isn't only reachable through bash." The heuristic is only *needed* at launch (bash) — the live toggle deterministically flips, it never re-measures — so a Go `chooseAxis` would be dead code (YAGNI). It's replaced by `flipAxis`, which is actually used by the toggle and unit-tested. The launch heuristic is covered by the bats truth table in Task 1.
- **Spec coverage:** semantic axes (Global Constraints) · auto heuristic + fallback (Task 1) · `AEYE_SPLIT` override (Task 1, exercised per-backend in 2–4) · all four backends (tmux T2, kitty T3, wezterm+iterm T4) · iTerm normalization (T4, `split vertically` added) · live `s` toggle tmux-only (T5) · out-of-tmux dim reads via native APIs (T3 `kitty @ ls`, T4 `wezterm cli list`/AppleScript) · macOS notes (Manual verification + T4).
