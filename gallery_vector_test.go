package main

import (
	"bytes"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// graphvizSVG is shaped like Graphviz output: the root carries width and height
// in pt, and a newline separates them from viewBox.
const graphvizSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="532pt" height="548pt"
 viewBox="0.00 0.00 532.00 548.00">
<rect x="0" y="0" width="532" height="548" fill="#ffffff"/>
<rect x="0" y="0" width="532" height="60" fill="#ff0000"/>
<rect x="0" y="488" width="532" height="60" fill="#0000ff"/>
</svg>
`

// TestCropViewBoxDropsRootSize: a cropped root must not keep its fixed size, or
// resvg sizes the canvas from it and fits the crop inside (letterboxing).
func TestCropViewBoxDropsRootSize(t *testing.T) {
	out, ok := cropViewBox([]byte(graphvizSVG), cropFrac{0.25, 0.40, 0.75, 0.60})
	if !ok {
		t.Fatal("expected crop applied")
	}
	want := "<svg xmlns=\"http://www.w3.org/2000/svg\"\n viewBox=\"133.0000 219.2000 266.0000 109.6000\">"
	if !bytes.Contains(out, []byte(want)) {
		t.Errorf("root size not dropped / viewBox not rewritten:\n%s", out)
	}
}

// TestCropViewBoxKeepsNestedSize: only the root tag loses its size; a nested
// <svg> (D2 nests one) keeps its width and height.
func TestCropViewBoxKeepsNestedSize(t *testing.T) {
	svg := []byte(`<svg width="10" viewBox="0 0 100 100"><svg width="40" height="50" viewBox="0 0 1 1"></svg></svg>`)
	out, ok := cropViewBox(svg, cropFrac{0, 0, 0.5, 0.5})
	if !ok {
		t.Fatal("expected crop applied")
	}
	want := `<svg viewBox="0.0000 0.0000 50.0000 50.0000"><svg width="40" height="50" viewBox="0 0 1 1"></svg></svg>`
	if string(out) != want {
		t.Errorf("got %s\nwant %s", out, want)
	}
}

// TestCropViewBoxKeepsSimilarAttrs: whole-name matching only; stroke-width and
// data-width on the root are not size attributes and must survive.
func TestCropViewBoxKeepsSimilarAttrs(t *testing.T) {
	svg := []byte(`<svg width="10" stroke-width="2" data-width="3" height="20" viewBox="0 0 100 100"></svg>`)
	out, ok := cropViewBox(svg, cropFrac{0, 0, 0.5, 0.5})
	if !ok {
		t.Fatal("expected crop applied")
	}
	want := `<svg stroke-width="2" data-width="3" viewBox="0.0000 0.0000 50.0000 50.0000"></svg>`
	if string(out) != want {
		t.Errorf("got %s\nwant %s", out, want)
	}
}

// TestCropViewBoxD2Unchanged: a root without size attributes (D2) is rewritten
// exactly as before: only the viewBox changes.
func TestCropViewBoxD2Unchanged(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50"><g/></svg>`)
	out, ok := cropViewBox(svg, cropFrac{0.2, 0.2, 0.6, 0.7})
	if !ok {
		t.Fatal("expected crop applied")
	}
	want := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="20.0000 10.0000 40.0000 25.0000"><g/></svg>`
	if string(out) != want {
		t.Errorf("got %s\nwant %s", out, want)
	}
}

// TestRenderVectorCropAspectRealResvg: a crop of a sized (Graphviz-shaped) svg
// must render at the crop's aspect ratio, not the full diagram's.
func TestRenderVectorCropAspectRealResvg(t *testing.T) {
	if _, err := exec.LookPath("resvg"); err != nil {
		t.Skip("resvg not on PATH")
	}
	t.Setenv("AEYE_RESVG", "")
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	svg := filepath.Join(dir, "graph.svg")
	if err := os.WriteFile(svg, []byte(graphvizSVG), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	crop := cropFrac{0.25, 0.40, 0.75, 0.60}
	out := renderVector(svg, crop, 1200)
	if out == "" {
		t.Fatal("renderVector failed")
	}
	size, ok := pngSize(out)
	if !ok {
		t.Fatalf("cannot read png size of %s", out)
	}
	want := (crop.h() * 548) / (crop.w() * 532) // crop height / crop width
	got := float64(size.Y) / float64(size.X)
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("rendered %v (aspect %.3f), want crop aspect %.3f", size, got, want)
	}
}

func TestCropViewBox(t *testing.T) {
	svg := []byte(`<svg viewBox="0 0 100 50"><g/></svg>`)
	out, ok := cropViewBox(svg, cropFrac{0.2, 0.2, 0.6, 0.7})
	if !ok {
		t.Fatal("expected crop applied")
	}
	// x: 0.2*100=20  y: 0.2*50=10  w: 0.4*100=40  h: 0.5*50=25
	if !bytes.Contains(out, []byte(`viewBox="20.0000 10.0000 40.0000 25.0000"`)) {
		t.Errorf("viewBox not rewritten to crop: %s", out)
	}
}

func TestCropViewBoxHonorsOffset(t *testing.T) {
	svg := []byte(`<svg viewBox="-10 -20 200 100"></svg>`)
	out, ok := cropViewBox(svg, cropFrac{0, 0, 0.5, 0.5})
	if !ok {
		t.Fatal("expected crop applied")
	}
	// origin offset carries through: x:-10  y:-20  w:0.5*200=100  h:0.5*100=50
	if !bytes.Contains(out, []byte(`viewBox="-10.0000 -20.0000 100.0000 50.0000"`)) {
		t.Errorf("offset viewBox not honored: %s", out)
	}
}

func TestCropViewBoxOnlyOuter(t *testing.T) {
	// Outer + inner d2-svg both carry a viewBox; only the first (outer) is the
	// crop window — the inner must be left intact.
	svg := []byte(`<svg viewBox="0 0 100 100"><svg viewBox="0 0 100 100"></svg></svg>`)
	out, _ := cropViewBox(svg, cropFrac{0, 0, 0.5, 0.5})
	if n := bytes.Count(out, []byte(`viewBox="0 0 100 100"`)); n != 1 {
		t.Errorf("inner viewBox should be untouched, found %d originals: %s", n, out)
	}
}

func TestCropViewBoxNoViewBox(t *testing.T) {
	if _, ok := cropViewBox([]byte(`<svg></svg>`), cropFrac{0, 0, 1, 1}); ok {
		t.Error("no viewBox → ok must be false")
	}
}

func TestRenderVectorMissingResvg(t *testing.T) {
	// A real svg file so we exercise the resvg-lookup path, not the stat bail.
	svg := filepath.Join(t.TempDir(), "x.svg")
	if err := os.WriteFile(svg, []byte(`<svg viewBox="0 0 10 10"></svg>`), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	t.Setenv("AEYE_RESVG", "/definitely/not/resvg")
	if got := renderVector(svg, fullCrop(), 1000); got != "" {
		t.Errorf("absent resvg must yield empty string, got %q", got)
	}
}

// TestRenderVectorBoundsHeight: the sharp render is sized by width, so a tall
// svg would come out arbitrarily tall without a height bound too.
func TestRenderVectorBoundsHeight(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	runLog := filepath.Join(dir, "resvg.log")
	stub := filepath.Join(dir, "resvg")
	script := "#!/bin/sh\necho \"$*\" >> " + runLog + "\nfor a; do :; done\necho png > \"$a\"\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil { //nolint:gosec // test stub must be executable
		t.Fatal(err)
	}
	t.Setenv("AEYE_RESVG", stub)
	svg := filepath.Join(dir, "x.svg")
	if err := os.WriteFile(svg, []byte(`<svg viewBox="0 0 100 8000"></svg>`), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	if renderVector(svg, fullCrop(), 1600) == "" {
		t.Fatal("renderVector failed")
	}
	logged, err := os.ReadFile(runLog) //nolint:gosec // temp-dir path
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(logged, []byte("--width 1600 --height 16384 ")) {
		t.Fatalf("resvg args %q, want --width 1600 --height 16384", logged)
	}
}

func TestRenderVectorTallSVGRealResvg(t *testing.T) {
	if _, err := exec.LookPath("resvg"); err != nil {
		t.Skip("resvg not on PATH")
	}
	t.Setenv("AEYE_RESVG", "")
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	svg := filepath.Join(dir, "tall.svg")
	if err := os.WriteFile(svg, []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="8000"/>`), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	out := renderVector(svg, fullCrop(), 1600)
	if out == "" {
		t.Fatal("renderVector failed")
	}
	if size, ok := pngSize(out); !ok || max(size.X, size.Y) > maxVectorHeight {
		t.Fatalf("rendered at %v (ok=%v), want long side <= %d", size, ok, maxVectorHeight)
	}
}
