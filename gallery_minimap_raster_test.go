package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func newRasterMinimapModel(t *testing.T) *galleryModel {
	t.Helper()
	m := newMinimapModel(t, newTransmitRecorder(t))
	m.backend, m.rasterFormat = backendRaster, formatITerm
	return m
}

func decodePNGFile(t *testing.T, path string) image.Image {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // scratch path the model wrote
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestStripSlotPNGPlainAtFit(t *testing.T) {
	m := newRasterMinimapModel(t)
	for i := range m.images {
		if want := cachedPNG(m.images[i].Path, m.l.stripW, m.l.stripH); m.stripSlotPNG(i) != want {
			t.Errorf("slot %d at fit = %s, want the plain thumb %s", i, m.stripSlotPNG(i), want)
		}
	}
}

func TestStripSlotPNGOverlayOnSelectedOnly(t *testing.T) {
	m := newRasterMinimapModel(t)
	m.zoomBy(zoomStep)
	if got := m.stripSlotPNG(m.cursor); got != m.minimapPNGPath() {
		t.Fatalf("selected slot = %s, want the overlay %s", got, m.minimapPNGPath())
	}
	other := 0
	if want := cachedPNG(m.images[other].Path, m.l.stripW, m.l.stripH); m.stripSlotPNG(other) != want {
		t.Errorf("unselected slot = %s, want the plain thumb %s", m.stripSlotPNG(other), want)
	}

	fit := minimapFit(m.curSize, m.minimapBox())
	r := minimapRect(fit, m.crop)
	img := decodePNGFile(t, m.minimapPNGPath())
	if got := img.Bounds().Size(); got != fit {
		t.Fatalf("overlay raster is %v, want %v", got, fit)
	}
	if c := color32(img.At(r.Min.X, r.Min.Y)); c != (rgb{0, 0, 0}) {
		t.Errorf("outer ring corner = %v, want black", c)
	}
	if c := color32(img.At(r.Min.X+1, r.Min.Y+1)); c != (rgb{255, 255, 255}) {
		t.Errorf("inner ring corner = %v, want white", c)
	}
}

func TestStripSlotPNGFollowsPanAndClears(t *testing.T) {
	m := newRasterMinimapModel(t)
	m.zoomBy(zoomStep * zoomStep)
	m.stripSlotPNG(m.cursor)
	before := decodePNGFile(t, m.minimapPNGPath())

	m.panBy(0.3, 0)
	if got := m.stripSlotPNG(m.cursor); got != m.minimapPNGPath() {
		t.Fatalf("after pan, selected slot = %s, want the overlay", got)
	}
	after := decodePNGFile(t, m.minimapPNGPath())
	if sameImage(before, after) {
		t.Error("overlay did not move with the pan")
	}

	m.crop = fullCrop()
	if want := cachedPNG(m.images[m.cursor].Path, m.l.stripW, m.l.stripH); m.stripSlotPNG(m.cursor) != want {
		t.Error("overlay not cleared at fit")
	}
}

func TestStripSlotPNGBrokenBaseFallsBack(t *testing.T) {
	m := newRasterMinimapModel(t)
	m.zoomBy(zoomStep)
	m.mini.baseKey = minimapBaseKey{}
	m.images[m.cursor].Path = "/nonexistent/aeye-missing.png"
	if want := cachedPNG(m.images[m.cursor].Path, m.l.stripW, m.l.stripH); m.stripSlotPNG(m.cursor) != want {
		t.Error("unreadable base should fall back to the plain thumb")
	}
}

type rgb struct{ r, g, b uint8 }

func color32(c interface{ RGBA() (r, g, b, a uint32) }) rgb {
	r, g, b, _ := c.RGBA()
	return rgb{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)} //nolint:gosec // 16-bit channel >> 8 fits in 8 bits
}

func sameImage(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			if color32(a.At(x, y)) != color32(b.At(x, y)) {
				return false
			}
		}
	}
	return true
}

// BenchmarkRasterPanStrip times one raster strip paint of the selected slot: the
// per-pan cost of the minimap on sixel/iTerm. "plain" is the baseline thumb.
func BenchmarkRasterPanStrip(b *testing.B) {
	if _, err := exec.LookPath("chafa"); err != nil {
		b.Skip("chafa not on PATH")
	}
	for _, mode := range []string{"plain", "overlay"} {
		b.Run(mode, func(b *testing.B) {
			m := newRasterBenchModel(b)
			if mode == "overlay" {
				m.zoomBy(zoomStep * zoomStep)
			}
			b.ResetTimer()
			for i := range b.N {
				if mode == "overlay" {
					m.panBy(0.01*float64(1-2*(i%2)), 0)
				}
				renderRaster(m.rasterFormat, m.stripSlotPNG(m.cursor), m.l.stripW, m.l.stripH)
			}
		})
	}
}

func newRasterBenchModel(b *testing.B) *galleryModel {
	b.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	p := filepath.Join(b.TempDir(), "bench.png")
	f, err := os.Create(p) //nolint:gosec // benchmark fixture path
	if err != nil {
		b.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		b.Fatal(err)
	}
	f.Close() //nolint:errcheck,gosec // fixture file
	m := &galleryModel{
		pane: "%rasterbench", backend: backendRaster, rasterFormat: formatSixel,
		width: 160, height: 50, ready: true,
		images:  []imageEntry{{Path: p, Mtime: 1}},
		curSize: image.Pt(1600, 900),
		crop:    fullCrop(),
	}
	m.l = computeLayout(m.width, m.height)
	b.Cleanup(func() { os.Remove(m.minimapPNGPath()) }) //nolint:errcheck,gosec // scratch cleanup
	return m
}
