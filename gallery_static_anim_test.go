package main

import (
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// flowHookSVG is a spoke the way the dispatcher's roster marks a live edge: a
// plain solid stroke tagged with the aeye-flow class hook, no dash of its own.
const flowHookSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 30">` +
	`<path class="aeye-flow" d="M0 15 L120 15" fill="none" stroke="#c00" stroke-width="3"/></svg>`

const solidLineSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 30">` +
	`<path d="M0 15 L120 15" fill="none" stroke="#c00" stroke-width="3"/></svg>`

// inkFraction is the share of the PNG at path covered by opaque pixels.
func inkFraction(t *testing.T, path string) float64 {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // test fixture in a temp dir
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck // read-only file
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0x8000 {
				n++
			}
		}
	}
	return float64(n) / float64(b.Dx()*b.Dy())
}

func mustRenderVector(t *testing.T, vector string, crop cropFrac) string {
	t.Helper()
	out := renderVector(vector, crop, 480)
	if out == "" {
		t.Fatalf("renderVector(%s, %v) failed", vector, crop)
	}
	return out
}

// TestStaticRastersKeepFlowDashes: every raster the gallery shows while the
// animation is not playing (the pan bitmap, the sharp re-render) must carry the
// dashes the animation draws, not a solid line.
func TestStaticRastersKeepFlowDashes(t *testing.T) {
	if _, err := exec.LookPath(resvgBin()); err != nil {
		t.Skip("resvg not on PATH")
	}
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	solid := filepath.Join(dir, "solid.svg")
	flow := filepath.Join(dir, "flow.svg")
	for p, s := range map[string]string{solid: solidLineSVG, flow: flowHookSVG} {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
			t.Fatal(err)
		}
	}

	t.Run("renderVector", func(t *testing.T) {
		solidInk := inkFraction(t, mustRenderVector(t, solid, fullCrop()))
		for _, crop := range []cropFrac{fullCrop(), {0.1, 0, 0.9, 1}} {
			ink := inkFraction(t, mustRenderVector(t, flow, crop))
			if ink*2 > solidInk {
				t.Errorf("crop %v: %.3f of the canvas inked, want well under the solid line's %.3f (dashed)", crop, ink, solidInk)
			}
		}
	})

	t.Run("render-diagram svg input", func(t *testing.T) {
		render := func(in, name string) float64 {
			out := filepath.Join(dir, name+".png")
			if err := runRenderDiagram(in, out); err != nil {
				t.Fatal(err)
			}
			return inkFraction(t, out)
		}
		solidInk, ink := render(solid, "solid"), render(flow, "roster")
		if ink*2 > solidInk {
			t.Errorf("%.3f of the canvas inked, want well under the solid line's %.3f (dashed)", ink, solidInk)
		}
		sib, err := os.ReadFile(filepath.Join(dir, "roster.svg")) //nolint:gosec // test fixture in a temp dir
		if err != nil {
			t.Fatal(err)
		}
		if string(sib) != flowHookSVG {
			t.Errorf("sibling svg must stay the producer's bytes, got %q", sib)
		}
	})
}
