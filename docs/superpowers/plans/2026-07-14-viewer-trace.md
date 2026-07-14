# Viewer Trace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add toggleable, shippable `AEYE_DEBUG` tracing to the aeye carousel viewer that can capture an intermittent first-frame glitch (issue #125) in the field.

**Architecture:** A small env-gated file-logging facility (`trace.go`) mirroring the existing `logDropped` precedent, wired into the carousel's `Update`/`View`/`transmitView` at viewer-lifecycle and terminal-boundary points. `AEYE_DEBUG` is forwarded into the viewer by every launcher in `tmux-claude-images.sh`. A tiny `0×0` size guard is added; the experimental `sizeProbe` is not carried forward. No behavioral fix ships yet — the trace is the instrument to find the real cause.

**Tech Stack:** Go 1.26 (charm.land/bubbletea/v2), bash launcher, bats tests.

## Global Constraints

- Best-effort logging: a trace file we can't open/write must never fail the viewer (mirror `logDropped`, `gallery_render.go:217`).
- No new Go module dependencies (keep nix `vendorHash` unchanged).
- Match the `AEYE_*` env family (`AEYE_DIR`/`AEYE_HOST`/`AEYE_OWNER`/`AEYE_BIN`) and the state-dir file convention.
- Zero `[]any` allocation on hot paths when tracing is disabled (guard call sites with `if traceEnabled`).
- pre-commit must pass: gofmt, shellcheck, shfmt, typos. Run inside the worktree devShell (`direnv exec .`).

## File Structure

- Create: `trace.go` — the trace facility (`traceInit`, `tracef`, package state). One responsibility: gated best-effort trace output.
- Create: `trace_test.go` — unit tests for the facility.
- Delete: `gallery_debug.go` — throwaway experimental `dbg()`, superseded.
- Modify: `gallery.go` — `runGallery` calls `traceInit`; trace points in `Update`/`View`/`transmitView`; add `0×0` guard. (Experimental `sizeProbe`/`dbg` scaffolding is discarded first — see Task 1 Step 1.)
- Modify: `scripts/tmux-claude-images.sh` — forward `AEYE_DEBUG` in all five `launch_*` functions.
- Modify: `tests/` bats — assert `AEYE_DEBUG` forwarding via the existing launch seams.
- Modify: `README.md` — document `AEYE_DEBUG`.

---

### Task 1: Trace facility (`trace.go`) + tests

**Files:**
- Reset: `gallery.go` (discard experimental working-tree edits), remove `gallery_debug.go`
- Create: `trace.go`
- Test: `trace_test.go`

**Interfaces:**
- Produces: `traceInit(pane string)` — resolves `AEYE_DEBUG`, opens the per-launch file (truncated), writes a header, sets `traceEnabled`. `tracef(format string, args ...any)` — appends a `[+NNNNms]` line when enabled, else no-op. Package var `traceEnabled bool` for hot-path guards.
- Consumes: `manifestPath(pane string) string` (`gallery_render.go:68`), `version() string` (`main.go`).

- [ ] **Step 1: Reset the experimental scaffolding to a clean base**

```bash
cd /home/noams/Data/git/noamsto/aeye-worktrees/fix-125-carousel-first-frame-settle
git restore gallery.go
gtrash put gallery_debug.go
```
Expected: `git status` shows only the committed spec + (untracked) plan; `gallery.go` matches HEAD.

- [ ] **Step 2: Write the failing tests**

Create `trace_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetTrace() {
	if traceFile != nil {
		traceFile.Close()
	}
	traceEnabled = false
	traceFile = nil
}

func TestTraceDisabledWhenUnset(t *testing.T) {
	t.Setenv("AEYE_DEBUG", "")
	resetTrace()
	traceInit("x")
	if traceEnabled {
		t.Fatal("tracing must be disabled when AEYE_DEBUG is unset")
	}
	tracef("no-op %d", 1) // must not panic or write
}

func TestTraceWritesToExplicitPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.log")
	t.Setenv("AEYE_DEBUG", path)
	resetTrace()
	traceInit("x")
	if !traceEnabled {
		t.Fatal("tracing must be enabled for an explicit path")
	}
	tracef("hello %d", 42)
	traceFile.Sync()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "=== aeye trace") {
		t.Errorf("missing header: %q", s)
	}
	if !strings.Contains(s, "hello 42") {
		t.Errorf("missing line: %q", s)
	}
}

func TestTraceDefaultPathFromPane(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AEYE_DIR", dir)
	t.Setenv("CLAUDE_STATUS_DIR", dir)
	t.Setenv("AEYE_DEBUG", "1")
	// The state dir must exist for O_CREATE to succeed.
	if err := os.MkdirAll(filepath.Dir(manifestPath("2")), 0o755); err != nil {
		t.Fatal(err)
	}
	resetTrace()
	traceInit("2")
	want := strings.TrimSuffix(manifestPath("2"), ".jsonl") + ".trace.log"
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected trace file at %s: %v", want, err)
	}
}

func TestTraceTruncatesOnInit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.log")
	t.Setenv("AEYE_DEBUG", path)
	resetTrace()
	traceInit("x")
	tracef("first-run-line")
	traceFile.Sync()
	resetTrace()
	traceInit("x") // reopening truncates
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "first-run-line") {
		t.Errorf("expected truncation on re-init, got: %q", b)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `direnv exec . go test ./... -run TestTrace -count=1`
Expected: FAIL — `undefined: traceInit`, `undefined: tracef`, `undefined: traceFile`, `undefined: traceEnabled`.

- [ ] **Step 4: Write `trace.go`**

```go
package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Trace facility (issue #125): AEYE_DEBUG toggles a best-effort, per-launch
// trace of the carousel's first-frame lifecycle. Mirrors logDropped: a file we
// can't write is never worth failing the viewer.
var (
	traceEnabled bool
	traceFile    *os.File
	traceStart   time.Time
	traceMu      sync.Mutex
)

// traceInit resolves AEYE_DEBUG and, when set, opens the trace file (truncated
// fresh each launch) and writes a header. Called once from runGallery. pane is
// the manifest key, used for the default per-pane path.
//
//	AEYE_DEBUG=1|true|on -> <state-dir>/<key>.trace.log (beside <key>.jsonl)
//	AEYE_DEBUG=<path>    -> that file
func traceInit(pane string) {
	v := os.Getenv("AEYE_DEBUG")
	if v == "" {
		return
	}
	var path string
	switch v {
	case "1", "true", "on":
		path = strings.TrimSuffix(manifestPath(pane), ".jsonl") + ".trace.log"
	default:
		path = v
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	traceFile = f
	traceStart = time.Now()
	traceEnabled = true
	fmt.Fprintf(f, "=== aeye trace %s pid=%d version=%s ===\n",
		traceStart.Format(time.RFC3339), os.Getpid(), version())
}

// tracef appends a timestamped line when tracing is enabled; a no-op otherwise.
// Guard hot (per-message/per-frame) call sites with `if traceEnabled` so the
// variadic slice isn't allocated when tracing is off.
func tracef(format string, args ...any) {
	if !traceEnabled {
		return
	}
	traceMu.Lock()
	defer traceMu.Unlock()
	ms := time.Since(traceStart).Milliseconds()
	fmt.Fprintf(traceFile, "[+%6dms] "+format+"\n", append([]any{ms}, args...)...)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `direnv exec . go test ./... -run TestTrace -count=1`
Expected: PASS (all four).

- [ ] **Step 6: Commit**

```bash
git add trace.go trace_test.go
git rm --cached --ignore-unmatch gallery_debug.go
git commit -m "feat(viewer): AEYE_DEBUG trace facility (#125)"
```

---

### Task 2: Wire lifecycle trace points + `0×0` guard into `gallery.go`

**Files:**
- Modify: `gallery.go` — `runGallery` (~`:685`), `Update` (`WindowSizeMsg` `:217`, `settleMsg` `:379`, `galleryTickMsg` reload `:366`), top-of-`Update`
- Test: manual (build + real-launch trace); no unit test (bubbletea loop).

**Interfaces:**
- Consumes: `traceInit`, `tracef`, `traceEnabled` (Task 1).

- [ ] **Step 1: Call `traceInit` in `runGallery`**

In `gallery.go`, immediately after `backend, rasterFmt := chooseGridBackend(...)` (~`:689`), before `m := galleryModel{`:

```go
	traceInit(pane)
	tracef("start backend=%v nimg=%d tmux=%v term=%q", backend, len(images), os.Getenv("TMUX") != "", termName())
```

- [ ] **Step 2: Add the top-of-`Update` message trace (excluding floods)**

At the very top of `func (m galleryModel) Update(...)`, before `switch msg := msg.(type) {`:

```go
	if traceEnabled {
		switch msg.(type) {
		case tea.MouseMotionMsg, galleryTickMsg:
			// high-frequency; excluded so the first-frame sequence stays legible
		default:
			tracef("msg %T", msg)
		}
	}
```

- [ ] **Step 3: Add the `0×0` guard + trace in the `WindowSizeMsg` case**

Replace the opening of `case tea.WindowSizeMsg:` (currently `firstReady := !m.ready`) with:

```go
	case tea.WindowSizeMsg:
		if msg.Width == 0 || msg.Height == 0 {
			// A freshly-spawned pane can report 0×0 before it's sized; committing
			// to it clamps computeLayout to 1×1 with no recovery until a manual
			// resize — the #125 symptom. Ignore it; a real size follows.
			tracef("WindowSizeMsg IGNORED w=%d h=%d", msg.Width, msg.Height)
			return m, nil
		}
		firstReady := !m.ready
		m.width, m.height = msg.Width, msg.Height
		m.l = computeLayout(m.width, m.height)
		m.ready = true
		tracef("WindowSizeMsg w=%d h=%d firstReady=%v prevW=%d prevH=%d", msg.Width, msg.Height, firstReady, m.l.previewW, m.l.previewH)
		m.transmitView()
```

(Keep the rest of the case — the `firstReady` batch with `settleCmd()` — unchanged.)

- [ ] **Step 4: Trace `settleMsg` and reload**

At the top of `case settleMsg:` add:

```go
		tracef("settleMsg re-store")
```

In `case galleryTickMsg:`, inside the `if mt := manifestMtime(m.pane); mt != m.mtime {` block, before `m.reload()`:

```go
			tracef("reload manifest mtime changed")
```

And in the same case, inside the theme-switch `if th := detectTheme(); th != m.theme {` block:

```go
			tracef("theme switch %s -> %s", m.theme, th)
```

- [ ] **Step 5: Build and vet**

Run: `direnv exec . go build ./... && direnv exec . go vet ./...`
Expected: no errors. (gopls "undefined"/"not in workspace" diagnostics in the wt worktree are false positives — trust `go build`.)

- [ ] **Step 6: Commit**

```bash
git add gallery.go
git commit -m "feat(viewer): trace lifecycle + guard 0x0 size (#125)"
```

---

### Task 3: Terminal-boundary signals (`transmitView` write result + `View` frame emits)

**Files:**
- Modify: `gallery.go` — `transmitView` (`:173`), `View` (`:524`)
- Test: manual (real-launch trace shows store `(n,err)` + frame ordering).

**Interfaces:**
- Consumes: `traceEnabled`, `tracef`, `traceStart` (Task 1).

- [ ] **Step 1: Capture the preview store write result in `transmitView`**

Replace the preview-store line (currently `fmt.Fprint(m.tty, transmitVirtual(previewID, src, m.l.previewW, m.l.previewH))`) with:

```go
	apc := transmitVirtual(previewID, src, m.l.previewW, m.l.previewH)
	n, err := fmt.Fprint(m.tty, apc)
	if traceEnabled {
		tracef("store preview id=%d bytes=%d n=%d err=%v cur=%d %dx%d nimg=%d",
			previewID, len(apc), n, err, m.cursor, m.l.previewW, m.l.previewH, len(m.images))
	}
```

A short write (`n < len(apc)`) or non-nil `err` explains a blank preview directly — no timing race needed.

- [ ] **Step 2: Trace `View` frame emits (first second only)**

In `func (m galleryModel) View() tea.View`, after `content` is computed and before `v := tea.NewView(content)`:

```go
	if traceEnabled && time.Since(traceStart) < time.Second {
		tracef("View emit ready=%v len=%d", m.ready, len(content))
	}
```

The `[+NNNNms]` ordering of a completed `store preview` line vs a `View emit` line is the in-process proxy for the store-vs-paint race.

- [ ] **Step 3: Build and vet**

Run: `direnv exec . go build ./... && direnv exec . go vet ./...`
Expected: no errors.

- [ ] **Step 4: Real-launch validation**

Build the trace binary and drive the real launcher (env forwarding lands in Task 4; here validate the viewer directly):

```bash
direnv exec . go build -o /tmp/aeye-trace .
tmux set-environment -g AEYE_BIN /tmp/aeye-trace
tmux set-environment -g AEYE_DEBUG 1
```
Open the carousel (keybinding). Then read `<state-dir>/<key>.trace.log`.
Expected: header line; `start backend=…`; a `WindowSizeMsg w=… h=…` (non-zero) or `WindowSizeMsg IGNORED`; `store preview … n=… err=<nil>`; `View emit …` lines; ordering visible.

- [ ] **Step 5: Commit**

```bash
git add gallery.go
git commit -m "feat(viewer): trace store write result + frame emits (#125)"
```

---

### Task 4: Forward `AEYE_DEBUG` through all launchers

**Files:**
- Modify: `scripts/tmux-claude-images.sh` — `launch_tmux` (`:91-98`), `launch_kitty` (`:155`, `:170-185`), `launch_wezterm` (`:207-208`), `launch_ghostty` (`:230`), `launch_iterm` (`:305`)
- Test: `tests/` bats seam

**Interfaces:**
- Produces: each spawned viewer command carries `AEYE_DEBUG=<val>` when it is set in the launcher's env; absent when unset.

- [ ] **Step 1: `launch_tmux` — add to the `env` prefix**

Replace the `cmd` construction (`:92-96`) with a debug-aware prefix:

```bash
	local dbg=""
	[[ -n ${AEYE_DEBUG:-} ]] && printf -v dbg 'AEYE_DEBUG=%q ' "$AEYE_DEBUG"
	local cmd
	if [[ -n ${CLAUDE_CODE_SESSION_ID:-} ]]; then
		printf -v cmd 'env %sAEYE_OWNER=%q %q %q' "$dbg" "$CLAUDE_CODE_SESSION_ID" "$VIEWER_BIN" "$KEY"
	else
		printf -v cmd 'env %s%q %q' "$dbg" "$VIEWER_BIN" "$KEY"
	fi
```

- [ ] **Step 2: `launch_kitty` — add a `--env` entry (both launch sites)**

After the `owner_env` line (`:156`), add:

```bash
	local debug_env=()
	[[ -n ${AEYE_DEBUG:-} ]] && debug_env=(--env AEYE_DEBUG="$AEYE_DEBUG")
```

Then in both `kitty @ launch` invocations (the stash path `:170-175` and the normal path `:181-185`), add after the `${owner_env[@]+...}` line:

```bash
			${debug_env[@]+"${debug_env[@]}"} \
```

- [ ] **Step 3: `launch_wezterm` — add to the `env` prefix**

Replace the split-pane command (`:207-208`) with:

```bash
	local dbg=()
	[[ -n ${AEYE_DEBUG:-} ]] && dbg=(AEYE_DEBUG="$AEYE_DEBUG")
	pane="$(wezterm cli split-pane --right --percent 40 --cwd "$STATE_DIR" -- \
		env "${dbg[@]}" AEYE_DIR="$STATE_DIR" CLAUDE_STATUS_DIR="$STATE_DIR" "$VIEWER_BIN" "$KEY")"
```

(`"${dbg[@]}"` on an empty array expands to nothing under `set -u` because it is directly quoted with elements only when set — verify with shellcheck; if flagged, use `${dbg[@]+"${dbg[@]}"}`.)

- [ ] **Step 4: `launch_ghostty` — add to the `cmd` array**

Replace the `cmd` array (`:230`) with:

```bash
	local dbg=()
	[[ -n ${AEYE_DEBUG:-} ]] && dbg=(AEYE_DEBUG="$AEYE_DEBUG")
	local cmd=(env ${dbg[@]+"${dbg[@]}"} AEYE_DIR="$STATE_DIR" CLAUDE_STATUS_DIR="$STATE_DIR" "$VIEWER_BIN" "$KEY")
```

- [ ] **Step 5: `launch_iterm` — add to the quoted `cmd` string**

Replace the `cmd=` assignment (`:305`) with:

```bash
	local dbg=""
	[[ -n ${AEYE_DEBUG:-} ]] && printf -v dbg 'AEYE_DEBUG=%q ' "$AEYE_DEBUG"
	local cmd
	cmd="env ${dbg}AEYE_DIR=$(printf '%q' "$STATE_DIR") CLAUDE_STATUS_DIR=$(printf '%q' "$STATE_DIR") $(printf '%q' "$VIEWER_BIN") $(printf '%q' "$KEY")"
```

- [ ] **Step 6: Add a bats assertion**

Find the existing launch seam test (`tests/toggle.bats` or `tests/launch-hidden.bats`) that inspects the built command / mocks `tmux`. Add a case: with `AEYE_DEBUG=1` exported, the captured viewer command contains `AEYE_DEBUG=1`; without it, it does not. Mirror the existing `AEYE_OWNER` assertion style in that file.

- [ ] **Step 7: Lint + test**

Run: `direnv exec . shellcheck scripts/tmux-claude-images.sh && direnv exec . shfmt -d scripts/tmux-claude-images.sh`
Run: `direnv exec . bats tests/`
Expected: shellcheck clean, shfmt no diff, bats green.

- [ ] **Step 8: Commit**

```bash
git add scripts/tmux-claude-images.sh tests/
git commit -m "feat(launcher): forward AEYE_DEBUG into the viewer on all hosts (#125)"
```

---

### Task 5: Document `AEYE_DEBUG` + delivery

**Files:**
- Modify: `README.md`
- No code.

- [ ] **Step 1: Document the env var**

Add a short "Debugging" subsection to `README.md` near the existing `AEYE_*` env documentation:

```markdown
### Debugging the carousel

Set `AEYE_DEBUG` to trace the viewer's first-frame lifecycle (issue #125-class
glitches):

- `AEYE_DEBUG=1` → writes `<state-dir>/<key>.trace.log` (beside the manifest),
  fresh on each open.
- `AEYE_DEBUG=/path/to/file` → writes there.

To leave it on, set it where the *launcher* runs:
- tmux keybinding: `tmux set-environment -g AEYE_DEBUG 1`
- `/aeye` skill / auto-open hook: export it in your shell profile.

The launcher forwards it into the spawned viewer on every host.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: document AEYE_DEBUG viewer trace (#125)"
```

- [ ] **Step 3: Delivery**

- Push the branch, open a PR (`feat(viewer): toggleable AEYE_DEBUG trace (#125)`), assign `@me`.
- Interim (before nix bump lands): `AEYE_BIN`/`AEYE_DEBUG` are already set in the tmux global env pointing at a stable-path build (`/tmp/aeye-trace` from Task 3, or a longer-lived path). Confirm the real installed carousel picks up the trace.
- After merge: refresh the nix package so the installed `aeye` carries the trace; then unset the temporary `AEYE_BIN` override (`tmux set-environment -gu AEYE_BIN`).

---

## Self-Review

**Spec coverage:** facility (Task 1) ✓; env gating + default/custom path + truncate (Task 1 tests) ✓; lifecycle trace points + `0×0` guard + `sizeProbe` revert (Task 1 Step 1 discards it; Task 2) ✓; terminal-boundary signals (Task 3) ✓; launcher forwarding all hosts + where-to-set table (Task 4 + Task 5 doc) ✓; tests (Task 1, Task 4) ✓; delivery (Task 5) ✓.

**Placeholder scan:** none — all steps carry concrete code/commands.

**Type consistency:** `traceInit`/`tracef`/`traceEnabled`/`traceFile`/`traceStart` used consistently across Tasks 1–3; `manifestPath`/`version` are existing symbols; the `<key>.trace.log` path is derived identically in `traceInit` and `TestTraceDefaultPathFromPane`.
