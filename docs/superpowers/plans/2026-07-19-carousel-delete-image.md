# Carousel Delete Image (x + undo) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user remove an unwanted carousel image with `x`, marked with a danger border + depleting countdown, recoverable with `u` for a 5-second window before the file is deleted.

**Architecture:** `x` marks the selected `imageEntry` as a single in-flight `pendingDeletion` on the `galleryModel` (path + caption + files-to-remove + deadline). The entry stays in the list, rendered with a danger-colored border and a footer countdown driven by a ~300ms tick. After a 5s `tea.Tick` the deletion commits — the file(s) are `os.Remove`d and `reload` drops the entry via the existing "undecodable → dropped" path (no manifest rewrite). A generation counter (`delGen`) debounces stale timers, mirroring the existing `vecGen`/`rasterGen` pattern.

**Tech Stack:** Go, bubbletea v2, lipgloss v2, standard library (`os`, `time`, `math`, `strings`, `path/filepath`).

## Global Constraints

- Viewer stays a **read-only** manifest consumer — never write `images/<pane>.jsonl`.
- d2 deletion scope = **rendered artifacts only**: both theme PNG files + both theme SVGs, derived from the entry via `withTheme`. Do **not** touch the `.d2` source.
- No pixel dimming / per-frame raster re-encode — pending state is conveyed by border + caption + text countdown only.
- Single pending deletion at a time (single-level undo).
- Match existing code style: small focused funcs, early returns, comments only for non-obvious *why*.
- All new tests use `t.TempDir()` + `t.Setenv("AEYE_DIR", dir)` and the existing `writeTestImage(t, path, w, h)` / `writeTestPNG(t)` helpers.
- Run tests with `go test -count=1 ./...` from inside the worktree (gopls diagnostics in a wt worktree are unreliable; trust `go build`/`go test`).

---

### Task 1: File-cluster resolution (`filesToDelete`)

Pure method on `imageEntry` returning the on-disk files a deletion should remove.

**Files:**
- Modify: `gallery_render.go` (add method near `withTheme`, ~line 157)
- Test: `gallery_render_test.go` (new file)

**Interfaces:**
- Consumes: existing `withTheme(path, mode string) string` (gallery_render.go:145), `imageEntry` (gallery_render.go:18).
- Produces: `func (e imageEntry) filesToDelete() []string`

- [ ] **Step 1: Write the failing test**

Create `gallery_render_test.go`:

```go
package main

import (
	"sort"
	"testing"
)

func TestFilesToDeletePlain(t *testing.T) {
	e := imageEntry{Path: "/shots/login.png", Source: "Read"}
	got := e.filesToDelete()
	if len(got) != 1 || got[0] != "/shots/login.png" {
		t.Fatalf("filesToDelete() = %v, want [/shots/login.png]", got)
	}
}

func TestFilesToDeleteD2Cluster(t *testing.T) {
	e := imageEntry{
		Path:   "/d/hash-dark.png",
		Vector: "/d/hash-dark.svg",
		Source: "d2",
	}
	got := e.filesToDelete()
	sort.Strings(got)
	want := []string{
		"/d/hash-dark.png", "/d/hash-dark.svg",
		"/d/hash-light.png", "/d/hash-light.svg",
	}
	if len(got) != len(want) {
		t.Fatalf("filesToDelete() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("filesToDelete() = %v, want %v", got, want)
		}
	}
}

func TestFilesToDeleteD2NoVector(t *testing.T) {
	e := imageEntry{Path: "/d/hash-dark.png", Source: "d2"}
	got := e.filesToDelete()
	if len(got) != 2 {
		t.Fatalf("filesToDelete() = %v, want the two png variants only", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -count=1 -run TestFilesToDelete ./...`
Expected: FAIL — `e.filesToDelete undefined`

- [ ] **Step 3: Write minimal implementation**

Add to `gallery_render.go` after `withTheme` (~line 157):

```go
// filesToDelete returns the on-disk files removed when this entry is deleted
// from the carousel. A plain capture is a single file; a d2 diagram is the
// whole rendered cluster — both theme PNG files and both theme SVGs — so a later
// theme switch can't resurrect a half-deleted diagram. The .d2 source lives in
// the adapter-owned src dir and is deliberately left untouched (the viewer
// stays decoupled from the hook's layout).
func (e imageEntry) filesToDelete() []string {
	if e.Source != "d2" {
		return []string{e.Path}
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range []string{e.Path, e.Vector} {
		if p == "" {
			continue
		}
		for _, mode := range []string{"light", "dark"} {
			v := withTheme(p, mode)
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -count=1 -run TestFilesToDelete ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gallery_render.go gallery_render_test.go
git commit -m "feat(carousel): filesToDelete resolves the delete file cluster (#137)"
```

---

### Task 2: Countdown bar renderer (`countdownBar`)

Pure function rendering a depleting `▓▓▓░░ 2s` bar. No model, no I/O.

**Files:**
- Modify: `gallery.go` (add near the other small render helpers; add `"math"` import)
- Test: `gallery_test.go`

**Interfaces:**
- Consumes: existing `clamp(v, lo, hi int) int`.
- Produces: `func countdownBar(remaining, total time.Duration, width int) string`

- [ ] **Step 1: Write the failing test**

Add to `gallery_test.go`:

```go
func TestCountdownBar(t *testing.T) {
	total := 5 * time.Second
	cases := []struct {
		name      string
		remaining time.Duration
		want      string
	}{
		{"full", 5 * time.Second, "▓▓▓▓▓ 5s"},
		{"half", 2 * time.Second, "▓▓░░░ 2s"},
		{"empty", 0, "░░░░░ 0s"},
		{"negative clamps to empty", -1 * time.Second, "░░░░░ 0s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countdownBar(tc.remaining, total, 5); got != tc.want {
				t.Fatalf("countdownBar(%v) = %q, want %q", tc.remaining, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -count=1 -run TestCountdownBar ./...`
Expected: FAIL — `undefined: countdownBar`

- [ ] **Step 3: Write minimal implementation**

Ensure `"math"` is in `gallery.go`'s import block, then add the helper (near `stripStart`, ~line 79):

```go
// countdownBar renders a depleting bar of `width` cells for `remaining` of a
// `total` window, suffixed with whole seconds remaining (e.g. "▓▓░░░ 2s"). Used
// for the pending-deletion undo countdown. Text only — no raster work.
func countdownBar(remaining, total time.Duration, width int) string {
	if remaining < 0 {
		remaining = 0
	}
	filled := clamp(int(math.Round(float64(remaining)/float64(total)*float64(width))), 0, width)
	secs := int(math.Ceil(remaining.Seconds()))
	return strings.Repeat("▓", filled) + strings.Repeat("░", width-filled) + fmt.Sprintf(" %ds", secs)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -count=1 -run TestCountdownBar ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gallery.go gallery_test.go
git commit -m "feat(carousel): countdownBar renders the undo-window bar (#137)"
```

---

### Task 3: Pending-deletion state + lifecycle methods

Add the model fields and the three lifecycle methods (`markPending`, `undoPending`, `commitPending`). No key/timer wiring yet.

**Files:**
- Modify: `gallery.go` — `galleryModel` struct (~line 85), new methods, new constants.
- Test: `gallery_test.go`

**Interfaces:**
- Consumes: `imageEntry.filesToDelete()` (Task 1), `imageEntry.caption()`, model fields `images`, `cursor`, `status`.
- Produces:
  - `type pendingDeletion struct { path, name string; files []string; deadline time.Time }`
  - `const undoWindow = 5 * time.Second`
  - `const countdownTick = 300 * time.Millisecond`
  - model fields: `pending *pendingDeletion`, `delGen uint64`
  - `func (m *galleryModel) markPending()`
  - `func (m *galleryModel) undoPending()`
  - `func (m *galleryModel) commitPending()`

- [ ] **Step 1: Write the failing test**

Add to `gallery_test.go`:

```go
func TestPendingLifecycle(t *testing.T) {
	f := writeTestPNG(t)
	m := &galleryModel{images: []imageEntry{{Path: f, Source: "Read"}}}

	m.markPending()
	if m.pending == nil || m.pending.path != f {
		t.Fatalf("markPending did not set pending for %q: %+v", f, m.pending)
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("file removed too early: %v", err)
	}
	gen := m.delGen

	// Undo clears pending, bumps the generation, leaves the file on disk.
	m.undoPending()
	if m.pending != nil {
		t.Fatalf("undoPending left pending set: %+v", m.pending)
	}
	if m.delGen == gen {
		t.Fatalf("undoPending did not bump delGen")
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("undo should not remove the file: %v", err)
	}

	// Commit removes the file.
	m.markPending()
	m.commitPending()
	if m.pending != nil {
		t.Fatalf("commitPending left pending set")
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatalf("commitPending did not remove the file (err=%v)", err)
	}
}

func TestMarkPendingEmptyIsNoop(t *testing.T) {
	m := &galleryModel{}
	m.markPending()
	if m.pending != nil {
		t.Fatalf("markPending on empty carousel set pending: %+v", m.pending)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -count=1 -run 'TestPendingLifecycle|TestMarkPendingEmptyIsNoop' ./...`
Expected: FAIL — `m.markPending undefined` (and `pending` field undefined)

- [ ] **Step 3: Write minimal implementation**

Add fields to `galleryModel` (after `status string`, ~line 109):

```go
	// Pending deletion: the selected entry is marked (danger border + countdown)
	// for undoWindow before the file(s) are removed. Single-level undo. delGen
	// invalidates in-flight commit/countdown ticks when the pending set changes,
	// mirroring vecGen/rasterGen.
	pending *pendingDeletion
	delGen  uint64
```

Add near the other model types (~line 145):

```go
type pendingDeletion struct {
	path     string   // manifest path; match key that survives the reload poll
	name     string   // caption, for the footer line
	files    []string // resolved at mark time; removed on commit
	deadline time.Time
}

const (
	undoWindow    = 5 * time.Second
	countdownTick = 300 * time.Millisecond
)
```

Add the methods (near `reload`, ~line 241):

```go
// markPending marks the selected entry for deletion. Any prior pending deletion
// commits first (single-level undo). No-op on an empty carousel.
func (m *galleryModel) markPending() {
	if len(m.images) == 0 {
		return
	}
	e := m.images[m.cursor]
	if m.pending != nil {
		m.commitPending()
	}
	m.pending = &pendingDeletion{
		path:     e.Path,
		name:     e.caption(),
		files:    e.filesToDelete(),
		deadline: time.Now().Add(undoWindow),
	}
	m.delGen++
}

// undoPending cancels a pending deletion, leaving every file on disk. Bumping
// delGen makes the scheduled commit tick a no-op when it fires.
func (m *galleryModel) undoPending() {
	if m.pending == nil {
		return
	}
	m.pending = nil
	m.delGen++
	m.status = "Deletion cancelled"
}

// commitPending removes the pending entry's files and clears the mark. The
// entry drops out of the carousel on the next reload via the existing
// "undecodable → dropped" path; the manifest is never rewritten.
func (m *galleryModel) commitPending() {
	if m.pending == nil {
		return
	}
	for _, f := range m.pending.files {
		os.Remove(f)
	}
	m.pending = nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -count=1 -run 'TestPendingLifecycle|TestMarkPendingEmptyIsNoop' ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gallery.go gallery_test.go
git commit -m "feat(carousel): pending-deletion state + lifecycle methods (#137)"
```

---

### Task 4: Key handling, commit/countdown timers, reload guard

Wire `x`/`u` keys, the two `tea.Tick` messages with `delGen` debounce, commit-on-quit, and reload's pending-cleanup.

**Files:**
- Modify: `gallery.go` — `KeyPressMsg` switch (~line 287, 369–374), `Update` message cases (~line 393), `reload` (~line 241), add `scheduleDeleteTicks`.
- Test: `gallery_test.go`

**Interfaces:**
- Consumes: `markPending`/`undoPending`/`commitPending` (Task 3), existing `m.schedulePaint()`, `m.reload()`.
- Produces:
  - `type deleteCommitMsg struct{ gen uint64 }`
  - `type deleteCountdownMsg struct{ gen uint64 }`
  - `func (m *galleryModel) scheduleDeleteTicks() tea.Cmd`

- [ ] **Step 1: Write the failing test**

Add to `gallery_test.go`:

```go
func TestDeleteCommitMsgGenGate(t *testing.T) {
	f := writeTestPNG(t)
	base := galleryModel{
		images:  []imageEntry{{Path: f, Source: "Read"}},
		pending: &pendingDeletion{path: f, files: []string{f}, deadline: time.Now()},
		delGen:  7,
	}

	// Stale generation: commit is ignored, file survives.
	stale := base
	m2, _ := stale.Update(deleteCommitMsg{gen: 6})
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("stale commit removed the file: %v", err)
	}
	if m2.(galleryModel).pending == nil {
		t.Fatalf("stale commit cleared pending")
	}

	// Current generation: commit removes the file and clears pending.
	current := base
	m3, _ := current.Update(deleteCommitMsg{gen: 7})
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatalf("current commit did not remove the file (err=%v)", err)
	}
	if m3.(galleryModel).pending != nil {
		t.Fatalf("current commit left pending set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -count=1 -run TestDeleteCommitMsgGenGate ./...`
Expected: FAIL — `undefined: deleteCommitMsg`

- [ ] **Step 3: Write minimal implementation**

**3a.** Add the message types + scheduler near the other tick helpers (~line 163):

```go
type deleteCommitMsg struct{ gen uint64 }
type deleteCountdownMsg struct{ gen uint64 }

// scheduleDeleteTicks arms the commit deadline and the countdown repaint for the
// current pending generation. Both carry delGen so a superseding x/u makes them
// no-op when they fire.
func (m *galleryModel) scheduleDeleteTicks() tea.Cmd {
	gen := m.delGen
	return tea.Batch(
		tea.Tick(undoWindow, func(time.Time) tea.Msg { return deleteCommitMsg{gen} }),
		tea.Tick(countdownTick, func(time.Time) tea.Msg { return deleteCountdownMsg{gen} }),
	)
}
```

**3b.** Add the `x`/`u` cases in the `KeyPressMsg` switch, before `default:` (~line 374). These early-return, so they skip the fall-through `scheduleVector`:

```go
		case "x":
			m.markPending()
			return m, tea.Batch(m.scheduleDeleteTicks(), m.schedulePaint())
		case "u":
			m.undoPending()
			return m, m.schedulePaint()
```

**3c.** Commit any pending deletion on quit — change the quit case (~line 287):

```go
		case "q", "ctrl+c":
			m.commitPending()
			return m, tea.Quit
```

**3d.** Add the two message cases in `Update` (near `vectorKickMsg`, ~line 393):

```go
	case deleteCommitMsg:
		if msg.gen != m.delGen || m.pending == nil {
			return m, nil
		}
		m.commitPending()
		m.reload()
		return m, m.schedulePaint()
	case deleteCountdownMsg:
		// Repaint-only tick: re-renders the depleting bar (text layer; rasters
		// are stored by id and not re-transmitted). Stops once pending clears.
		if msg.gen != m.delGen || m.pending == nil {
			return m, nil
		}
		return m, tea.Tick(countdownTick, func(time.Time) tea.Msg { return deleteCountdownMsg{msg.gen} })
```

**3e.** Add the pending-cleanup guard at the end of `reload` (after the pin/warm block, ~line 240), so an externally-vanished entry clears the mark:

```go
	if m.pending != nil {
		found := false
		for _, e := range m.images {
			if e.Path == m.pending.path {
				found = true
				break
			}
		}
		if !found {
			m.pending = nil
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -count=1 -run TestDeleteCommitMsgGenGate ./...`
Expected: PASS

Then the whole suite: `go test -count=1 ./...` → PASS.

- [ ] **Step 5: Commit**

```bash
git add gallery.go gallery_test.go
git commit -m "feat(carousel): wire x/u keys, commit + countdown timers (#137)"
```

---

### Task 5: Rendering — danger border, marked caption, footer countdown, legend

Surface the pending state in the view: danger border on the pending filmstrip cell and preview frame, a `✗` caption marker, the footer countdown line, and `x del` in the legend. Resolve `dangerColor` once at startup.

**Files:**
- Modify: `gallery.go` — `galleryModel` struct (color field, ~line 122), `runGallery` color resolution (~line 783), `View`/render (preview border ~642, subtitle ~651, filmstrip border ~675, legend ~691).
- Test: `gallery_test.go`

**Interfaces:**
- Consumes: `countdownBar` (Task 2), `m.pending` (Task 3), existing `thmColor`, `truncateToWidth`, `borderWithTitle`.
- Produces: model field `dangerColor imgcolor.Color`; visible-only render changes (asserted via the footer-string helper below).

- [ ] **Step 1: Write the failing test**

The footer string is the cleanest testable seam. Extract it into a helper and test that.

Add to `gallery_test.go`:

```go
func TestActionRowPending(t *testing.T) {
	m := &galleryModel{
		width: 80,
		pending: &pendingDeletion{
			name:     "diagram",
			deadline: time.Now().Add(2 * time.Second),
		},
	}
	got := m.actionRow()
	for _, want := range []string{"diagram", "u to undo", "▓"} {
		if !strings.Contains(got, want) {
			t.Fatalf("actionRow() = %q, missing %q", got, want)
		}
	}
}

func TestActionRowIdleShowsKeys(t *testing.T) {
	m := &galleryModel{width: 80}
	got := m.actionRow()
	if !strings.Contains(got, "x del") {
		t.Fatalf("actionRow() = %q, want the x del hint", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -count=1 -run TestActionRow ./...`
Expected: FAIL — `m.actionRow undefined`

- [ ] **Step 3: Write minimal implementation**

**3a.** Add the color field to `galleryModel` (with the other theme colors, ~line 122):

```go
	selColor, dimColor, hintFg, textFg, dangerColor imgcolor.Color
```

**3b.** Resolve it in `runGallery` with the other colors (~line 783):

```go
	m.dangerColor = m.thmColor("@thm_red", "#f38ba8", "#d20f39")
```

**3c.** Add the `actionRow` helper (near the `View`/render func). It replaces the inline `actionKeys`/`m.status` block; note the new `x del` entry:

```go
// actionRow is the footer's second line: the pending-deletion countdown when a
// deletion is in flight, else a transient status, else the action-key hints.
func (m galleryModel) actionRow() string {
	actionKeys := "↵ open · O folder · y copy · d drag · x del · r reload · q quit"
	if m.pending != nil {
		line := fmt.Sprintf("✗ Deleting %s  %s — u to undo",
			m.pending.name, countdownBar(time.Until(m.pending.deadline), undoWindow, 5))
		return lipgloss.NewStyle().Foreground(m.dangerColor).Render(truncateToWidth(line, m.width))
	}
	if m.status != "" {
		return lipgloss.NewStyle().Foreground(m.selColor).Render(truncateToWidth(m.status, m.width))
	}
	return lipgloss.NewStyle().Foreground(m.hintFg).Render(truncateToWidth(actionKeys, m.width))
}
```

**3d.** Replace the inline second-line block (~line 691–697) with:

```go
	second := m.actionRow()
```

(Delete the old `actionKeys := …`, `second := hintStyle.Render(…)`, and the `if m.status != "" { … }` lines it replaces.)

**3e.** Danger border on the pending filmstrip cell — in the strip loop (~line 675):

```go
			border := dimColor
			if idx == m.cursor {
				border = selColor
			}
			if m.pending != nil && m.images[idx].Path == m.pending.path {
				border = m.dangerColor
			}
```

**3f.** Danger frame on the preview when the selected entry is pending (~line 642):

```go
	frameColor := selColor
	if m.pending != nil && len(m.images) > 0 && m.images[m.cursor].Path == m.pending.path {
		frameColor = m.dangerColor
	}
	preview = borderWithTitle(preview, m.l.previewW, context, frameColor)
```

**3g.** `✗` prefix + danger color on the subtitle caption when the selected entry is pending (~line 651). Replace the `subtitle :=` assignment with:

```go
	capText := m.images[m.cursor].caption()
	capFg := textFg
	if m.pending != nil && m.images[m.cursor].Path == m.pending.path {
		capText = "✗ " + capText
		capFg = m.dangerColor
	}
	subtitle := center(lipgloss.NewStyle().Foreground(capFg).Render(
		truncateToWidth(fmt.Sprintf("[%d/%d]  %s", m.cursor+1, len(m.images), capText), m.width)))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -count=1 -run TestActionRow ./...` → PASS
Run: `go test -count=1 ./...` → PASS
Run: `go build ./...` → no errors (confirms the `hintStyle` removal didn't orphan a reference; if `hintStyle` is still used by the nav row, leave its declaration in place).

- [ ] **Step 5: Commit**

```bash
git add gallery.go gallery_test.go
git commit -m "feat(carousel): render pending-deletion border, caption, countdown (#137)"
```

---

### Task 6: End-to-end verification + docs

Confirm the feature works in the real carousel and record the key in user-facing docs.

**Files:**
- Modify: `README.md` (keybinding list, if one exists), `CHANGELOG.md` if hand-maintained (check first — release-please may own it).

- [ ] **Step 1: Full test + vet**

Run:
```bash
go test -count=1 ./...
go vet ./...
gofmt -l .
```
Expected: tests PASS, vet clean, `gofmt -l` prints nothing.

- [ ] **Step 2: Drive the real carousel**

Build a trace binary and open the carousel against a scratch manifest with a couple of throwaway images, then exercise the flow:

```bash
go build -o /tmp/aeye-137 .
```

Follow the `verify` skill / `AEYE_DEBUG` bridge (see memory `aeye-debug-trace-and-125`): launch the viewer, press `x` on an image → confirm the danger border, `✗` caption, and depleting `▓▓▓░░ Ns` countdown appear; press `u` before 5s → mark clears, file still present; press `x` and wait >5s → entry disappears and the underlying file is gone from disk (`ls` the path). Verify `q` while a deletion is pending removes the file too.

Record what you observed (which cues rendered, that the file was actually removed) in the PR description.

- [ ] **Step 3: Document the keybinding**

Check `README.md` for a keybinding/usage list; if present, add `x` (delete image, with `u` to undo). Do not invent a section that doesn't exist. Leave `CHANGELOG.md` to release-please unless it's clearly hand-edited.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "docs(carousel): document x delete / u undo keybinding (#137)"
```

---

## Self-Review

**Spec coverage:**
- `x` marks selected, stays visible, danger border + `✗` caption + countdown → Tasks 3, 5.
- `u` cancels within window → Tasks 3, 4.
- 5s commit → `os.Remove` → Tasks 3, 4.
- Plain vs d2 cluster (rendered artifacts only) → Task 1.
- No manifest rewrite; decode-drop keeps it out → relies on existing `loadManifest` (Task 4 `reload` guard covers the mark).
- Reload re-applies mark by path across the 1.5s poll → path-keyed render checks (Task 5) + reload cleanup only clears when the file truly vanished (Task 4).
- Countdown tick only while pending, text-only → Task 4 `deleteCountdownMsg`.
- delGen debounce like vecGen/rasterGen → Tasks 3, 4.
- Single-level undo; second `x` commits prior; quit commits → Tasks 3 (`markPending`), 4 (quit case).
- Footer `x del` → Task 5.
- Deferred multi-select / cross-session backfill → out of scope, no task (correct).

**Placeholder scan:** none — every step carries concrete code and commands.

**Type consistency:** `pendingDeletion{path,name,files,deadline}` defined in Task 3 and used identically in Tasks 4–5. `deleteCommitMsg`/`deleteCountdownMsg{gen uint64}`, `scheduleDeleteTicks`, `markPending`/`undoPending`/`commitPending`, `filesToDelete`, `countdownBar`, `actionRow`, `dangerColor` — names consistent across tasks. `imgcolor.Color` matches the existing color-field type.
