package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// BenchmarkColdSwitchAfterResize16 times the selection switch (Update on l)
// between two 16-bit screenshots when only the departing image's thumbnails are
// cached at the current size, as after a resize or split: the input-path cost of
// the first switch to an image the cache hasn't seen at that size.
func BenchmarkColdSwitchAfterResize16(b *testing.B) {
	dir := b.TempDir()
	paths := []string{
		write16BitFixture(b, dir, "a.png", benchSrcW, benchSrcH, 1),
		write16BitFixture(b, dir, "b.png", benchSrcW, benchSrcH, 2),
	}
	tty, err := os.CreateTemp(dir, "tty")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { tty.Close() }) //nolint:errcheck,gosec // benchmark cleanup

	old := imgCacheDir
	imgCacheDir = filepath.Join(dir, "cache")
	b.Cleanup(func() { imgCacheDir = old })

	l := computeLayout(benchPaneW, benchPaneH)
	m := galleryModel{
		pane:    "bench-" + strings.ReplaceAll(b.Name(), "/", "_"),
		backend: backendKitty,
		tty:     tty,
		crop:    fullCrop(),
		ready:   true,
		width:   benchPaneW,
		height:  benchPaneH,
		l:       l,
		cellW:   benchCellW,
		cellH:   benchCellH,
	}
	for _, p := range paths {
		m.images = append(m.images, imageEntry{Path: p})
	}
	cachedPNG(paths[0], l.previewW, l.previewH)
	cachedPNG(paths[0], l.stripW, l.stripH)
	warm := map[string]bool{}
	ents, err := os.ReadDir(imgCacheDir)
	if err != nil {
		b.Fatal(err)
	}
	for _, e := range ents {
		warm[e.Name()] = true
	}
	for _, p := range []string{m.zoomRawPath(), m.zoomScratchPath()} {
		os.Remove(p)                       //nolint:errcheck,gosec // stale scratch from an earlier run
		b.Cleanup(func() { os.Remove(p) }) //nolint:errcheck,gosec // benchmark cleanup
	}

	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		ents, err := os.ReadDir(imgCacheDir)
		if err != nil {
			b.Fatal(err)
		}
		for _, e := range ents {
			if !warm[e.Name()] {
				os.Remove(filepath.Join(imgCacheDir, e.Name())) //nolint:errcheck,gosec // back to only A cached
			}
		}
		m.cursor, m.lastTransmitSig = 0, nil
		b.StartTimer()
		next, _ := m.Update(tea.KeyPressMsg{Text: "l", Code: 'l'})
		m = next.(galleryModel)
	}
}
