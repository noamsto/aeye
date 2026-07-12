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

// Even a PNG already small enough is pinned into the viewer-owned cache dir
// rather than handed to kitty by its live path: kitty fetches the transmit path
// asynchronously, so the source could be pruned/re-rendered before the fetch.
func TestCachedPNGSmallPNGPinned(t *testing.T) {
	imgCacheDir = t.TempDir()
	src := filepath.Join(t.TempDir(), "small.png")
	writeTestImage(t, src, 8, 8)

	got := cachedPNG(src, 20, 10)
	if got == src {
		t.Fatalf("small PNG should be pinned to the cache, got original path %q", got)
	}
	if filepath.Dir(got) != imgCacheDir {
		t.Errorf("pinned file %q not under cache dir %q", got, imgCacheDir)
	}
	if !decodesAsPNG(t, got) {
		t.Errorf("pinned file %q is not a PNG", got)
	}
}

// warmCache must populate the transcode cache for both the preview and strip
// sizes so navigating to a freshly captured image is a hit, not a decode.
func TestWarmCachePopulatesBothSizes(t *testing.T) {
	imgCacheDir = t.TempDir()
	src := filepath.Join(t.TempDir(), "big.jpeg")
	writeTestImage(t, src, 400, 300)

	warmCache([]string{src}, 20, 10, 8, 4)

	entries, err := os.ReadDir(imgCacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("warmCache should cache 2 distinct sizes, got %d files", len(entries))
	}
	for _, dims := range [][2]int{{20, 10}, {8, 4}} {
		got := cachedPNG(src, dims[0], dims[1])
		if got == src {
			t.Errorf("size %v not warmed: cachedPNG fell back to the original path", dims)
		}
		if _, err := os.Stat(got); err != nil {
			t.Errorf("warmed cache file %q missing: %v", got, err)
		}
	}
}

// A missing file falls back to the original path (kitty will simply draw nothing).
func TestCachedPNGMissingFile(t *testing.T) {
	imgCacheDir = t.TempDir()
	if got := cachedPNG("/no/such/file.png", 20, 10); got != "/no/such/file.png" {
		t.Errorf("missing file should fall back to src, got %q", got)
	}
}
