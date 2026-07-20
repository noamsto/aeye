package main

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

func TestTmuxPassthrough(t *testing.T) {
	t.Setenv("TMUX", "")
	if got := tmuxPassthrough("\x1b_Ga=d\x1b\\"); got != "\x1b_Ga=d\x1b\\" {
		t.Errorf("no-tmux passthrough should be identity, got %q", got)
	}
	t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
	got := tmuxPassthrough("\x1b_Ga=d\x1b\\")
	want := "\x1bPtmux;\x1b\x1b_Ga=d\x1b\x1b\\\x1b\\"
	if got != want {
		t.Errorf("tmux passthrough = %q, want %q", got, want)
	}
}

func TestTransmitVirtual(t *testing.T) {
	t.Setenv("TMUX", "")
	// base64 of "/x.png" is "L3gucG5n". q=2 suppresses kitty's response.
	got := transmitVirtual(7, "/x.png", 20, 10)
	want := "\x1b_Gi=7,a=T,U=1,q=2,f=100,c=20,r=10,t=f;L3gucG5n\x1b\\"
	if got != want {
		t.Errorf("transmitVirtual = %q, want %q", got, want)
	}
}

func TestDeleteAll(t *testing.T) {
	t.Setenv("TMUX", "")
	if got := deleteAll(); got != "\x1b_Ga=d,d=A,q=2\x1b\\" {
		t.Errorf("deleteAll = %q", got)
	}
}

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

func TestPlaceholderBlock(t *testing.T) {
	// id=1 -> fg 0;0;1; 2 cols x 1 row. Cell = U+10EEEE + diacritic[row] + diacritic[col].
	got := placeholderBlock(1, 2, 1)
	want := "\x1b[38;2;0;0;1m" +
		"\U0010EEEE̅̅" + // row 0, col 0
		"\U0010EEEE̅̍" + // row 0, col 1
		"\x1b[39m"
	if got != want {
		t.Errorf("placeholderBlock(1,2,1) =\n%q\nwant\n%q", got, want)
	}
}

func TestPlaceholderBlockTwoRows(t *testing.T) {
	got := placeholderBlock(1, 1, 2)
	want := "\x1b[38;2;0;0;1m\U0010EEEE̅̅\x1b[39m\n" +
		"\x1b[38;2;0;0;1m\U0010EEEE̍̅\x1b[39m"
	if got != want {
		t.Errorf("placeholderBlock(1,1,2) =\n%q\nwant\n%q", got, want)
	}
}

func TestSymbolsArgs(t *testing.T) {
	got := symbolsArgs("/a/b.png", 20, 10)
	// No --clear: chafa's --clear wipes the whole screen, which would erase the
	// rest of the grid on every per-cell render.
	want := []string{"-f", "symbols", "--size", "20x10", "/a/b.png"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got[i], want[i])
		}
	}
}

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

func TestComputeLayout(t *testing.T) {
	l := computeLayout(120, 50)
	if l.previewW > maxCellDim || l.previewH > maxCellDim || l.stripW > maxCellDim || l.stripH > maxCellDim {
		t.Errorf("dims must clamp to %d: %+v", maxCellDim, l)
	}
	if l.previewW < 1 || l.previewH < 1 || l.stripCols < 1 {
		t.Errorf("dims must be >= 1: %+v", l)
	}
	// preview(+2 border) + filmstrip(stripH+2 border) + title + subtitle + hints
	// must fit the pane height (the full row budget computeLayout reserves).
	if l.previewH+2+l.stripH+2+3 > 50 {
		t.Errorf("rows overflow pane height: %+v", l)
	}
	// filmstrip thumbnails + gutters must fit the pane width.
	if l.stripCols*l.stripW+(l.stripCols-1)*stripGutter > 120 {
		t.Errorf("filmstrip overflows pane width: %+v", l)
	}
}

func TestComputeLayoutTiny(t *testing.T) {
	l := computeLayout(10, 6) // degenerate pane
	if l.previewW < 1 || l.previewH < 1 || l.stripW < 1 || l.stripH < 1 || l.stripCols < 1 {
		t.Errorf("tiny pane must still yield a valid layout: %+v", l)
	}
}

func TestStripStart(t *testing.T) {
	// All fit -> window starts at 0.
	if s := stripStart(3, 8, 5); s != 0 {
		t.Errorf("all-fit start = %d, want 0", s)
	}
	// More than fit -> window centers on cursor, clamped.
	if s := stripStart(0, 4, 20); s != 0 {
		t.Errorf("start at head = %d, want 0", s)
	}
	if s := stripStart(19, 4, 20); s != 16 {
		t.Errorf("start at tail = %d, want 16 (n-stripCols)", s)
	}
	if s := stripStart(10, 4, 20); s != 8 {
		t.Errorf("centered start = %d, want 8 (cursor-stripCols/2)", s)
	}
}

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

func TestParseManifest(t *testing.T) {
	data := []byte(`{"type":"image","path":"/a/one.png","source":"Read","ts":"t","mtime":1}

  {"type":"image","path":"/b/two.png","source":"Write","ts":"t","mtime":2}
not json
{"type":"image","path":"","source":"Read"}
{"type":"image","path":"/c/three.png","source":"Screenshot"}
`)
	got := parseManifest(data)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (blank/corrupt/empty-path skipped): %+v", len(got), got)
	}
	if got[0].Path != "/a/one.png" || got[0].Source != "Read" {
		t.Errorf("entry 0 = %+v", got[0])
	}
	if got[2].Path != "/c/three.png" {
		t.Errorf("entry 2 = %+v", got[2])
	}
}

func TestParseManifestDedupes(t *testing.T) {
	data := []byte(`{"type":"image","path":"/a/one.png","source":"Read","mtime":1}
{"type":"image","path":"/a/one.png","source":"Read","mtime":1}
{"type":"image","path":"/a/one.png","source":"d2","mtime":2}
{"type":"image","path":"/b/two.png","source":"Write","mtime":5}
`)
	got := parseManifest(data)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (duplicate path+mtime collapsed): %+v", len(got), got)
	}
	// Same path with a new mtime is a distinct re-capture, kept.
	if got[0].Mtime != 1 || got[1].Mtime != 2 || got[1].Path != "/a/one.png" || got[2].Path != "/b/two.png" {
		t.Errorf("re-captured entry not kept distinct: %+v", got)
	}
}

func TestLoadManifestDropsUndecodableFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AEYE_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(dir, "live.png")
	writeTestImage(t, live, 4, 4)
	// A 3-byte "PNG" stub: exists on disk but doesn't decode (a failed render).
	stub := filepath.Join(dir, "stub.png")
	if err := os.WriteFile(stub, []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "images", "p1.jsonl")
	lines := `{"type":"image","path":"` + live + `","source":"Read","mtime":1}
{"type":"image","path":"` + stub + `","source":"d2","mtime":2}
{"type":"image","path":"` + filepath.Join(dir, "gone.png") + `","source":"Write","mtime":3}
`
	if err := os.WriteFile(manifest, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadManifest("p1", "dark")
	if len(got) != 1 || got[0].Path != live {
		t.Fatalf("loadManifest = %+v, want only the decodable file %q", got, live)
	}

	log, err := os.ReadFile(filepath.Join(dir, "images", "dropped.log"))
	if err != nil {
		t.Fatalf("dropped.log not written: %v", err)
	}
	for _, want := range []string{stub, filepath.Join(dir, "gone.png")} {
		if !strings.Contains(string(log), want) {
			t.Errorf("dropped.log missing %q:\n%s", want, log)
		}
	}
}

func TestParseManifestVectorField(t *testing.T) {
	got := parseManifest([]byte(`{"type":"image","path":"/d/a.png","vector":"/d/a.svg","source":"d2"}`))
	if len(got) != 1 || got[0].Vector != "/d/a.svg" {
		t.Fatalf("vector not parsed: %+v", got)
	}
	// absence is fine (backward compatible).
	got = parseManifest([]byte(`{"type":"image","path":"/d/b.png","source":"Read"}`))
	if len(got) != 1 || got[0].Vector != "" {
		t.Fatalf("missing vector should be empty: %+v", got)
	}
}

// TestSettleMsgReTransmits guards the first-paint fix (#61): on a freshly
// spawned window the initial store lands before bubbletea switches to the
// alt-screen, so the settle repaint must RE-store, not just ClearScreen —
// otherwise the carousel stays blank until the user interacts.
func TestSettleMsgReTransmits(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	got := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(r); got <- b }()

	m := galleryModel{
		backend: backendKitty,
		tty:     w,
		images:  []imageEntry{{Path: writeTestPNG(t)}},
		ready:   true,
		width:   100,
		height:  40,
	}
	m.l = computeLayout(m.width, m.height)

	m.Update(settleMsg{})
	w.Close()

	// deleteAll() is transmitView's first write; its absence means the settle
	// handler skipped the re-store.
	if out := <-got; !bytes.Contains(out, []byte("\x1b_Ga=d,d=A")) {
		t.Fatalf("settleMsg did not re-transmit the image store to the tty; got %q", out)
	}
}

func writeTestPNG(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCaption(t *testing.T) {
	// A diagram's name (its .d2 source) beats the content-hash png basename.
	named := imageEntry{Path: "/d/1495b42c989a23e6.png", Name: "raster-backend"}
	if got := named.caption(); got != "raster-backend" {
		t.Fatalf("named caption = %q, want raster-backend", got)
	}
	// No name (a screenshot) falls back to the file basename.
	plain := imageEntry{Path: "/shots/login.png"}
	if got := plain.caption(); got != "login.png" {
		t.Fatalf("unnamed caption = %q, want login.png", got)
	}
}

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

func TestMarkPendingCommitsPrior(t *testing.T) {
	a := writeTestPNG(t)
	b := writeTestPNG(t)
	m := &galleryModel{images: []imageEntry{{Path: a, Source: "Read"}, {Path: b, Source: "Read"}}}

	m.cursor = 0
	m.markPending() // mark A
	m.cursor = 1
	m.markPending() // marking B commits A first

	if m.pending == nil || m.pending.path != b {
		t.Fatalf("pending should now be B (%q): %+v", b, m.pending)
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatalf("marking B did not commit (remove) A's file (err=%v)", err)
	}
	if _, err := os.Stat(b); err != nil {
		t.Fatalf("B's file should still be on disk during its window: %v", err)
	}
}

func TestMarkPendingSameEntryTwiceKeepsWindow(t *testing.T) {
	f := writeTestPNG(t)
	m := &galleryModel{images: []imageEntry{{Path: f, Source: "Read"}}}

	m.markPending() // arm the undo window
	gen := m.delGen

	// A stray second x on the same entry must not commit early: the file stays
	// on disk, pending is unchanged, and the generation doesn't advance.
	m.markPending()
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("second x deleted the file before its undo window elapsed: %v", err)
	}
	if m.pending == nil || m.pending.path != f {
		t.Fatalf("second x cleared or re-marked pending: %+v", m.pending)
	}
	if m.delGen != gen {
		t.Fatalf("second x bumped delGen (%d -> %d), invalidating the armed ticks", gen, m.delGen)
	}
}

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

func TestIsPending(t *testing.T) {
	d2 := &galleryModel{pending: &pendingDeletion{path: "/d/h-dark.png"}}
	if !d2.isPending(imageEntry{Path: "/d/h-light.png", Source: "d2"}) {
		t.Fatalf("isPending should match a d2 entry's -light variant against a -dark pending path")
	}
	if d2.isPending(imageEntry{Path: "/d/other-light.png", Source: "d2"}) {
		t.Fatalf("isPending should not match a different hash")
	}

	// Non-d2 paths are matched exactly, never theme-normalized.
	plain := &galleryModel{pending: &pendingDeletion{path: "/shots/login.png"}}
	if !plain.isPending(imageEntry{Path: "/shots/login.png", Source: "Read"}) {
		t.Fatalf("isPending should match an identical non-d2 path")
	}
	if plain.isPending(imageEntry{Path: "/shots/other.png", Source: "Read"}) {
		t.Fatalf("isPending should not match a different non-d2 path")
	}

	// Regression: a non-d2 pending must not be conflated with its -light/-dark
	// namesake sibling merely because the path shapes match withTheme's rewrite.
	icon := &galleryModel{pending: &pendingDeletion{path: "/assets/icon-dark.png"}}
	if icon.isPending(imageEntry{Path: "/assets/icon-light.png", Source: "Read"}) {
		t.Fatalf("isPending should not conflate a non-d2 entry with its -light/-dark namesake sibling")
	}

	if (&galleryModel{}).isPending(imageEntry{Path: "/shots/login.png", Source: "Read"}) {
		t.Fatalf("isPending with no pending deletion should never match")
	}
}

// TestPendingSurvivesThemeSwitch guards against a pending d2 deletion silently
// cancelling on a live theme flip: pending.path is captured already
// theme-resolved (e.g. "-dark"), and reload() re-resolves every d2 entry to the
// new theme's variant, so a byte-exact path match would find no survivor and
// wrongly clear m.pending.
func TestPendingSurvivesThemeSwitch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AEYE_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0o755); err != nil {
		t.Fatal(err)
	}

	darkPNG := filepath.Join(dir, "h-dark.png")
	lightPNG := filepath.Join(dir, "h-light.png")
	writeTestImage(t, darkPNG, 4, 4)
	writeTestImage(t, lightPNG, 4, 4)
	darkSVG := filepath.Join(dir, "h-dark.svg")
	lightSVG := filepath.Join(dir, "h-light.svg")
	for _, p := range []string{darkSVG, lightSVG} {
		if err := os.WriteFile(p, []byte("<svg/>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	manifest := filepath.Join(dir, "images", "p1.jsonl")
	line := `{"type":"image","path":"` + darkPNG + `","vector":"` + darkSVG + `","source":"d2","name":"diagram","mtime":1}` + "\n"
	if err := os.WriteFile(manifest, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &galleryModel{pane: "p1", theme: "dark"}
	m.reload()
	if len(m.images) != 1 || m.images[0].Path != darkPNG {
		t.Fatalf("reload (dark) = %+v, want the dark-resolved d2 entry", m.images)
	}
	m.cursor = 0
	m.markPending()
	if m.pending == nil || m.pending.path != darkPNG {
		t.Fatalf("markPending did not set pending for %q: %+v", darkPNG, m.pending)
	}

	// Live theme switch, mirroring the galleryTickMsg handler's reload() call.
	m.theme = "light"
	m.reload()

	if m.pending == nil {
		t.Fatalf("pending was cleared across a theme switch (d2 path resolution changed underneath it)")
	}
	if len(m.images) != 1 || m.images[0].Path != lightPNG {
		t.Fatalf("reload (light) = %+v, want the light-resolved d2 entry", m.images)
	}
	if !m.isPending(m.images[0]) {
		t.Fatalf("isPending does not match the re-resolved light path %q against pending %q",
			m.images[0].Path, m.pending.path)
	}
}

func TestTruncateToWidthRuneSafe(t *testing.T) {
	s := "✗ Deleting café  ▓▓░░░ 2s"
	for w := 1; w < len(s); w++ {
		got := truncateToWidth(s, w)
		if !utf8.ValidString(got) {
			t.Fatalf("truncateToWidth(%q, %d) = %q, contains a partial rune", s, w, got)
		}
		if lipgloss.Width(got) > w {
			t.Fatalf("truncateToWidth(%q, %d) = %q, display width %d > %d", s, w, got, lipgloss.Width(got), w)
		}
	}
}
