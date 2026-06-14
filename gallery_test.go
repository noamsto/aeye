package main

import (
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
	cases := []struct {
		term string
		want gridBackend
	}{
		{"xterm-kitty", backendKitty},
		{"xterm-ghostty", backendKitty},
		{"xterm-kitty-something", backendKitty},
		{"foot", backendSymbols},
		{"xterm-256color", backendSymbols},
		{"", backendSymbols},
	}
	for _, c := range cases {
		if got := chooseGridBackend(c.term); got != c.want {
			t.Errorf("chooseGridBackend(%q) = %v, want %v", c.term, got, c.want)
		}
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

func TestLoadManifestDropsUndecodableFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_CAROUSEL_DIR", dir)
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
	got := loadManifest("p1")
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
