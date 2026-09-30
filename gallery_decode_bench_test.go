package main

import (
	"context"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

const (
	benchSrcW, benchSrcH = 3456, 2234 // a retina screenshot
	benchPaneW           = 124        // previewW 122 cells x 10 px = 1220
	benchPaneH           = 57         // previewH 37 cells x 22 px = 814
	benchCellW           = 10
	benchCellH           = 22
	benchBoxW            = 1220
	benchBoxH            = 814
)

// noiseNRGBA64 is a w x h opaque image of deterministic incompressible 16-bit
// noise: a splitmix64 stream keyed by seed fills Pix eight bytes (one pixel) at a
// time.
func noiseNRGBA64(w, h int, seed uint64) *image.NRGBA64 {
	img := image.NewNRGBA64(image.Rect(0, 0, w, h))
	state := seed
	for i := 0; i+8 <= len(img.Pix); i += 8 {
		state += 0x9e3779b97f4a7c15
		z := state
		z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
		z = (z ^ (z >> 27)) * 0x94d049bb133111eb
		z ^= z >> 31
		binary.BigEndian.PutUint64(img.Pix[i:], z)
		img.Pix[i+6], img.Pix[i+7] = 0xff, 0xff
	}
	return img
}

// write16BitFixture writes a w x h 16-bit noise PNG named name into dir and
// returns its path. BestSpeed keeps generating the fixture cheap; the noise
// makes the file about as large as the raw pixels either way.
func write16BitFixture(tb testing.TB, dir, name string, w, h int, seed uint64) string {
	tb.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p) //nolint:gosec // path is under the test's temp dir
	if err != nil {
		tb.Fatal(err)
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(f, noiseNRGBA64(w, h, seed)); err != nil {
		f.Close() //nolint:errcheck,gosec // already failing
		tb.Fatal(err)
	}
	if err := f.Close(); err != nil {
		tb.Fatal(err)
	}
	return p
}

func logFixtureSize(b *testing.B, p string) {
	b.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("%s: %.1f MB on disk", filepath.Base(p), float64(fi.Size())/(1<<20))
}

// BenchmarkSelectSwitch16 times the selection switch (Update on l/h) between two
// 16-bit screenshots with the thumbnail caches warm: the input-path cost of
// moving the cursor.
func BenchmarkSelectSwitch16(b *testing.B) {
	dir := b.TempDir()
	paths := []string{
		write16BitFixture(b, dir, "a.png", benchSrcW, benchSrcH, 1),
		write16BitFixture(b, dir, "b.png", benchSrcW, benchSrcH, 2),
	}
	for _, p := range paths {
		logFixtureSize(b, p)
	}
	tty, err := os.CreateTemp(dir, "tty")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { tty.Close() }) //nolint:errcheck,gosec // benchmark cleanup

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
		cachedPNG(p, l.previewW, l.previewH)
		cachedPNG(p, l.stripW, l.stripH)
	}
	for _, p := range []string{m.zoomRawPath(), m.zoomScratchPath()} {
		os.Remove(p)                       //nolint:errcheck,gosec // stale scratch from an earlier run
		b.Cleanup(func() { os.Remove(p) }) //nolint:errcheck,gosec // benchmark cleanup
	}

	keys := [2]tea.KeyPressMsg{{Text: "l", Code: 'l'}, {Text: "h", Code: 'h'}}
	b.ResetTimer()
	for i := range b.N {
		next, _ := m.Update(keys[i%2])
		m = next.(galleryModel)
	}
}

// BenchmarkRetainedSelected reports the heap kept alive per selected image:
// the full image.Decode result (what a synchronous decode retains) against the
// bounded working copy.
func BenchmarkRetainedSelected(b *testing.B) {
	dir := b.TempDir()
	p := write16BitFixture(b, dir, "a.png", benchSrcW, benchSrcH, 1)
	logFixtureSize(b, p)

	decode16 := func() image.Image {
		f, err := os.Open(p) //nolint:gosec // path is under the test's temp dir
		if err != nil {
			b.Fatal(err)
		}
		defer f.Close() //nolint:errcheck // read-only file
		img, _, err := image.Decode(f)
		if err != nil {
			b.Fatal(err)
		}
		return img
	}
	working := func() image.Image {
		img, _, _ := decodeWorking(context.Background(), p, benchBoxW, benchBoxH)
		if img == nil {
			b.Fatal("decodeWorking failed")
		}
		return img
	}

	for _, tc := range []struct {
		name   string
		decode func() image.Image
	}{{"decode16", decode16}, {"working", working}} {
		b.Run(tc.name, func(b *testing.B) {
			var retained float64
			for range b.N {
				b.StopTimer()
				var before, after runtime.MemStats
				runtime.GC()
				runtime.ReadMemStats(&before)
				b.StartTimer()
				img := tc.decode()
				b.StopTimer()
				runtime.GC()
				runtime.ReadMemStats(&after)
				runtime.KeepAlive(img)
				retained += float64(after.HeapAlloc) - float64(before.HeapAlloc)
			}
			b.ReportMetric(retained/float64(b.N)/(1<<20), "MB-retained")
		})
	}
}

// BenchmarkCropRaster times cropRaster on a ~2x zoom in the preview box: the
// per-keystroke cost of pan and zoom, from the full 16-bit decode against the
// working copy.
func BenchmarkCropRaster(b *testing.B) {
	dir := b.TempDir()
	p := write16BitFixture(b, dir, "a.png", benchSrcW, benchSrcH, 1)
	logFixtureSize(b, p)
	f, err := os.Open(p) //nolint:gosec // path is under the test's temp dir
	if err != nil {
		b.Fatal(err)
	}
	decoded, _, err := image.Decode(f)
	f.Close() //nolint:errcheck,gosec // read-only file
	if err != nil {
		b.Fatal(err)
	}
	working := workingCopy(decoded, workingScale(decoded.Bounds().Size(), benchBoxW, benchBoxH))

	m := galleryModel{
		backend: backendKitty,
		l:       computeLayout(benchPaneW, benchPaneH),
		cellW:   benchCellW,
		cellH:   benchCellH,
	}
	frac := boxAspectFrac(benchSrcW, benchSrcH, benchBoxW, benchBoxH)
	m.crop = cropAtMagnification(2, .5, .5, frac)

	for _, tc := range []struct {
		name string
		src  image.Image
	}{{"decode16", decoded}, {"working", working}} {
		b.Run(tc.name, func(b *testing.B) {
			for range b.N {
				if m.cropRaster(tc.src, m.l.previewW, m.l.previewH) == nil {
					b.Fatal("nothing rendered")
				}
			}
		})
	}
}
