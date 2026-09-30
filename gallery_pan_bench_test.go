package main

import (
	"image"
	"os"
	"testing"
)

// BenchmarkPanFrame times the per-frame cost of a pan on kitty: panBy plus the
// preview re-store. The step moves the crop 0.15 of the image each frame, so a
// filmstrip-thumb overlay would move every frame: the worst case.
func BenchmarkPanFrame(b *testing.B) {
	dir := b.TempDir()
	p := write16BitFixture(b, dir, "a.png", benchSrcW, benchSrcH, 1)
	src, err := os.Open(p) //nolint:gosec // path is under the test's temp dir
	if err != nil {
		b.Fatal(err)
	}
	decoded, _, err := image.Decode(src)
	src.Close() //nolint:errcheck,gosec // read-only file
	if err != nil {
		b.Fatal(err)
	}
	working := workingCopy(decoded, workingScale(decoded.Bounds().Size(), benchBoxW, benchBoxH))

	tty, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0) //nolint:gosec // fixed path
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { tty.Close() }) //nolint:errcheck,gosec // benchmark cleanup
	b.Setenv("TMUX", "")

	m := &galleryModel{
		pane:       "%bench",
		backend:    backendKitty,
		l:          computeLayout(benchPaneW, benchPaneH),
		cellW:      benchCellW,
		cellH:      benchCellH,
		width:      benchPaneW,
		height:     benchPaneH,
		tty:        tty,
		ready:      true,
		images:     []imageEntry{{Path: p, Mtime: 1}},
		curImg:     working,
		curImgPath: p,
		curSize:    image.Pt(benchSrcW, benchSrcH),
	}
	frac := boxAspectFrac(benchSrcW, benchSrcH, benchBoxW, benchBoxH)
	m.crop = cropAtMagnification(2, .5, .5, frac)

	for _, s := range []string{m.zoomRawPath(), m.zoomScratchPath()} {
		os.Remove(s)                       //nolint:errcheck,gosec // stale scratch from an earlier run
		b.Cleanup(func() { os.Remove(s) }) //nolint:errcheck,gosec // benchmark cleanup
	}

	m.transmitView()
	m.transmitPreviewOnly()

	b.ReportAllocs()
	b.ResetTimer()
	d := 0.3
	for range b.N {
		m.panBy(d, 0)
		m.transmitPreviewOnly()
		d = -d
	}
}
