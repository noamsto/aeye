# OSC 1337 raster backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render crisp images on iTerm2 (and higher-fidelity on wezterm) via the OSC 1337 inline-image protocol, reusing the existing `backendRaster` lifecycle.

**Architecture:** Two layers. **Render** (Tasks 1–3): keep the single `backendRaster` enum; add a `rasterFormat` field ("sixels" | "iterm") that selects the chafa `-f` flag. `chooseGridBackend` returns `(backend, format)` and gains non-tmux branches for iTerm2/wezterm. The entire out-of-band paint lifecycle (blank holes, debounced repaint, cursor-positioned paint, ClearScreen teardown) is unchanged. **Launch** (Task 4): add a `MODE=iterm` standalone mode to `scripts/tmux-claude-images.sh` driven by AppleScript (`osascript`), so iTerm2 is actually launchable — otherwise its render path is dead code.

**Tech Stack:** Go, charm.land/bubbletea v2, chafa (external, emits both sixel and OSC 1337), bash + AppleScript (`osascript`) for the macOS launch mode, bats for the launcher tests.

**Spec:** `docs/superpowers/specs/2026-06-18-osc1337-iterm-backend-design.md`

---

### Task 1: Backend selection returns a raster format

**Files:**
- Modify: `gallery_render.go:171-198` (add format constants, rewrite `chooseGridBackend`)
- Modify: `gallery.go:83-104` (add `rasterFormat` field), `gallery.go:561-568` (call site)
- Test: `gallery_test.go:44-79` (rewrite `TestChooseGridBackend`)

- [ ] **Step 1: Rewrite the failing test**

Replace `TestChooseGridBackend` (`gallery_test.go:44-79`) with this version (new `termProgram`/`lcTerminal` inputs, `wantFmt` output, new cases):

```go
func TestChooseGridBackend(t *testing.T) {
	yes := func() bool { return true }
	no := func() bool { return false }
	cases := []struct {
		name        string
		term        string
		inTmux      bool
		termProgram string
		lcTerminal  string
		weztermPane string
		envTerm     string
		probe       func() bool
		want        gridBackend
		wantFmt     string
	}{
		{"kitty termname", "xterm-kitty", false, "", "", "", "xterm-kitty", nil, backendKitty, ""},
		{"ghostty termname", "xterm-ghostty", false, "", "", "", "xterm-ghostty", nil, backendKitty, ""},
		{"kitty suffix", "xterm-kitty-direct", false, "", "", "", "foot", nil, backendKitty, ""},
		{"iterm by TERM_PROGRAM", "xterm-256color", false, "iTerm.app", "", "", "xterm-256color", nil, backendRaster, formatITerm},
		{"iterm by LC_TERMINAL", "xterm-256color", false, "", "iTerm2", "", "xterm-256color", nil, backendRaster, formatITerm},
		{"wezterm by TERM_PROGRAM", "xterm-256color", false, "WezTerm", "", "", "xterm-256color", nil, backendRaster, formatITerm},
		{"wezterm by WEZTERM_PANE", "xterm-256color", false, "", "", "1", "xterm-256color", nil, backendRaster, formatITerm},
		{"foot standalone", "foot", false, "", "", "", "foot", nil, backendRaster, formatSixel},
		{"in tmux, probe yes", "tmux-256color", true, "tmux", "", "", "tmux-256color", yes, backendRaster, formatSixel},
		{"in tmux, probe no", "tmux-256color", true, "tmux", "", "", "tmux-256color", no, backendSymbols, ""},
		{"leaked weztermpane in tmux still probes", "tmux-256color", true, "", "", "1", "tmux-256color", no, backendSymbols, ""},
		{"leaked iterm env in tmux still probes", "tmux-256color", true, "iTerm.app", "iTerm2", "", "tmux-256color", yes, backendRaster, formatSixel},
		{"unknown standalone, probe yes", "xterm-256color", false, "", "", "", "xterm-256color", yes, backendRaster, formatSixel},
		{"unknown standalone, probe no", "xterm-256color", false, "", "", "", "xterm-256color", no, backendSymbols, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			probe := c.probe
			if probe == nil {
				probe = func() bool { t.Fatal("probe must not run on a fast-path"); return false }
			}
			got, fmt := chooseGridBackend(c.term, c.inTmux, c.termProgram, c.lcTerminal, c.weztermPane, c.envTerm, probe)
			if got != c.want || fmt != c.wantFmt {
				t.Errorf("chooseGridBackend = (%v, %q), want (%v, %q)", got, fmt, c.want, c.wantFmt)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestChooseGridBackend -count=1`
Expected: FAIL — compile error, `chooseGridBackend` returns 1 value / takes 5 args, and `formatITerm`/`formatSixel` undefined.

- [ ] **Step 3: Add format constants and rewrite `chooseGridBackend`**

In `gallery_render.go`, after the backend enum block (`gallery_render.go:171-177`), add:

```go
// chafa output formats for backendRaster. iterm = OSC 1337 inline image (true
// color, iTerm2/wezterm); sixels = palette-indexed sixel (foot, tmux passthrough).
const (
	formatSixel = "sixels"
	formatITerm = "iterm"
)
```

Replace `chooseGridBackend` (`gallery_render.go:187-198`) with:

```go
func chooseGridBackend(termname string, inTmux bool, termProgram, lcTerminal, weztermPane, term string, probeSixel func() bool) (gridBackend, string) {
	if strings.HasPrefix(termname, "xterm-kitty") || strings.HasPrefix(termname, "xterm-ghostty") {
		return backendKitty, ""
	}
	// OSC 1337 / sixel hosts are only identifiable standalone: inside tmux,
	// TERM_PROGRAM becomes "tmux" and WEZTERM_PANE isn't forwarded, so fall to the
	// DA1 probe (which reflects tmux's own sixel capability).
	if !inTmux {
		if termProgram == "iTerm.app" || lcTerminal == "iTerm2" {
			return backendRaster, formatITerm
		}
		if termProgram == "WezTerm" || weztermPane != "" {
			return backendRaster, formatITerm
		}
		if strings.HasPrefix(term, "foot") {
			return backendRaster, formatSixel
		}
	}
	if probeSixel() {
		return backendRaster, formatSixel
	}
	return backendSymbols, ""
}
```

Also update the doc comment above it (`gallery_render.go:179-186`) so it no longer claims "a standalone host identified by env gets sixel" — it now gets OSC 1337 for iTerm2/wezterm, sixel for foot. Suggested replacement comment:

```go
// chooseGridBackend picks the grid renderer and, for backendRaster, the chafa
// format. kitty needs unicode PLACEHOLDERS (not just kitty graphics), which has no
// capability query and which wezterm lacks despite speaking graphics — so kitty
// stays a termname allowlist, never a probe. Standalone hosts identified by env get
// a real-pixel raster: OSC 1337 for iTerm2/wezterm (true color), sixel for foot.
// Everything else (notably in-tmux, where the only reliable signal is whether THIS
// tmux passes sixel) falls to the DA1 probe. probeSixel is injected so this stays a
// pure, testable function and is invoked lazily — only on the probe branch.
```

- [ ] **Step 4: Add the model field and update the call site**

In `gallery.go`, add to the `galleryModel` struct after `backend gridBackend` (`gallery.go:86`):

```go
	rasterFormat string // chafa -f value when backend == backendRaster (formatSixel/formatITerm)
```

Replace the call site (`gallery.go:561-568`) so it destructures both returns. Change:

```go
	m := galleryModel{
		pane:    pane,
		images:  images,
		backend: chooseGridBackend(termName(), os.Getenv("TMUX") != "", os.Getenv("WEZTERM_PANE"), os.Getenv("TERM"), probeSixel),
		theme:   theme,
		tty:     tty,
		mtime:   manifestMtime(pane),
		cursor:  max(0, len(images)-1),
```

to:

```go
	backend, rasterFmt := chooseGridBackend(termName(), os.Getenv("TMUX") != "", os.Getenv("TERM_PROGRAM"), os.Getenv("LC_TERMINAL"), os.Getenv("WEZTERM_PANE"), os.Getenv("TERM"), probeSixel)
	m := galleryModel{
		pane:         pane,
		images:       images,
		backend:      backend,
		rasterFormat: rasterFmt,
		theme:        theme,
		tty:          tty,
		mtime:        manifestMtime(pane),
		cursor:       max(0, len(images)-1),
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test . -run TestChooseGridBackend -count=1`
Expected: PASS

- [ ] **Step 6: Verify the whole package still builds and tests pass**

Run: `go build ./... && go test . -count=1`
Expected: PASS (`rasterFormat` is set but not yet read — Go allows unused struct fields).

- [ ] **Step 7: Commit**

```bash
git add gallery_render.go gallery.go gallery_test.go
git commit -m "feat: select OSC 1337 raster format for iTerm2/wezterm (#60)"
```

---

### Task 2: Render the selected format via chafa

**Files:**
- Modify: `gallery_raster.go:106-120` (`renderSixel` → `rasterArgs` + `renderRaster`), `gallery_raster.go:146,156` (callers)
- Test: `gallery_test.go` (add `TestRasterArgs` after `TestSymbolsArgs`)

- [ ] **Step 1: Write the failing test**

Add after `TestSymbolsArgs` (`gallery_test.go:115`):

```go
func TestRasterArgs(t *testing.T) {
	cases := []struct {
		format string
		want   []string
	}{
		{formatITerm, []string{"-f", "iterm", "--size", "20x10", "/a/b.png"}},
		{formatSixel, []string{"-f", "sixels", "--size", "20x10", "/a/b.png"}},
	}
	for _, c := range cases {
		got := rasterArgs(c.format, "/a/b.png", 20, 10)
		if len(got) != len(c.want) {
			t.Fatalf("%s: len = %d, want %d: %v", c.format, len(got), len(c.want), got)
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("%s: arg %d = %q, want %q", c.format, i, got[i], c.want[i])
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestRasterArgs -count=1`
Expected: FAIL — compile error, `rasterArgs` undefined.

- [ ] **Step 3: Add `rasterArgs`, refactor `renderSixel` → `renderRaster`**

In `gallery_raster.go`, replace `renderSixel` and its doc comment (`gallery_raster.go:106-120`) with:

```go
// rasterArgs builds the chafa command for a format ("iterm" = OSC 1337, "sixels" =
// sixel) sized to cols×rows cells. Factored out for testing the format selection
// without executing chafa (mirrors symbolsArgs).
func rasterArgs(format, pngPath string, cols, rows int) []string {
	return []string{"-f", format, "--size", fmt.Sprintf("%dx%d", cols, rows), pngPath}
}

// renderRaster rasterizes a PNG to a real-pixel payload (sixel or OSC 1337, per
// format) sized to cols×rows cells via chafa, or "" on failure. chafa sizes both
// formats by --size in cells, so the image fills the cell box without tuning. chafa
// wraps output in cursor hide/show (ESC [?25l … ESC [?25h); strip them so they don't
// fight the TUI's cursor management (same as symbolsBlock).
func renderRaster(format, pngPath string, cols, rows int) string {
	out, err := exec.Command("chafa", rasterArgs(format, pngPath, cols, rows)...).Output()
	if err != nil {
		return ""
	}
	s := string(out)
	s = strings.ReplaceAll(s, "\x1b[?25l", "")
	s = strings.ReplaceAll(s, "\x1b[?25h", "")
	return strings.TrimRight(s, "\n")
}
```

Update the two callers to pass the model's format. In `paintPreview` (`gallery_raster.go:146`):

```go
	paintSixelAt(m.tty, r, renderRaster(m.rasterFormat, src, r.w, r.h))
```

In `paintStrip` (`gallery_raster.go:156`):

```go
		paintSixelAt(m.tty, inner, renderRaster(m.rasterFormat, png, inner.w, inner.h))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test . -run TestRasterArgs -count=1`
Expected: PASS

- [ ] **Step 5: Verify the whole package builds and tests pass**

Run: `go build ./... && go test . -count=1`
Expected: PASS — no remaining references to `renderSixel` (it had only the two callers just updated).

- [ ] **Step 6: Commit**

```bash
git add gallery_raster.go gallery_test.go
git commit -m "feat: render OSC 1337 or sixel from the chosen raster format (#60)"
```

---

### Task 3 (optional): Chafa execution smoke test

Nice-to-have — `TestRasterArgs` already pins the format. This asserts chafa actually emits OSC 1337, skipping when chafa is absent.

**Files:**
- Test: `gallery_raster_test.go` (new)

- [ ] **Step 1: Write the test**

Create `gallery_raster_test.go`:

```go
package main

import (
	"os/exec"
	"strings"
	"testing"
)

// renderRaster with formatITerm must emit an OSC 1337 inline-image payload with the
// cursor-toggle wrappers stripped. Skipped when chafa is unavailable.
func TestRenderRasterITerm(t *testing.T) {
	if _, err := exec.LookPath("chafa"); err != nil {
		t.Skip("chafa not on PATH")
	}
	src := filepath.Join(t.TempDir(), "shot.png")
	writeTestImage(t, src, 32, 32)

	got := renderRaster(formatITerm, src, 8, 4)
	if !strings.HasPrefix(got, "\x1b]1337;File=") {
		t.Errorf("iterm payload does not start with OSC 1337 marker: %q", got[:min(40, len(got))])
	}
	if strings.Contains(got, "\x1b[?25l") || strings.Contains(got, "\x1b[?25h") {
		t.Errorf("cursor wrappers not stripped: %q", got)
	}
}
```

Note: `writeTestImage` is the existing test helper used by `gallery_cache_test.go:75`; `min` is the Go builtin. Add `"path/filepath"` to the import block.

- [ ] **Step 2: Run the test**

Run: `go test . -run TestRenderRasterITerm -count=1 -v`
Expected: PASS (or SKIP if chafa absent).

- [ ] **Step 3: Commit**

```bash
git add gallery_raster_test.go
git commit -m "test: smoke-test chafa OSC 1337 output (#60)"
```

---

### Task 4: iTerm2 launch mode (script + bats)

Adds `MODE=iterm` to the launcher so standalone iTerm2 actually opens the viewer. iTerm2's only stable IPC is AppleScript, so spawn/toggle go through `osascript`: split the current session running the viewer, persist the new session's unique id, close by id on toggle. macOS-only at runtime; tested here with a stubbed `osascript`.

**Files:**
- Modify: `scripts/tmux-claude-images.sh` (`resolve_target`, new `launch_iterm` + 3 `osascript` helpers, `main` guard + message, header doc comment)
- Test: `tests/toggle-window.bats` (`setup`: unset `TERM_PROGRAM`, add `osascript` stub; new iTerm2 `@test` group)

- [ ] **Step 1: Extend the bats `setup()` with an `osascript` stub**

In `tests/toggle-window.bats`, add `TERM_PROGRAM` to the `unset` line (line 8) so a stray host env can't leak in:

```bash
	unset TMUX KITTY_LISTEN_ON WEZTERM_PANE GHOSTTY_RESOURCES_DIR TERM TERM_PROGRAM
```

Then, just before the final `export PATH="$STUB_BIN:$PATH"` (line 50), add the stub. It logs args, echoes a fake session id for a split, no-ops a close, and reports liveness from `$STUB_ITERM_ALIVE` (the split/close/alive scripts are distinguishable by the literal AppleScript text they contain):

```bash
	# osascript stub: logs args; a split echoes a session id; a close is logged
	# only; anything else is the liveness query → echoes 1 when $STUB_ITERM_ALIVE.
	export OSASCRIPT_LOG="$BATS_TEST_TMPDIR/osascript.log"
	: >"$OSASCRIPT_LOG"
	cat >"$STUB_BIN/osascript" <<'STUB'
#!/usr/bin/env bash
echo "$*" >>"$OSASCRIPT_LOG"
args="$*"
if [[ $args == *"split horizontally"* ]]; then
	echo "${STUB_ITERM_SESSION:-iterm-sess-1}"
elif [[ $args == *"to close"* ]]; then
	:
else
	[[ -n ${STUB_ITERM_ALIVE:-} ]] && echo 1 || echo 0
fi
STUB
	chmod +x "$STUB_BIN/osascript"
```

- [ ] **Step 2: Write the failing iTerm2 bats tests**

Append this `@test` group to the end of `tests/toggle-window.bats`:

```bash
@test "resolve: iterm when TERM_PROGRAM=iTerm.app" {
	export TERM_PROGRAM=iTerm.app
	run bash "$APP" --resolve
	[ "$status" -eq 0 ]
	[ "$(echo "$output" | cut -f1)" = iterm ]
	[ "$(echo "$output" | cut -f3)" = "$CLAUDE_STATUS_DIR/images/sess-123.jsonl" ]
}

@test "iterm: split opens a viewer and records the session id" {
	export TERM_PROGRAM=iTerm.app
	unset STUB_ITERM_ALIVE
	export STUB_ITERM_SESSION=ABC-123
	run bash "$APP"
	[ "$status" -eq 0 ]
	grep -q "split horizontally" "$OSASCRIPT_LOG"
	[ "$(cat "$CLAUDE_STATUS_DIR/images/$CLAUDE_CODE_SESSION_ID.iterm-session")" = ABC-123 ]
}

@test "iterm: bare toggle closes the live session" {
	export TERM_PROGRAM=iTerm.app
	printf 'ABC-123\n' >"$CLAUDE_STATUS_DIR/images/$CLAUDE_CODE_SESSION_ID.iterm-session"
	export STUB_ITERM_ALIVE=1
	run bash "$APP"
	[ "$status" -eq 0 ]
	grep -q "to close" "$OSASCRIPT_LOG"
	[ ! -f "$CLAUDE_STATUS_DIR/images/$CLAUDE_CODE_SESSION_ID.iterm-session" ]
}

@test "iterm: --ensure-open with a live session does not split again" {
	export TERM_PROGRAM=iTerm.app
	printf 'ABC-123\n' >"$CLAUDE_STATUS_DIR/images/$CLAUDE_CODE_SESSION_ID.iterm-session"
	export STUB_ITERM_ALIVE=1
	run bash "$APP" --ensure-open
	[ "$status" -eq 0 ]
	run grep -c "split horizontally" "$OSASCRIPT_LOG"
	[ "$output" = 0 ]
}
```

- [ ] **Step 3: Run the new tests to verify they fail**

Run: `bats tests/toggle-window.bats`
Expected: the four `iterm:` / `resolve: iterm` tests FAIL — `--resolve` prints `none` (no iterm branch yet) and a bare run hits the `MODE=none` message, so no `osascript` is logged and no `.iterm-session` file is written.

- [ ] **Step 4: Add the `resolve_target` iTerm2 branch**

In `scripts/tmux-claude-images.sh`, add a doc line to the header block (after the `MODE=ghostty` line, ~line 23):

```sh
#   MODE=iterm   + KEY=<cc session id>  outside tmux, in iTerm2 (macOS, AppleScript)
```

Then insert the branch in `resolve_target`, between the ghostty `elif` and the final `else` (~line 49):

```sh
	elif [[ ${TERM_PROGRAM:-} == iTerm.app ]]; then
		MODE=iterm
		KEY="${CLAUDE_CODE_SESSION_ID:-}"
		MANIFEST="$IMAGES_DIR/$KEY.jsonl"
```

- [ ] **Step 5: Add `launch_iterm` and its `osascript` helpers**

In `scripts/tmux-claude-images.sh`, immediately after `launch_ghostty()` (~line 153), add the helpers and launcher. Each helper builds its script from repeated `-e` lines (no HEREDOC, keeping shellcheck quiet) and passes the argument via `-- "$1"` to `on run argv`, so shell values never enter the AppleScript text:

```sh
# iTerm2's only stable IPC is AppleScript. We split the current session to run the
# viewer, persist the new session's unique id (keyed by cc session id), and close by
# id on toggle. pgrep-on-viewer (ghostty's trick) can kill the viewer but not reliably
# close the split pane — that depends on the profile's "When session ends" setting.
iterm_split() {
	osascript \
		-e 'on run argv' \
		-e 'tell application "iTerm2"' \
		-e 'tell current session of current window' \
		-e 'set s to (split horizontally with default profile command (item 1 of argv))' \
		-e 'end tell' \
		-e 'return id of s' \
		-e 'end tell' \
		-e 'end run' \
		-- "$1"
}

iterm_alive() {
	local r
	r="$(osascript \
		-e 'on run argv' \
		-e 'set targetId to item 1 of argv' \
		-e 'tell application "iTerm2"' \
		-e 'repeat with w in windows' \
		-e 'repeat with t in tabs of w' \
		-e 'repeat with s in sessions of t' \
		-e 'if (id of s) is targetId then return "1"' \
		-e 'end repeat' \
		-e 'end repeat' \
		-e 'end repeat' \
		-e 'end tell' \
		-e 'return "0"' \
		-e 'end run' \
		-- "$1" 2>/dev/null)"
	[[ $r == 1 ]]
}

iterm_close() {
	osascript \
		-e 'on run argv' \
		-e 'set targetId to item 1 of argv' \
		-e 'tell application "iTerm2"' \
		-e 'repeat with w in windows' \
		-e 'repeat with t in tabs of w' \
		-e 'repeat with s in sessions of t' \
		-e 'if (id of s) is targetId then tell s to close' \
		-e 'end repeat' \
		-e 'end repeat' \
		-e 'end repeat' \
		-e 'end tell' \
		-e 'end run' \
		-- "$1" >/dev/null 2>&1 || true
}

launch_iterm() {
	local idfile="$IMAGES_DIR/$KEY.iterm-session" session=""
	[[ -f $idfile ]] && session="$(<"$idfile")"
	if [[ -n $session ]] && iterm_alive "$session"; then
		[[ -n $ENSURE_OPEN ]] && return # already open; ensure-open is a no-op
		iterm_close "$session"
		rm -f "$idfile"
		return
	fi
	# command runs under iTerm2's app environment (never saw our env), so forward the
	# state dir explicitly — same as the wezterm/ghostty paths.
	session="$(iterm_split "env AEYE_DIR=$STATE_DIR CLAUDE_STATUS_DIR=$STATE_DIR $VIEWER_BIN $KEY")"
	printf '%s\n' "$session" >"$idfile"
}
```

- [ ] **Step 6: Add `iterm` to the `main` guard and the no-host message**

In `scripts/tmux-claude-images.sh` `main()` (~lines 164–167), extend the `MODE=none` message and the session-id guard group:

```sh
	none)
		echo "image carousel needs tmux, kitty remote control, wezterm, ghostty, or iTerm2" >&2
		exit 0
		;;
	kitty | wezterm | ghostty | iterm)
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `bats tests/toggle-window.bats`
Expected: PASS — all four new tests plus the existing wezterm/ghostty/none tests.

- [ ] **Step 8: Lint the script**

Run: `shellcheck scripts/tmux-claude-images.sh`
Expected: no output (clean). Fix any warning before committing.

- [ ] **Step 9: Commit**

```bash
git add scripts/tmux-claude-images.sh tests/toggle-window.bats
git commit -m "feat: launch the carousel in standalone iTerm2 (#60)"
```

---

### Task 5: Verify, document, and open the PR

**Files:**
- Modify: spec/issue references as needed (no code).

- [ ] **Step 1: Full check via the dev shell**

Run: `direnv exec . go test ./... -count=1`
Expected: PASS. Also run `direnv exec . gofmt -l .` (expect no output), `direnv exec . bats tests/` (expect all bats green), and `shellcheck scripts/tmux-claude-images.sh` (expect clean).

- [ ] **Step 2: Live wezterm verification (manual)**

In the `.#verify` devShell (wezterm, added in #59), launch the gallery on a manifest
with at least one photo and one diagram. Confirm: preview + filmstrip render crisp
(true color, no sixel banding), arrow-key navigation repaints correctly, terminal
resize reflows, and quitting leaves no image residue.

- [ ] **Step 3: Manual iTerm2 pass (best-effort, on a Mac)**

There is no macOS in CI or on the dev host, so the launch mode + OSC 1337 render path
are unverified by automation. If a Mac is available: in standalone iTerm2 (no tmux),
trigger the toggle and confirm a split opens running the viewer, images render crisp
(OSC 1337), and toggling again closes the split. If no Mac is available, state plainly
in the PR that iTerm2 is covered only by the stub/unit tests.

- [ ] **Step 4: Push and open the PR**

```bash
git push -u origin feat/60-osc1337-iterm
gh pr create --assignee @me --title "feat: OSC 1337 rendering + iTerm2 launch mode (#60)" --body "Closes #60.

**Render:** adds an OSC 1337 (\`chafa -f iterm\`) path to backendRaster. iTerm2 and standalone wezterm now render crisp instead of block-art (iTerm2) / quantized sixel (wezterm, now true-color). foot and in-tmux keep sixel.

**Launch:** adds a \`MODE=iterm\` standalone mode to the toggle script (AppleScript via osascript: split current session, persist session id, close by id), so iTerm2 is actually launchable.

**Verification:** Go unit tests pin backend/format selection; bats (stubbed osascript) cover resolve + spawn/toggle/ensure-open; wezterm verified live via .#verify. iTerm2 itself is macOS-only — no macOS in CI or on the dev host, so its AppleScript launch + OSC 1337 render are covered only by stub/unit tests (+ a manual Mac pass if available)."
```

---

## Self-Review

**Spec coverage:**
- Selection table (iTerm2/wezterm→iterm, foot→sixels, probe→sixels, `!inTmux` gate) → Task 1 ✓
- Named format constants → Task 1 Step 3 ✓
- `renderRaster` + `rasterArgs` seam → Task 2 ✓
- `TestChooseGridBackend` matrix → Task 1 ✓; `TestRasterArgs` → Task 2 ✓; optional chafa smoke test → Task 3 ✓
- Teardown unchanged (no `deleteAll` for raster) → no task needed, confirmed in spec ✓
- Launch layer: `resolve_target` iTerm2 branch (`TERM_PROGRAM=iTerm.app`), `launch_iterm` + `iterm_split`/`iterm_alive`/`iterm_close`, `main` guard + message → Task 4 ✓
- Launch tests: resolve seam, spawn (records id), toggle-off (closes + removes id file), ensure-open no-op → Task 4 Steps 1–2 ✓
- `shellcheck` clean → Task 4 Step 8, Task 5 Step 1 ✓
- Live wezterm verification → Task 5 ✓; best-effort manual iTerm2 pass → Task 5 ✓
- No per-image fallback / param footgun → documented in spec; no code action ✓

**Placeholder scan:** none — every code step shows complete code.

**Type consistency:**
- Render: `chooseGridBackend` returns `(gridBackend, string)` consistently across Task 1 signature, test, and call site. `formatSixel`/`formatITerm` defined in Task 1 Step 3, used in Tasks 1–3. `rasterFormat` field defined Task 1 Step 4, read in Task 2 Step 3. `renderRaster(format, pngPath, cols, rows)` signature matches between definition (Task 2), callers (Task 2), and smoke test (Task 3).
- Launch: `MODE=iterm` set in `resolve_target` (Task 4 Step 4) dispatches to `launch_iterm` (Task 4 Step 5) via the existing `launch_$MODE`; guard group lists `iterm` (Step 6). State file `$KEY.iterm-session` written by `launch_iterm` and asserted by the bats tests (Step 2). The `osascript` stub keys on the literal strings `split horizontally` / `to close` that `iterm_split` / `iterm_close` actually emit (Steps 1, 5) — stub and script agree.
