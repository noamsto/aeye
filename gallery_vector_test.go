package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

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
	if err := os.WriteFile(svg, []byte(`<svg viewBox="0 0 10 10"></svg>`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEYE_RESVG", "/definitely/not/resvg")
	if got := renderVector(svg, fullCrop(), 1000); got != "" {
		t.Errorf("absent resvg must yield empty string, got %q", got)
	}
}
