# Drag the Selected Image Out — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user drag the selected image out of aeye into another app, via a capability-chosen ladder (native kitty OSC 72 → `ripdrag`/`dragon` GUI helper → clipboard + hint).

**Architecture:** A new `dragout.go` holds tier selection and the two active mechanisms, mirroring the existing `clipboard.go`/`probeSixel` patterns. A new `d` keybinding calls `m.dragSelected()`, which walks the ladder. Native OSC 72 (the risky, interactive tier) is gated behind a spike and layered on last — Stage 1 ships working drag-out on Linux + graceful degradation everywhere.

**Tech Stack:** Go, bubbletea v2, kitty OSC 72 DnD protocol, `ripdrag`/`dragon` (external GTK helpers).

**Spec:** `docs/superpowers/specs/2026-06-21-drag-image-out-design.md` · **Issue:** [#86](https://github.com/noamsto/aeye/issues/86)

---

## File Structure

- **Create `dragout.go`** — `dragHelper()`, `runDragHelper()`, and (Stage 3) `parseDragDA()`, `probeDragProtocol()`, native OSC 72 sequence builders.
- **Create `dragout_test.go`** — hermetic table tests for the pure functions.
- **Modify `gallery.go`** — add `dragSelected()` method (next to `copySelected`, ~line 385), add the `d` case to the `KeyPressMsg` switch (~line 296), and (Stage 3) a `dragNative bool` model field + startup probe.
- **Modify `README.md`** — document the `d` key and the optional `ripdrag`/`dragon` dependency.

## Testing Convention

`dragHelper` calls the package-level `lookPath` (like `clipboardTool` does), so tests stay hermetic by pointing `PATH` at a temp dir holding fake executables — no production refactor, real `lookPath`, deterministic preference order. Run the suite inside the worktree's devshell: `direnv exec . go test -count=1 ./...`.

---

# Stage 1 — GUI helper + clipboard fallback (ships independently)

### Task 1: `dragHelper` — pick the GUI drag-source program

**Files:**
- Create: `dragout.go`
- Test: `dragout_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeBins makes a temp dir on PATH containing empty executable files for each
// given name, so lookPath finds them without running anything real.
func fakeBins(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestDragHelper(t *testing.T) {
	t.Run("prefers ripdrag when both present", func(t *testing.T) {
		fakeBins(t, "ripdrag", "dragon")
		name, args := dragHelper()
		if name != "ripdrag" || len(args) != 0 {
			t.Fatalf("got (%q, %v), want ripdrag with no leading args", name, args)
		}
	})
	t.Run("falls back to dragon with -x", func(t *testing.T) {
		fakeBins(t, "dragon")
		name, args := dragHelper()
		if name != "dragon" || len(args) != 1 || args[0] != "-x" {
			t.Fatalf("got (%q, %v), want dragon -x", name, args)
		}
	})
	t.Run("empty when neither present", func(t *testing.T) {
		fakeBins(t)
		if name, _ := dragHelper(); name != "" {
			t.Fatalf("got %q, want empty", name)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `direnv exec . go test -run TestDragHelper -count=1 .`
Expected: FAIL — `undefined: dragHelper`.

- [ ] **Step 3: Write minimal implementation**

```go
package main

import "os/exec"

// dragHelper picks an external GUI drag-source program and the args that precede
// the file path, or an empty name when none is on PATH. ripdrag and dragon are
// Linux-only GTK tools that pop a small window the user drags out of; prefer
// ripdrag (actively maintained), then dragon.
func dragHelper() (string, []string) {
	if _, ok := lookPath("ripdrag"); ok {
		return "ripdrag", nil
	}
	if _, ok := lookPath("dragon"); ok {
		return "dragon", []string{"-x"} // -x: exit after one drop
	}
	return "", nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `direnv exec . go test -run TestDragHelper -count=1 .`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add dragout.go dragout_test.go
git commit -m "feat: dragHelper picks ripdrag/dragon for drag-out (#86)"
```

---

### Task 2: `runDragHelper` — launch the helper detached

**Files:**
- Modify: `dragout.go`
- Test: `dragout_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRunDragHelper(t *testing.T) {
	t.Run("returns name and launches when present", func(t *testing.T) {
		fakeBins(t, "ripdrag") // the fake is a no-op #!/bin/sh that exits immediately
		if name := runDragHelper("/x/a.png"); name != "ripdrag" {
			t.Fatalf("got %q, want ripdrag", name)
		}
	})
	t.Run("empty when no helper installed", func(t *testing.T) {
		fakeBins(t)
		if name := runDragHelper("/x/a.png"); name != "" {
			t.Fatalf("got %q, want empty", name)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `direnv exec . go test -run TestRunDragHelper -count=1 .`
Expected: FAIL — `undefined: runDragHelper`.

- [ ] **Step 3: Write minimal implementation** (append to `dragout.go`)

```go
// runDragHelper launches the GUI drag-source for path, detached so it doesn't
// block the TUI (the helper holds its window open until the user drops). Returns
// the helper's name for the status line, or "" when none is installed. Mirrors
// openSelected's detached exec.Command(...).Start().
func runDragHelper(path string) string {
	name, args := dragHelper()
	if name == "" {
		return ""
	}
	_ = exec.Command(name, append(args, path)...).Start()
	return name
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `direnv exec . go test -run TestRunDragHelper -count=1 .`
Expected: PASS (the fake `ripdrag` is a no-op script; `.Start()` launches and returns immediately).

- [ ] **Step 5: Commit**

```bash
git add dragout.go dragout_test.go
git commit -m "feat: runDragHelper launches drag-source detached (#86)"
```

---

### Task 3: `dragSelected` — the ladder (helper → clipboard + hint)

**Files:**
- Modify: `gallery.go` (add method after `copySelected`, ~line 394)
- Test: `dragout_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestDragSelected(t *testing.T) {
	t.Run("uses helper when present", func(t *testing.T) {
		fakeBins(t, "ripdrag")
		m := &galleryModel{images: []imageEntry{{Path: "/x/a.png"}}}
		m.dragSelected()
		if m.status != "Opened drag window (ripdrag)" {
			t.Fatalf("status = %q", m.status)
		}
	})
	t.Run("falls back to clipboard with hint when no helper", func(t *testing.T) {
		fakeBins(t) // no helper, and no clipboard tool on this PATH either
		m := &galleryModel{images: []imageEntry{{Path: "/x/a.png"}}}
		m.dragSelected()
		if !strings.Contains(m.status, "ripdrag/dragon") {
			t.Fatalf("status missing drag-out hint: %q", m.status)
		}
	})
	t.Run("no images is a no-op", func(t *testing.T) {
		fakeBins(t, "ripdrag")
		m := &galleryModel{}
		m.dragSelected()
		if m.status != "" {
			t.Fatalf("status = %q, want empty", m.status)
		}
	})
}
```

Add `"strings"` to the `dragout_test.go` import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `direnv exec . go test -run TestDragSelected -count=1 .`
Expected: FAIL — `m.dragSelected undefined`.

- [ ] **Step 3: Write minimal implementation** (in `gallery.go`, right after `copySelected`)

```go
// dragSelected hands the selected image to an external GUI drag-source so it can
// be dropped into another app, falling back to the clipboard with a hint when no
// helper is installed. Native OSC 72 drag-out is prepended to this ladder in a
// later stage. Records a one-line result in m.status for the footer.
func (m *galleryModel) dragSelected() {
	if len(m.images) == 0 {
		return
	}
	if name := runDragHelper(m.images[m.cursor].Path); name != "" {
		m.status = "Opened drag window (" + name + ")"
		return
	}
	m.copySelected()
	m.status += " (drag-out needs kitty or ripdrag/dragon)"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `direnv exec . go test -run TestDragSelected -count=1 .`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add gallery.go dragout_test.go
git commit -m "feat: dragSelected ladder — helper then clipboard fallback (#86)"
```

---

### Task 4: Wire the `d` keybinding

**Files:**
- Modify: `gallery.go` (the `tea.KeyPressMsg` switch, ~line 294-297)

- [ ] **Step 1: Add the case** (immediately after the `"y"` case)

```go
		case "y":
			m.copySelected()
		case "d":
			m.dragSelected()
		case "r":
			m.reload()
```

- [ ] **Step 2: Build to verify it compiles**

Run: `direnv exec . go build ./...`
Expected: no output (success).

- [ ] **Step 3: Run the full suite**

Run: `direnv exec . go test -count=1 ./...`
Expected: PASS (gopls diagnostics in the worktree may show false errors — trust `go build`/`go test -count=1` run inside the worktree, per project memory).

- [ ] **Step 4: Manual smoke test** (Linux + a GUI session)

Run aeye on a manifest with at least one image, install `ripdrag` (or `dragon`), select an image, press `d`. Expected: a small drag window appears holding the image; status shows `Opened drag window (ripdrag)`. With no helper installed: status shows the clipboard result + `(drag-out needs kitty or ripdrag/dragon)`.

- [ ] **Step 5: Commit**

```bash
git add gallery.go
git commit -m "feat: bind d to drag the selected image out (#86)"
```

---

### Task 5: Document the keybinding

**Files:**
- Modify: `README.md` (keybindings section + features/dependencies)

- [ ] **Step 1: Add `d` to the keybinding docs**

Locate the keybinding list (search README for the `y` / `o` key docs) and add a row/line:

```
`d` — drag the selected image out (native drag on kitty ≥ 0.47; otherwise opens a `ripdrag`/`dragon` window on Linux; else copies to clipboard).
```

- [ ] **Step 2: Note the optional dependency**

Near the existing optional-tools note (where `chafa`/clipboard tools are mentioned), add: drag-out's helper fallback needs `ripdrag` or `dragon` on PATH (Linux/X11/Wayland only).

- [ ] **Step 3: Verify typos pass** (the pre-commit hook runs `typos`)

Run: `direnv exec . typos README.md`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document the d drag-out keybinding (#86)"
```

**End of Stage 1 — open a PR here.** Working drag-out on Linux, graceful hint elsewhere, no dependency on the native tier.

---

# Stage 2 — Native OSC 72 spike (timeboxed investigation, no production code)

### Task 6: Prove inbound OSC 72 capture under bubbletea v2

**Goal:** decide whether the native tier is buildable before investing in it. Not TDD — a throwaway prove-out against a live kitty ≥ 0.47.

- [ ] **Step 1: Confirm the terminal** — `kitty --version` ≥ 0.47, running outside tmux.

- [ ] **Step 2: Probe by hand** — from a scratch Go `main`, open `/dev/tty`, `term.MakeRaw`, write `\x1b]72;t=q\x1b\\\x1b[c`, read raw bytes, and confirm an `\x1b]72;...` reply arrives before the DA1 `c`. This validates `probeDragProtocol` before Task 7 codifies it.

- [ ] **Step 3: The critical question — inbound events in bubbletea v2.** In a minimal bubbletea v2 program, send the arm sequence (`\x1b]72;t=o:x=1\x1b\\`), perform a mouse drag, and log every `tea.Msg`. Determine whether the terminal's OSC 72 events surface as a matchable message (e.g. an unknown-sequence / raw-input msg on ultraviolet) or are swallowed.

- [ ] **Step 4: Confirm ordering & arming semantics** — whether `t=o:x=1` arming disrupts normal `tea.MouseMsg` capture, and the exact data/gesture/`t=P` ordering that produces a successful drop into a file manager.

- [ ] **Step 5: Decision gate.**
  - **Spike succeeds** → proceed to Stage 3.
  - **Spike fails** (v2 swallows inbound OSC 72) → stop. File a follow-up issue referencing #86, note it in the spec's staging section, and ship Stage 1 as the final state. Do not build Task 7+.

No commit (throwaway code is discarded; record findings in the issue).

---

# Stage 3 — Native OSC 72 tier (GATED on Task 6 succeeding)

> Build only if the spike confirmed inbound capture. Sequence builders below follow the [protocol](https://sw.kovidgoyal.net/kitty/dnd-protocol/); adjust to the exact ordering the spike confirmed.

### Task 7: `parseDragDA` — the capability race

**Files:**
- Modify: `dragout.go`
- Test: `dragout_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestParseDragDA(t *testing.T) {
	for _, c := range []struct {
		name string
		resp string
		want bool
	}{
		{"osc72 before DA1", "\x1b]72;t=q\x1b\\\x1b[?62;4c", true},
		{"DA1 only", "\x1b[?62;4c", false},
		{"DA1 before osc72", "\x1b[?62c\x1b]72;t=q\x1b\\", false},
		{"garbage", "hello", false},
		{"empty", "", false},
	} {
		if got := parseDragDA(c.resp); got != c.want {
			t.Errorf("%s: parseDragDA(%q) = %v, want %v", c.name, c.resp, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `direnv exec . go test -run TestParseDragDA -count=1 .`
Expected: FAIL — `undefined: parseDragDA`.

- [ ] **Step 3: Write minimal implementation** (append to `dragout.go`, add `"strings"` import)

```go
// parseDragDA reports whether the terminal supports the OSC 72 DnD protocol,
// given the raw reply to a "\x1b]72;t=q" query followed by a DA1 request: support
// means an OSC 72 reply ("\x1b]72;") appears before the DA1 terminator 'c'. A
// terminal without DnD answers only DA1, so 'c' comes first (or there's no reply).
func parseDragDA(resp string) bool {
	osc := strings.Index(resp, "\x1b]72;")
	if osc < 0 {
		return false
	}
	c := strings.IndexByte(resp, 'c')
	return c < 0 || osc < c
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `direnv exec . go test -run TestParseDragDA -count=1 .`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add dragout.go dragout_test.go
git commit -m "feat: parseDragDA detects OSC 72 support from the probe reply (#86)"
```

---

### Task 8: `probeDragProtocol` + `dragNative` startup wiring

**Files:**
- Modify: `dragout.go` (add `probeDragProtocol`)
- Modify: `gallery.go` (add `dragNative bool` field ~line 99; set it in `runGallery` ~line 561)

- [ ] **Step 1: Add the probe** (append to `dragout.go`; imports `os`, `time`, `github.com/charmbracelet/x/term`)

Model it on `probeSixel` (gallery_raster.go): open `/dev/tty`, `term.MakeRaw`, write the query + DA1, read to `c` in a goroutine with a 150ms deadline, and pass the drained reply to `parseDragDA`. Reuse `probeSixel`'s exact read loop, changing only the written bytes and the parser:

```go
// probeDragProtocol reports whether the terminal supports the OSC 72 drag-and-drop
// protocol, using the same raw-mode /dev/tty handshake as probeSixel. Only kitty
// implements OSC 72 today, so callers gate this on a kitty $TERM to avoid touching
// the tty on terminals that cannot support it — the query still confirms the
// running version actually answers.
func probeDragProtocol() bool {
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

	if _, err := tty.WriteString("\x1b]72;t=q\x1b\\\x1b[c"); err != nil {
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
		return parseDragDA(s)
	case <-time.After(150 * time.Millisecond):
		return false
	}
}
```

- [ ] **Step 2: Add the model field** — in the `galleryModel` struct (`gallery.go` ~line 99), after `regions`:

```go
	dragNative bool // terminal supports OSC 72 drag-out (probed once at startup)
```

- [ ] **Step 3: Set it at startup** — in `runGallery` (`gallery.go` ~line 561), gate the probe on a kitty `$TERM`:

```go
		crop:    fullCrop(),
		dragNative: strings.HasPrefix(termName(), "xterm-kitty") && probeDragProtocol(),
```

(Confirm `strings` is already imported in `gallery.go`; it is used by `chooseGridBackend`'s neighbors — if not, add it.)

- [ ] **Step 4: Build + test**

Run: `direnv exec . go build ./... && direnv exec . go test -count=1 ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add dragout.go gallery.go
git commit -m "feat: probe OSC 72 support at startup, cache as dragNative (#86)"
```

---

### Task 9: Native sequence builders

**Files:**
- Modify: `dragout.go`
- Test: `dragout_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestDragArmSequence(t *testing.T) {
	if got := dragArmSequence(); got != "\x1b]72;t=o:x=1\x1b\\" {
		t.Fatalf("dragArmSequence() = %q", got)
	}
}

func TestDragURIPayload(t *testing.T) {
	// text/uri-list payload is the percent-encoded file:// URI, base64-encoded.
	got := dragURIPayload("/tmp/a b.png")
	want := base64.StdEncoding.EncodeToString([]byte("file:///tmp/a%20b.png\r\n"))
	if got != want {
		t.Fatalf("dragURIPayload = %q, want %q", got, want)
	}
}
```

Add `"encoding/base64"` to the test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `direnv exec . go test -run 'TestDragArmSequence|TestDragURIPayload' -count=1 .`
Expected: FAIL — `undefined: dragArmSequence`.

- [ ] **Step 3: Write minimal implementation** (append to `dragout.go`; imports `encoding/base64`, `net/url`, `path/filepath`)

```go
// dragArmSequence is the OSC 72 escape that declares an outgoing drag; the
// terminal then reports the user's drag gesture back to us.
func dragArmSequence() string { return "\x1b]72;t=o:x=1\x1b\\" }

// dragURIPayload is the base64-encoded text/uri-list body for a single local
// file: a percent-encoded file:// URI terminated by CRLF, as drop targets expect.
func dragURIPayload(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	uri := (&url.URL{Scheme: "file", Path: abs}).String() + "\r\n"
	return base64.StdEncoding.EncodeToString([]byte(uri))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `direnv exec . go test -run 'TestDragArmSequence|TestDragURIPayload' -count=1 .`
Expected: PASS. (If the `url.URL` encoding of the space differs from `%20`, adjust the expectation to match Go's encoding — verify the actual output and lock the test to it.)

- [ ] **Step 5: Commit**

```bash
git add dragout.go dragout_test.go
git commit -m "feat: native OSC 72 drag sequence + uri-list payload builders (#86)"
```

---

### Task 10: Prepend the native tier to `dragSelected` + inbound handling

**Files:**
- Modify: `gallery.go` (`dragSelected`, plus a `dragEventMsg` case in `Update` per the spike's findings)
- Test: `dragout_test.go`

- [ ] **Step 1: Extend the ladder test**

```go
func TestDragSelectedNativeFirst(t *testing.T) {
	fakeBins(t, "ripdrag") // helper present, but native must win
	m := &galleryModel{images: []imageEntry{{Path: "/x/a.png"}}, dragNative: true, tty: nil}
	m.dragSelected()
	if !strings.Contains(m.status, "drag with the mouse") {
		t.Fatalf("native tier not taken: status = %q", m.status)
	}
}
```

(Writing to a nil `tty` must be guarded — see Step 3 — so the test exercises the branch without a real terminal.)

- [ ] **Step 2: Run test to verify it fails**

Run: `direnv exec . go test -run TestDragSelectedNativeFirst -count=1 .`
Expected: FAIL — native branch not present, status is the helper string.

- [ ] **Step 3: Prepend the native branch** (top of `dragSelected`, before the helper check)

```go
	if m.dragNative {
		if m.tty != nil {
			_, _ = m.tty.WriteString(tmuxPassthrough(dragArmSequence()))
		}
		m.status = "Drag armed — drag with the mouse"
		return
	}
```

Then wire the inbound `dragEventMsg` in `Update` exactly as the Task 6 spike established (respond with the `text/uri-list` offer, send `dragURIPayload(path)`, then the `t=P` initiate sequence). Build the offer/data/initiate writes as small helpers next to `dragArmSequence` so they stay testable.

- [ ] **Step 4: Build + full suite + manual drop test**

Run: `direnv exec . go build ./... && direnv exec . go test -count=1 ./...`
Then on live kitty ≥ 0.47: select an image, press `d`, drag into a file manager, confirm the file drops.
Expected: PASS + a successful drop.

- [ ] **Step 5: Commit**

```bash
git add gallery.go dragout.go dragout_test.go
git commit -m "feat: native OSC 72 drag-out tier, ahead of the helper fallback (#86)"
```

---

## Self-Review (completed during authoring)

- **Spec coverage:** ladder (Tasks 3, 10), helper tier (Tasks 1–2), clipboard fallback + hint (Task 3), capability detection (Tasks 7–8), `d` keybinding (Task 4), tests for pure fns (1, 3, 7, 9), README (Task 5), staging (Stages 1–3), out-of-scope respected (no drop-in, no multi-select, no macOS pbcopy). ✓
- **Type consistency:** `dragHelper() (string,[]string)`, `runDragHelper(string) string`, `dragSelected()`, `parseDragDA(string) bool`, `probeDragProtocol() bool`, `dragArmSequence() string`, `dragURIPayload(string) string`, `dragNative bool` — names identical across all tasks. ✓
- **Placeholder scan:** none. Stage 3 carries real protocol code; its open variable (exact inbound wiring) is explicitly resolved by the Task 6 spike, not deferred as a placeholder. ✓
- **Risk handling:** native tier gated on Task 6 with an explicit stop-and-ship-Stage-1 branch. ✓
