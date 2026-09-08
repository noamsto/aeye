package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetSymbolsCache(t *testing.T) {
	t.Helper()
	origRunChafa := runChafa
	t.Cleanup(func() {
		runChafa = origRunChafa
		symbolsCacheMu.Lock()
		symbolsCache = map[string]string{}
		symbolsCacheMu.Unlock()
	})
}

func stubRunChafa(t *testing.T) *int {
	t.Helper()
	resetSymbolsCache(t)
	var calls int
	runChafa = func(args []string) ([]byte, error) {
		calls++
		return []byte("ART\n"), nil
	}
	return &calls
}

func TestCachedSymbolsHit(t *testing.T) {
	calls := stubRunChafa(t)
	src := filepath.Join(t.TempDir(), "a.png")
	writeTestImage(t, src, 40, 30)
	crop := fullCrop()

	cachedSymbols(src, 20, 10, crop, func() string { return src })
	cachedSymbols(src, 20, 10, crop, func() string { return src })

	if *calls != 1 {
		t.Fatalf("same path/size/crop twice: %d chafa calls, want 1", *calls)
	}
}

func TestCachedSymbolsCropMiss(t *testing.T) {
	calls := stubRunChafa(t)
	src := filepath.Join(t.TempDir(), "a.png")
	writeTestImage(t, src, 40, 30)

	cachedSymbols(src, 20, 10, fullCrop(), func() string { return src })
	cachedSymbols(src, 20, 10, cropFrac{0.1, 0.1, 0.9, 0.9}, func() string { return src })

	if *calls != 2 {
		t.Fatalf("crop change: %d chafa calls, want 2", *calls)
	}
}

func TestCachedSymbolsCellBoxMiss(t *testing.T) {
	calls := stubRunChafa(t)
	src := filepath.Join(t.TempDir(), "a.png")
	writeTestImage(t, src, 40, 30)
	crop := fullCrop()

	cachedSymbols(src, 20, 10, crop, func() string { return src })
	cachedSymbols(src, 8, 4, crop, func() string { return src })

	if *calls != 2 {
		t.Fatalf("cell box change: %d chafa calls, want 2", *calls)
	}
}

func TestCachedSymbolsDistinctPathMiss(t *testing.T) {
	calls := stubRunChafa(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	writeTestImage(t, a, 40, 30)
	writeTestImage(t, b, 40, 30)
	crop := fullCrop()

	cachedSymbols(a, 20, 10, crop, func() string { return a })
	cachedSymbols(b, 20, 10, crop, func() string { return b })

	if *calls != 2 {
		t.Fatalf("distinct paths: %d chafa calls, want 2", *calls)
	}
}

func TestCachedSymbolsFailThenSucceed(t *testing.T) {
	resetSymbolsCache(t)
	var calls int
	runChafa = func(args []string) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, fmt.Errorf("transient")
		}
		return []byte("ART\n"), nil
	}

	src := filepath.Join(t.TempDir(), "a.png")
	writeTestImage(t, src, 40, 30)
	crop := fullCrop()

	if got := cachedSymbols(src, 20, 10, crop, func() string { return src }); got != "[img]" {
		t.Fatalf("first call: got %q, want [img]", got)
	}
	if got := cachedSymbols(src, 20, 10, crop, func() string { return src }); got != "ART" {
		t.Fatalf("second call: got %q, want ART", got)
	}
	cachedSymbols(src, 20, 10, crop, func() string { return src })

	if calls != 2 {
		t.Fatalf("fail then succeed then hit: %d chafa calls, want 2", calls)
	}
}

func TestCachedSymbolsMtimeMiss(t *testing.T) {
	calls := stubRunChafa(t)
	src := filepath.Join(t.TempDir(), "a.png")
	writeTestImage(t, src, 40, 30)
	crop := fullCrop()

	cachedSymbols(src, 20, 10, crop, func() string { return src })

	f, err := os.OpenFile(src, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cachedSymbols(src, 20, 10, crop, func() string { return src })

	if *calls != 2 {
		t.Fatalf("mtime/size change: %d chafa calls, want 2", *calls)
	}
}

func TestRenderViewSymbolsCache(t *testing.T) {
	calls := stubRunChafa(t)
	dir := t.TempDir()
	const n = 5
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("img%d.png", i))
		writeTestImage(t, p, 20, 20)
	}

	m := galleryModel{
		backend: backendSymbols,
		width:   100,
		height:  40,
		cursor:  2,
		crop:    fullCrop(),
	}
	for i := 0; i < n; i++ {
		m.images = append(m.images, imageEntry{Path: filepath.Join(dir, fmt.Sprintf("img%d.png", i))})
	}
	m.l = computeLayout(m.width, m.height)

	visible := min(m.l.stripCols, len(m.images))
	wantFirst := 1 + visible

	*calls = 0
	out := m.renderView()
	if !strings.Contains(out, "ART") {
		t.Fatalf("first renderView output missing stub art ART")
	}
	if *calls != wantFirst {
		t.Fatalf("first renderView: %d chafa calls, want %d (1 preview + %d thumbs)", *calls, wantFirst, visible)
	}

	before := *calls
	_ = m.renderView()
	if *calls-before != 0 {
		t.Fatalf("second renderView: %d additional chafa calls, want 0", *calls-before)
	}
}

// TestRenderViewSymbolsZoomCacheHit asserts a cache hit on a zoomed crop skips
// both the chafa fork and the crop/PNG re-encode: the zoom scratch file's
// mtime must not advance on a repeat renderView().
func TestRenderViewSymbolsZoomCacheHit(t *testing.T) {
	calls := stubRunChafa(t)
	src := filepath.Join(t.TempDir(), "half.png")
	img := writeHalfToneImage(t, src, 120, 80)

	m := galleryModel{
		pane:       "%zoomcache",
		backend:    backendSymbols,
		images:     []imageEntry{{Path: src}},
		cursor:     0,
		curImg:     img,
		curImgPath: src,
		crop:       cropFrac{0, 0, 0.5, 1},
		width:      80,
		height:     40,
		cellW:      10,
		cellH:      20,
		l:          layout{previewW: 40, previewH: 16, stripW: 12, stripH: 6, stripCols: 1},
	}

	*calls = 0
	_ = m.renderView()
	if *calls == 0 {
		t.Fatal("first renderView with a zoomed crop should fork chafa at least once")
	}

	scratch := m.zoomScratchPath()
	fi, err := os.Stat(scratch)
	if err != nil {
		t.Fatalf("expected zoom scratch PNG to exist after first render: %v", err)
	}
	encodedAt := fi.ModTime()

	before := *calls
	_ = m.renderView()
	if *calls != before {
		t.Fatalf("second identical renderView: %d additional chafa forks, want 0", *calls-before)
	}

	fi, err = os.Stat(scratch)
	if err != nil {
		t.Fatalf("zoom scratch PNG vanished between renders: %v", err)
	}
	if !fi.ModTime().Equal(encodedAt) {
		t.Fatal("second identical renderView re-encoded the zoom scratch PNG; the crop source must stay lazy on a cache hit")
	}
}
