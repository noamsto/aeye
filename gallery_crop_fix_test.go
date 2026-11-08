package main

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// TestRenderVectorNestedViewBoxFallsBack: when the crop can't be applied to the
// root viewBox, renderVector returns "" (so the caller keeps the bitmap crop)
// instead of rendering the whole diagram under a sharp frame of the wrong region.
func TestRenderVectorNestedViewBoxFallsBack(t *testing.T) {
	cases := map[string]string{
		"marker":     `<svg width="100" height="100"><marker id="m" viewBox="0 0 10 10"></marker></svg>`,
		"nested-svg": `<svg width="100" height="100"><svg viewBox="0 0 10 10"></svg></svg>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
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
			if err := os.WriteFile(svg, []byte(body), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
				t.Fatal(err)
			}
			if got := renderVector(svg, cropFrac{0, 0, 0.5, 0.5}, 800); got != "" {
				t.Errorf("renderVector = %q, want \"\" when the crop can't apply", got)
			}
			if _, err := os.Stat(runLog); err == nil {
				t.Error("resvg ran although the crop could not be applied")
			}
		})
	}
}

// TestStorePreviewCropRawIsStraightAlpha: kitty reads f=32 bytes as straight
// RGBA, so the raw zoom frame must be un-premultiplied (a half-transparent red
// stays 128,0,0,128 rather than the premultiplied 64,0,0,128).
func TestStorePreviewCropRawIsStraightAlpha(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("TMUX", "")
	cases := map[string]struct {
		pix  color.NRGBA
		want [4]byte
	}{
		"translucent": {color.NRGBA{128, 0, 0, 128}, [4]byte{128, 0, 0, 128}},
		"opaque":      {color.NRGBA{10, 20, 30, 255}, [4]byte{10, 20, 30, 255}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			src := image.NewNRGBA(image.Rect(0, 0, 400, 400))
			for y := range 400 {
				for x := range 400 {
					src.SetNRGBA(x, y, c.pix)
				}
			}
			m := &galleryModel{
				pane:    "%alpha",
				backend: backendKitty,
				l:       computeLayout(120, 40),
				cellW:   10,
				cellH:   20,
				width:   120,
				height:  40,
				images:  []imageEntry{{Path: "/nonexistent/a.png", Mtime: 1}},
				curImg:  src,
				curSize: image.Pt(400, 400),
				crop:    cropFrac{0.25, 0.25, 0.75, 0.75},
			}
			os.Remove(m.zoomRawPath()) //nolint:errcheck,gosec // stale scratch
			m.storePreviewCrop()
			raw, err := os.ReadFile(m.zoomRawPath()) //nolint:gosec // temp-dir path
			if err != nil {
				t.Fatalf("no raw frame written: %v", err)
			}
			if len(raw) < 4 || len(raw)%4 != 0 {
				t.Fatalf("raw frame length %d", len(raw))
			}
			for i := 0; i < len(raw); i += 4 {
				for k := range 4 {
					if d := int(raw[i+k]) - int(c.want[k]); d < -1 || d > 1 {
						t.Fatalf("pixel %d = %v, want %v (+-1)", i/4, raw[i:i+4], c.want)
					}
				}
			}
		})
	}
}
