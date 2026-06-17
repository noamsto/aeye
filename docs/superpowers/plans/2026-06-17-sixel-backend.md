# Sixel Rendering Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render crisp sixel thumbnails in the carousel on terminals that support sixel but not the kitty unicode-placeholder protocol (wezterm standalone, and any host inside a sixel-capable tmux).

**Architecture:** A new `backendRaster` joins `backendKitty`/`backendSymbols`. Selection is env-first with a DA1 sixel capability probe as the in-tmux/ambiguous fallback. The backend leaves blank "holes" in `View()` and paints `chafa -f sixels` output out-of-band on `/dev/tty` at cell coordinates taken from the existing `previewRect()`/`filmstripCellRects()` helpers. Painting is driven by a debounced post-render tick (mirroring the existing `vecGen` vector-render debounce) so the overlay always lands after bubbletea has flushed its frame.

**Tech Stack:** Go, `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `chafa` (sixel encoder), `github.com/charmbracelet/x/term` (raw-mode tty for the probe — already a dependency).

**Spec:** `docs/superpowers/specs/2026-06-17-raster-backend-design.md`

---

## File Structure

- **`gallery_raster.go`** (new) — everything sixel: `parseSixelDA`, `probeSixel`, `renderSixel`, `paintSixelAt`, `paintPreview`, `paintStrip`, `paintRaster`, `rasterPaintMsg`, `schedulePaint`. One responsibility: the sixel backend, mirroring how kitty primitives sit in `gallery_render.go`.
- **`gallery_raster_test.go`** (new) — host-independent unit tests for the pure pieces (`parseSixelDA`, `paintSixelAt`, `blankBlock`).
- **`gallery_render.go`** (modify) — add `backendRaster` const, rewrite `chooseGridBackend`, add `blankBlock`.
- **`gallery.go`** (modify) — `runGallery` passes probe + env to `chooseGridBackend`; `renderView` grows raster arms; `galleryModel` gains `rasterGen`; `Update` schedules/handles the paint.
- **`gallery_test.go`** (modify) — update existing `TestChooseGridBackend` for the new signature.

---

### Task 1: DA1 sixel capability probe

**Files:**
- Create: `gallery_raster.go`
- Test: `gallery_raster_test.go`

- [ ] **Step 1: Write the failing test for the DA1 reply parser**

Create `gallery_raster_test.go`:

```go
package main

import "testing"

func TestParseSixelDA(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"\x1b[?62;4;6;9;22c", true},                  // VT, sixel (4) present
		{"\x1b[?64;1;2;4;6;9;15;18;21;22c", true},      // 4 in a long list
		{"\x1b[?14;4c", true},                          // 4 at the end
		{"\x1b[?62;6;9;22c", false},                    // no 4
		{"\x1b[?1;2c", false},                          // no 4
		{"\x1b[?44c", false},                           // 44 is not 4
		{"\x1b[?62;4", false},                          // no 'c' terminator
		{"garbage", false},                             // no '?'
		{"", false},                                    // empty
	}
	for _, c := range cases {
		if got := parseSixelDA(c.in); got != c.want {
			t.Errorf("parseSixelDA(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `direnv exec . go test -run TestParseSixelDA ./...`
Expected: FAIL — `undefined: parseSixelDA`.

- [ ] **Step 3: Write `gallery_raster.go` with the parser and the probe**

```go
package main

import (
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
)

// parseSixelDA reports whether a Primary Device Attributes reply advertises
// sixel support (attribute 4). Reply form: ESC [ ? p1 ; p2 ; ... c
func parseSixelDA(resp string) bool {
	i := strings.IndexByte(resp, '?')
	j := strings.LastIndexByte(resp, 'c')
	if i < 0 || j < 0 || j < i {
		return false
	}
	for _, p := range strings.Split(resp[i+1:j], ";") {
		if p == "4" {
			return true
		}
	}
	return false
}

// probeSixel asks the terminal whether it can render sixel by writing a Primary
// Device Attributes query (ESC [ c) to /dev/tty and checking the reply for the
// sixel attribute (4). It runs in raw mode with a short deadline and fully drains
// the reply to the 'c' terminator, so a late response can't leak onto bubbletea's
// stdin and be misparsed as keystrokes. Any failure (no tty, timeout, no reply)
// returns false, so we never emit sixel bytes to an unconfirmed terminal.
//
// Inside tmux, tmux answers this query reflecting its own sixel capability
// (present when tmux is built --enable-sixel over a sixel-capable outer terminal).
func probeSixel() bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	old, err := term.MakeRaw(tty.Fd())
	if err != nil {
		return false
	}
	defer term.Restore(tty.Fd(), old)

	if _, err := tty.WriteString("\x1b[c"); err != nil {
		return false
	}

	ch := make(chan string, 1)
	go func() {
		var buf []byte
		b := make([]byte, 1)
		for {
			n, err := tty.Read(b)
			if n > 0 {
				buf = append(buf, b[0])
				if b[0] == 'c' {
					break
				}
			}
			if err != nil {
				break
			}
		}
		ch <- string(buf)
	}()

	select {
	case s := <-ch:
		return parseSixelDA(s)
	case <-time.After(150 * time.Millisecond):
		// Deferred tty.Close() unblocks the goroutine's Read, so it can't leak.
		return false
	}
}
```

- [ ] **Step 4: Run the parser test to verify it passes**

Run: `direnv exec . go test -run TestParseSixelDA ./...`
Expected: PASS.

- [ ] **Step 5: Update go.mod (promote x/term from indirect to direct)**

Run: `direnv exec . go mod tidy`
Expected: `github.com/charmbracelet/x/term` loses its `// indirect` comment in `go.mod`; no other changes.

- [ ] **Step 6: Commit**

```bash
git add gallery_raster.go gallery_raster_test.go go.mod go.sum
git commit -m "feat(gallery): add DA1 sixel capability probe (#60)"
```

---

### Task 2: `backendRaster` constant and `chooseGridBackend` rewrite

**Files:**
- Modify: `gallery_render.go:129-144` (const block + `chooseGridBackend`)
- Modify: `gallery.go:506` (the `chooseGridBackend` call in `runGallery`)
- Test: `gallery_test.go:40-57` (rewrite `TestChooseGridBackend`)

- [ ] **Step 1: Rewrite the failing test for the new signature**

Replace `TestChooseGridBackend` in `gallery_test.go` (currently lines 40-57) with:

```go
func TestChooseGridBackend(t *testing.T) {
	yes := func() bool { return true }
	no := func() bool { return false }
	noProbe := func() bool { t.Fatal("probe must not run on a fast-path"); return false }
	cases := []struct {
		name        string
		term        string
		inTmux      bool
		weztermPane string
		envTerm     string
		probe       func() bool
		want        gridBackend
	}{
		{"kitty termname", "xterm-kitty", false, "", "xterm-kitty", noProbe, backendKitty},
		{"ghostty termname", "xterm-ghostty", false, "", "xterm-ghostty", noProbe, backendKitty},
		{"kitty suffix", "xterm-kitty-direct", false, "", "", noProbe, backendKitty},
		{"wezterm standalone", "xterm-256color", false, "1", "xterm-256color", noProbe, backendRaster},
		{"foot standalone", "foot", false, "", "foot", noProbe, backendRaster},
		{"in tmux, probe yes", "tmux-256color", true, "", "tmux-256color", yes, backendRaster},
		{"in tmux, probe no", "tmux-256color", true, "", "tmux-256color", no, backendSymbols},
		{"leaked weztermpane in tmux still probes", "tmux-256color", true, "1", "tmux-256color", no, backendSymbols},
		{"unknown standalone, probe yes", "xterm-256color", false, "", "xterm-256color", yes, backendRaster},
		{"unknown standalone, probe no", "xterm-256color", false, "", "xterm-256color", no, backendSymbols},
	}
	for _, c := range cases {
		got := chooseGridBackend(c.term, c.inTmux, c.weztermPane, c.envTerm, c.probe)
		if got != c.want {
			t.Errorf("%s: chooseGridBackend = %v, want %v", c.name, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `direnv exec . go test -run TestChooseGridBackend ./...`
Expected: FAIL — too many arguments to `chooseGridBackend` / `undefined: backendRaster`.

- [ ] **Step 3: Add the `backendRaster` const**

In `gallery_render.go`, change the const block (currently lines 131-134):

```go
const (
	backendKitty gridBackend = iota
	backendSymbols
	backendRaster
)
```

- [ ] **Step 4: Rewrite `chooseGridBackend`**

Replace `chooseGridBackend` (currently lines 136-144) with:

```go
// chooseGridBackend picks the grid renderer. The kitty backend needs unicode
// PLACEHOLDERS (not just kitty graphics), which has no capability query and which
// wezterm lacks despite speaking graphics — so kitty stays a termname allowlist,
// never a probe. Otherwise: a standalone (no tmux) host identified by env gets
// sixel with no probe latency; everything else (notably in-tmux, where the only
// reliable signal is whether THIS tmux passes sixel) falls to the DA1 probe.
// probeSixel is injected so this stays a pure, testable function and is invoked
// lazily — only on the probe branch.
func chooseGridBackend(termname string, inTmux bool, weztermPane, term string, probeSixel func() bool) gridBackend {
	if strings.HasPrefix(termname, "xterm-kitty") || strings.HasPrefix(termname, "xterm-ghostty") {
		return backendKitty
	}
	if !inTmux && (weztermPane != "" || strings.HasPrefix(term, "foot")) {
		return backendRaster
	}
	if probeSixel() {
		return backendRaster
	}
	return backendSymbols
}
```

- [ ] **Step 5: Update the caller in `runGallery`**

In `gallery.go`, the model literal currently has (line 506):

```go
		backend: chooseGridBackend(termName()),
```

Replace with:

```go
		backend: chooseGridBackend(termName(), os.Getenv("TMUX") != "", os.Getenv("WEZTERM_PANE"), os.Getenv("TERM"), probeSixel),
```

(`os` is already imported in `gallery.go`.)

- [ ] **Step 6: Run the test to verify it passes**

Run: `direnv exec . go test -run TestChooseGridBackend ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add gallery_render.go gallery.go gallery_test.go
git commit -m "feat(gallery): select backendRaster via env + sixel probe (#60)"
```

---

### Task 3: Blank-hole emitter and `renderView` raster arms

**Files:**
- Modify: `gallery_render.go` (add `blankBlock` near `symbolsBlock`)
- Modify: `gallery.go:382-387` and `gallery.go:421-426` (renderView preview + thumb switches)
- Test: `gallery_raster_test.go` (add `TestBlankBlock`)

- [ ] **Step 1: Write the failing test**

Add to `gallery_raster_test.go`:

```go
func TestBlankBlock(t *testing.T) {
	if got := blankBlock(3, 2); got != "   \n   " {
		t.Errorf("blankBlock(3,2) = %q, want %q", got, "   \n   ")
	}
	if got := blankBlock(2, 1); got != "  " {
		t.Errorf("blankBlock(2,1) = %q, want %q", got, "  ")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `direnv exec . go test -run TestBlankBlock ./...`
Expected: FAIL — `undefined: blankBlock`.

- [ ] **Step 3: Add `blankBlock` to `gallery_render.go`**

Add at the end of `gallery_render.go`:

```go
// blankBlock is h lines of w spaces — the hole the raster backend paints a sixel
// image into out-of-band. It is to backendRaster what placeholderBlock is to
// backendKitty: the View()-side placeholder occupying the image's cells.
func blankBlock(w, h int) string {
	row := strings.Repeat(" ", w)
	rows := make([]string, h)
	for i := range rows {
		rows[i] = row
	}
	return strings.Join(rows, "\n")
}
```

(`strings` is already imported in `gallery_render.go`.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `direnv exec . go test -run TestBlankBlock ./...`
Expected: PASS.

- [ ] **Step 5: Add the raster arm to the preview switch**

In `gallery.go` `renderView`, the preview block is currently (lines 382-387):

```go
	var preview string
	if m.backend == backendKitty {
		preview = placeholderBlock(previewID, m.l.previewW, m.l.previewH)
	} else {
		preview = symbolsBlock(m.images[m.cursor].Path, m.l.previewW, m.l.previewH)
	}
```

Replace with a three-way switch:

```go
	var preview string
	switch m.backend {
	case backendKitty:
		preview = placeholderBlock(previewID, m.l.previewW, m.l.previewH)
	case backendRaster:
		preview = blankBlock(m.l.previewW, m.l.previewH)
	default:
		preview = symbolsBlock(m.images[m.cursor].Path, m.l.previewW, m.l.previewH)
	}
```

- [ ] **Step 6: Add the raster arm to the thumbnail switch**

In `gallery.go` `renderView`, the thumb block is currently (lines 421-426):

```go
		var thumb string
		if m.backend == backendKitty {
			thumb = placeholderBlock(s+1, m.l.stripW, m.l.stripH)
		} else {
			thumb = symbolsBlock(m.images[idx].Path, m.l.stripW, m.l.stripH)
		}
```

Replace with:

```go
		var thumb string
		switch m.backend {
		case backendKitty:
			thumb = placeholderBlock(s+1, m.l.stripW, m.l.stripH)
		case backendRaster:
			thumb = blankBlock(m.l.stripW, m.l.stripH)
		default:
			thumb = symbolsBlock(m.images[idx].Path, m.l.stripW, m.l.stripH)
		}
```

- [ ] **Step 7: Run the full build and tests**

Run: `direnv exec . go build ./... && direnv exec . go test ./...`
Expected: build OK, all tests PASS. (At this point a raster terminal shows blank holes — no image yet; that's Task 4-5.)

- [ ] **Step 8: Commit**

```bash
git add gallery_render.go gallery.go
git commit -m "feat(gallery): emit blank holes for backendRaster in View (#60)"
```

---

### Task 4: Sixel paint primitives

**Files:**
- Modify: `gallery_raster.go` (add `renderSixel`, `paintSixelAt`, `paintPreview`, `paintStrip`, `paintRaster`)
- Test: `gallery_raster_test.go` (add `TestPaintSixelAt`)

- [ ] **Step 1: Write the failing test for `paintSixelAt`**

Add to `gallery_raster_test.go`:

```go
import "strings" // add to the existing import block

func TestPaintSixelAt(t *testing.T) {
	var b strings.Builder
	paintSixelAt(&b, rect{x: 4, y: 2, w: 8, h: 4}, "SIXELDATA")
	// Cursor coords are 1-based: row = y+1 = 3, col = x+1 = 5.
	want := "\x1b7\x1b[3;5HSIXELDATA\x1b8"
	if b.String() != want {
		t.Errorf("paintSixelAt =\n%q\nwant\n%q", b.String(), want)
	}
}

func TestPaintSixelAtEmpty(t *testing.T) {
	var b strings.Builder
	paintSixelAt(&b, rect{x: 1, y: 1, w: 4, h: 4}, "")
	if b.String() != "" {
		t.Errorf("paintSixelAt with empty payload wrote %q, want nothing", b.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `direnv exec . go test -run TestPaintSixelAt ./...`
Expected: FAIL — `undefined: paintSixelAt`.

- [ ] **Step 3: Add the paint primitives to `gallery_raster.go`**

Extend the import block of `gallery_raster.go` to:

```go
import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
)
```

Add these functions:

```go
// renderSixel rasterizes a PNG to a sixel payload sized to cols×rows cells via
// chafa, or "" on failure. chafa's non-tty default cell geometry is 10×20 px,
// matching cellPxW/cellPxH, so the image fills the cell box without tuning.
// chafa wraps output in cursor hide/show (ESC [?25l … ESC [?25h); strip them so
// they don't fight the TUI's cursor management (same as symbolsBlock).
func renderSixel(pngPath string, cols, rows int) string {
	out, err := exec.Command("chafa", "-f", "sixels", "--size", fmt.Sprintf("%dx%d", cols, rows), pngPath).Output()
	if err != nil {
		return ""
	}
	s := string(out)
	s = strings.ReplaceAll(s, "\x1b[?25l", "")
	s = strings.ReplaceAll(s, "\x1b[?25h", "")
	return strings.TrimRight(s, "\n")
}

// paintSixelAt writes a sixel payload at rect r's top-left, bracketed by
// save/restore-cursor so the out-of-band paint never disturbs bubbletea's cursor.
// Cursor coordinates are 1-based; rects are 0-based (origin top-left). No-op on
// an empty payload (chafa failed / nothing to draw).
func paintSixelAt(w io.Writer, r rect, sixel string) {
	if sixel == "" {
		return
	}
	fmt.Fprintf(w, "\x1b7\x1b[%d;%dH%s\x1b8", r.y+1, r.x+1, sixel)
}

// paintPreview paints the selected image into the preview rect, honoring the
// current zoom/crop (mirrors transmitView's source selection).
func (m *galleryModel) paintPreview() {
	r := m.previewRect()
	if r.w == 0 || r.h == 0 {
		return
	}
	src := cachedPNG(m.images[m.cursor].Path, r.w, r.h)
	if m.curImg != nil && !m.crop.isFull() {
		src = m.renderZoom(r.w, r.h)
	}
	paintSixelAt(m.tty, r, renderSixel(src, r.w, r.h))
}

// paintStrip paints each visible filmstrip thumbnail into the inner area of its
// cell rect (inset by 1 to clear the lipgloss border, which is drawn as text).
func (m *galleryModel) paintStrip() {
	start := stripStart(m.cursor, m.l.stripCols, len(m.images))
	for i, cell := range m.filmstripCellRects() {
		inner := rect{x: cell.x + 1, y: cell.y + 1, w: m.l.stripW, h: m.l.stripH}
		png := cachedPNG(m.images[start+i].Path, inner.w, inner.h)
		paintSixelAt(m.tty, inner, renderSixel(png, inner.w, inner.h))
	}
}

// paintRaster paints the whole view (preview + filmstrip) out-of-band on the tty.
// The raster analog of transmitView; called only from the debounced rasterPaintMsg
// handler, after bubbletea has flushed the blank-hole frame.
func (m *galleryModel) paintRaster() {
	if m.backend != backendRaster || m.tty == nil || len(m.images) == 0 {
		return
	}
	m.paintPreview()
	m.paintStrip()
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `direnv exec . go test -run TestPaintSixel ./...`
Expected: PASS (both `TestPaintSixelAt` and `TestPaintSixelAtEmpty`).

- [ ] **Step 5: Verify the package still builds**

Run: `direnv exec . go build ./...`
Expected: OK. (`paintRaster` is defined but not yet wired — that's Task 5.)

- [ ] **Step 6: Commit**

```bash
git add gallery_raster.go gallery_raster_test.go
git commit -m "feat(gallery): sixel paint primitives for backendRaster (#60)"
```

---

### Task 5: Wire the debounced post-render paint into `Update`

**Files:**
- Modify: `gallery.go:83-110` (add `rasterGen` field to `galleryModel`)
- Modify: `gallery_raster.go` (add `rasterPaintMsg`, `schedulePaint`)
- Modify: `gallery.go` `Update` (batch `schedulePaint` onto the relevant returns; add `rasterPaintMsg` and raster `settleMsg` handling)

There is no unit test for this wiring (it's bubbletea event-loop plumbing verified on-host in Task 6); the safety net is that the package builds and all existing tests stay green.

- [ ] **Step 1: Add the `rasterGen` field to `galleryModel`**

In `gallery.go`, the `galleryModel` struct has (line 102):

```go
	vecGen     uint64      // debounce generation: only the latest scheduled vector kick fires
```

Add directly below it:

```go
	rasterGen  uint64      // debounce generation for the post-render sixel repaint
```

- [ ] **Step 2: Add `rasterPaintMsg` and `schedulePaint` to `gallery_raster.go`**

Add `"charm.land/bubbletea/v2"` to the import block (aliased `tea`, matching the rest of the package):

```go
import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
)
```

Add:

```go
// rasterPaintMsg fires after the debounce window to repaint the sixel overlay.
// Only the latest generation survives, so a navigation burst paints once, on the
// final framing — mirroring the vectorKickMsg debounce.
type rasterPaintMsg struct{ gen uint64 }

// rasterPaintDebounce delays the repaint just past bubbletea's frame flush, so the
// sixel lands on top of the freshly-drawn blank holes rather than being clobbered
// by the renderer. Short enough not to feel laggy; tune on-host if needed.
const rasterPaintDebounce = 50 * time.Millisecond

// schedulePaint arms a debounced sixel repaint, bumping the generation so an
// earlier tick arrives stale and is dropped. Returns nil off the raster backend,
// so callers can batch it unconditionally.
func (m *galleryModel) schedulePaint() tea.Cmd {
	if m.backend != backendRaster {
		return nil
	}
	m.rasterGen++
	g := m.rasterGen
	return tea.Tick(rasterPaintDebounce, func(time.Time) tea.Msg { return rasterPaintMsg{gen: g} })
}
```

- [ ] **Step 3: Handle `rasterPaintMsg` and raster `settleMsg` in `Update`**

In `gallery.go` `Update`, the `settleMsg` case is currently (lines 332-333):

```go
	case settleMsg:
		return m, tea.ClearScreen
```

Replace with:

```go
	case settleMsg:
		if m.backend == backendRaster {
			// ClearScreen forces a full redraw of the blank holes; repaint the
			// sixel overlay just after, on top of them.
			return m, tea.Batch(tea.ClearScreen, m.schedulePaint())
		}
		return m, tea.ClearScreen
	case rasterPaintMsg:
		if msg.gen == m.rasterGen {
			m.paintRaster()
		}
		return m, nil
```

- [ ] **Step 4: Batch `schedulePaint` onto the key-handler return**

In `gallery.go` `Update`, the `KeyPressMsg` case ends with (lines 295-298):

```go
		// After any key, debounce-schedule a sharp d2 re-render off the event loop.
		// nil for non-d2/non-kitty, so this is a no-op there.
		cmd = m.scheduleVector()
		return m, cmd
```

Replace the return with:

```go
		// After any key, debounce-schedule a sharp d2 re-render off the event loop.
		// nil for non-d2/non-kitty, so this is a no-op there.
		cmd = m.scheduleVector()
		return m, tea.Batch(cmd, m.schedulePaint())
```

- [ ] **Step 5: Batch `schedulePaint` onto the mouse, resize, and reload returns**

In `gallery.go` `Update`:

In the `tea.MouseMsg` case (lines 299-302), change:

```go
	case tea.MouseMsg:
		var cmd tea.Cmd
		m, cmd = m.handleMouse(msg)
		return m, cmd
```
to:
```go
	case tea.MouseMsg:
		var cmd tea.Cmd
		m, cmd = m.handleMouse(msg)
		return m, tea.Batch(cmd, m.schedulePaint())
```

In the `tea.WindowSizeMsg` case, change the non-first-frame return (line 208) from:

```go
		return m, m.scheduleVector()
```
to:
```go
		return m, tea.Batch(m.scheduleVector(), m.schedulePaint())
```

(The first-frame path already returns `settleCmd()`, whose `settleMsg` now schedules the paint — no change there.)

In the `galleryTickMsg` case, change the reload return (line 329) from:

```go
			return m, tea.Batch(galleryTickCmd(), m.kickVector())
```
to:
```go
			return m, tea.Batch(galleryTickCmd(), m.kickVector(), m.schedulePaint())
```

- [ ] **Step 6: Build and run the full test suite**

Run: `direnv exec . go build ./... && direnv exec . go test ./...`
Expected: build OK, all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add gallery.go gallery_raster.go
git commit -m "feat(gallery): debounced post-render sixel repaint (#60)"
```

---

### Task 6: Full verification (suite + lint + manual host)

**Files:** none (verification only).

- [ ] **Step 1: Run the full Go suite and vet**

Run: `direnv exec . go test ./... && direnv exec . go vet ./...`
Expected: all PASS, no vet warnings.

- [ ] **Step 2: Run gofmt (pre-commit parity)**

Run: `direnv exec . gofmt -l gallery_raster.go gallery_render.go gallery.go gallery_test.go gallery_raster_test.go`
Expected: no output (all formatted).

- [ ] **Step 3: Manual host verification in wezterm (no tmux)**

Run: `nix develop .#verify` then, inside it, build and launch the viewer directly against a manifest with a few images (e.g. point `AEYE_DIR` at an existing `/tmp/claude-status` dir, or capture one screenshot first):

```bash
go build -o /tmp/aeye .
WEZTERM_PANE=0 /tmp/aeye <pane-key>
```

Confirm:
- Backend is raster (sixel), not chafa block-art — thumbnails are crisp real pixels.
- Navigate (`h`/`l`, `n`/`p`, `g`/`G`): the preview and filmstrip repaint to the new selection; no permanently-stale tiles when the strip window shifts near the ends.
- Zoom/pan (`z`/`Z`, arrows): the preview updates.
- Resize the wezterm pane: the carousel re-lays-out and the images repaint.
- Quit (`q`): the terminal is left clean (no leftover sixel).

If repaints feel laggy or flicker, adjust `rasterPaintDebounce` (Task 5, Step 2) and re-verify.

- [ ] **Step 4: Manual host verification in wezterm inside tmux**

Inside the `.#verify` shell, start tmux (the nixpkgs tmux is built with sixel support) and run the viewer in a split:

```bash
tmux new-session
# inside tmux:
go build -o /tmp/aeye . && /tmp/aeye <pane-key>
```

Confirm the probe detects sixel (crisp thumbnails, not chafa) and the overlay survives tmux redraws and navigation. If tmux lacks sixel, confirm it cleanly falls back to chafa block-art (no garbage).

- [ ] **Step 5: Sanity-check the chafa fallback is intact**

In a terminal with no sixel (e.g. `TERM=xterm-256color` with no `WEZTERM_PANE`, outside tmux), confirm the viewer still renders chafa block-art (`backendSymbols`) and is unchanged.

- [ ] **Step 6: Final commit (if any tuning was applied)**

```bash
git add -A
git commit -m "chore(gallery): tune sixel repaint debounce after host verification (#60)"
```

---

## Self-Review

**Spec coverage:**
- Sixel-only scope, env-first + probe-fallback selection → Task 2. ✓
- Why-not-probe-kitty (termname allowlist) → encoded in `chooseGridBackend` comment, Task 2. ✓
- DA1 sixel probe (raw mode, 150 ms, drain to `c`) → Task 1. ✓
- `$TMUX`-gated fast-path incl. leaked-`WEZTERM_PANE` case → Task 2 test. ✓
- Geometry reuse of `previewRect`/`filmstripCellRects`, filmstrip inset by 1 → Task 4. ✓
- Out-of-band raw paint to `/dev/tty`, save/restore cursor, 1-based coords → Task 4. ✓
- `chafa -f sixels` + strip cursor toggles → Task 4 `renderSixel`. ✓
- Blank-space holes in `View()` → Task 3. ✓
- Coexistence via post-render debounced repaint (mirrors `vecGen`) + `settleMsg`/ClearScreen handling → Task 5. ✓
- Host-independent tests (`parseSixelDA`, `chooseGridBackend`, `paintSixelAt`, `blankBlock`) → Tasks 1-4. ✓
- Manual wezterm verification (no-tmux, in-tmux, chafa fallback) → Task 6. ✓
- Edge cases (probe timeout, tiny panes via empty rects, chafa missing → empty payload no-op) → covered by guards in Tasks 1 & 4. ✓

**Type consistency:** `gridBackend`/`backendRaster`, `rect{x,y,w,h}`, `chooseGridBackend(string, bool, string, string, func() bool)`, `paintSixelAt(io.Writer, rect, string)`, `renderSixel(string, int, int) string`, `rasterPaintMsg{gen uint64}`, `schedulePaint() tea.Cmd`, `paintRaster()/paintPreview()/paintStrip()` are used consistently across tasks.

**Placeholder scan:** none — every step has concrete code or an exact command.
