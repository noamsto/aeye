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

	cachedSymbols(src, 20, 10, crop)
	cachedSymbols(src, 20, 10, crop)

	if *calls != 1 {
		t.Fatalf("same path/size/crop twice: %d chafa calls, want 1", *calls)
	}
}

func TestCachedSymbolsCropMiss(t *testing.T) {
	calls := stubRunChafa(t)
	src := filepath.Join(t.TempDir(), "a.png")
	writeTestImage(t, src, 40, 30)

	cachedSymbols(src, 20, 10, fullCrop())
	cachedSymbols(src, 20, 10, cropFrac{0.1, 0.1, 0.9, 0.9})

	if *calls != 2 {
		t.Fatalf("crop change: %d chafa calls, want 2", *calls)
	}
}

func TestCachedSymbolsCellBoxMiss(t *testing.T) {
	calls := stubRunChafa(t)
	src := filepath.Join(t.TempDir(), "a.png")
	writeTestImage(t, src, 40, 30)
	crop := fullCrop()

	cachedSymbols(src, 20, 10, crop)
	cachedSymbols(src, 8, 4, crop)

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

	cachedSymbols(a, 20, 10, crop)
	cachedSymbols(b, 20, 10, crop)

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

	if got := cachedSymbols(src, 20, 10, crop); got != "[img]" {
		t.Fatalf("first call: got %q, want [img]", got)
	}
	if got := cachedSymbols(src, 20, 10, crop); got != "ART" {
		t.Fatalf("second call: got %q, want ART", got)
	}
	cachedSymbols(src, 20, 10, crop)

	if calls != 2 {
		t.Fatalf("fail then succeed then hit: %d chafa calls, want 2", calls)
	}
}

func TestCachedSymbolsMtimeMiss(t *testing.T) {
	calls := stubRunChafa(t)
	src := filepath.Join(t.TempDir(), "a.png")
	writeTestImage(t, src, 40, 30)
	crop := fullCrop()

	cachedSymbols(src, 20, 10, crop)

	f, err := os.OpenFile(src, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cachedSymbols(src, 20, 10, crop)

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
