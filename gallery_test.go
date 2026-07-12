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
