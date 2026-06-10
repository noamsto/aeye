package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// writeTestImage encodes a solid w×h image to path, format chosen by extension.
func writeTestImage(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if filepath.Ext(path) == ".jpeg" {
		if err := jpeg.Encode(f, img, nil); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func decodesAsPNG(t *testing.T, path string) bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, format, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return format == "png"
}

// A JPEG must be transcoded to a PNG cache file so kitty's f=100 transmit can
// decode it — this is the bug that left JPEGs blank in the carousel.
func TestCachedPNGTranscodesJPEG(t *testing.T) {
	imgCacheDir = t.TempDir()
	src := filepath.Join(t.TempDir(), "big.jpeg")
	writeTestImage(t, src, 400, 300)

	got := cachedPNG(src, 20, 10)
	if got == src {
		t.Fatalf("JPEG should be transcoded, got original path %q", got)
	}
	if filepath.Dir(got) != imgCacheDir {
		t.Errorf("cache file %q not under cache dir %q", got, imgCacheDir)
	}
	if !decodesAsPNG(t, got) {
		t.Errorf("cache file %q is not a PNG", got)
	}
}

// A PNG already no larger than the cell box is handed to kitty untouched.
func TestCachedPNGSmallPNGUntouched(t *testing.T) {
	imgCacheDir = t.TempDir()
	src := filepath.Join(t.TempDir(), "small.png")
	writeTestImage(t, src, 8, 8)

	if got := cachedPNG(src, 20, 10); got != src {
		t.Errorf("small PNG should be returned untouched, got %q", got)
	}
}

// A missing file falls back to the original path (kitty will simply draw nothing).
func TestCachedPNGMissingFile(t *testing.T) {
	imgCacheDir = t.TempDir()
	if got := cachedPNG("/no/such/file.png", 20, 10); got != "/no/such/file.png" {
		t.Errorf("missing file should fall back to src, got %q", got)
	}
}
