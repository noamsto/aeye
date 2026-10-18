package main

import (
	"fmt"
	"image"
	"math"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// displayedSpan is how much of the preview box, per axis, the zoomed view covers
// once kitty fits the crop into the box preserving aspect: 1 on the box-binding
// axis, the letterbox shortfall on the other.
func displayedSpan(crop cropFrac, src image.Point, boxW, boxH int) (spanX, spanY float64) {
	cw, ch := crop.w()*float64(src.X), crop.h()*float64(src.Y)
	scale := min(float64(boxW)/cw, float64(boxH)/ch)
	return cw * scale / float64(boxW), ch * scale / float64(boxH)
}

// Regression (#285): on an extreme-aspect image, the magnification at which the
// letterboxed axis grows to fill the box can exceed zoomMax. Clamping the
// box-bound axis at zoomMax then stopped zoom short of that point, so no amount
// of z or wheel ever filled the box — only at pane sizes whose aspect pushed the
// fill point past zoomMax, which is what made it look intermittent.
func TestZoomFillsBoxOnExtremeAspect(t *testing.T) {
	images := []struct {
		name string
		size image.Point
	}{
		{"thin-1:6", image.Pt(300, 1800)},
		{"short-6:1", image.Pt(1800, 300)},
		{"thin-1:10", image.Pt(200, 2000)},
		{"short-10:1", image.Pt(2000, 200)},
	}
	panes := []image.Point{{100, 50}, {200, 50}, {60, 80}, {40, 100}, {120, 40}}
	cells := []image.Point{{10, 20}, {8, 17}, {12, 26}}
	inputs := []struct {
		name string
		step func(m *galleryModel)
	}{
		{"z", func(m *galleryModel) {
			out, _ := m.Update(tea.KeyPressMsg{Text: "z", Code: 'z'})
			*m = out.(galleryModel)
		}},
		{"wheel-center", func(m *galleryModel) {
			pr := m.previewRect()
			*m, _ = m.handleMouse(tea.MouseWheelMsg{X: pr.x + pr.w/2, Y: pr.y + pr.h/2, Button: tea.MouseWheelUp})
		}},
		{"wheel-corner", func(m *galleryModel) {
			pr := m.previewRect()
			*m, _ = m.handleMouse(tea.MouseWheelMsg{X: pr.x + pr.w/5, Y: pr.y + pr.h/5, Button: tea.MouseWheelUp})
		}},
	}
	for _, img := range images {
		for _, pane := range panes {
			for _, cell := range cells {
				for _, in := range inputs {
					name := fmt.Sprintf("%s/pane%dx%d/cell%dx%d/%s", img.name, pane.X, pane.Y, cell.X, cell.Y, in.name)
					t.Run(name, func(t *testing.T) {
						m := mouseModel(pane.X, pane.Y, 0, 1)
						m.ready = true
						m.cellW, m.cellH = cell.X, cell.Y
						m.curSize = img.size
						m.curImgPath = m.images[0].Path
						m.crop = fullCrop()
						boxW, boxH := m.l.previewW*cell.X, m.l.previewH*cell.Y
						frac := boxAspectFrac(img.size.X, img.size.Y, boxW, boxH)
						fillMag := max(frac, 1/frac) // rest-relative zoom at which both axes fill
						requested := 1.0
						for i := range 40 {
							in.step(&m)
							requested *= zoomStep
							if requested < fillMag {
								continue
							}
							sx, sy := displayedSpan(m.crop, img.size, boxW, boxH)
							if math.Abs(sx-1) > 1e-3 || math.Abs(sy-1) > 1e-3 {
								t.Fatalf("step %d (zoom %.2fx >= fill %.2fx): view covers %.3f x %.3f of the box, crop=%+v",
									i+1, requested, fillMag, sx, sy, m.crop)
							}
						}
					})
				}
			}
		}
	}
}

// extremeAspectModel is a 300x1800 image in a 100x50 pane of 10x20 cells; the
// preview box's aspect puts the fill point past zoomMax. It also returns the
// box aspect fraction.
func extremeAspectModel(t *testing.T) (galleryModel, float64) {
	t.Helper()
	m := mouseModel(100, 50, 0, 1)
	if len(m.images) < 1 {
		t.Fatalf("model has %d images, want 1", len(m.images))
	}
	m.ready = true
	m.cellW, m.cellH = 10, 20
	m.curSize = image.Pt(300, 1800)
	m.curImgPath = m.images[0].Path
	m.crop = fullCrop()
	return m, boxAspectFrac(300, 1800, m.l.previewW*10, m.l.previewH*20)
}

func pressZoom(m *galleryModel, r rune) {
	out, _ := m.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	*m = out.(galleryModel)
}

func TestZoomDeepestIsFillViewOnExtremeAspect(t *testing.T) {
	m, frac := extremeAspectModel(t)
	for range 40 {
		pressZoom(&m, 'z')
	}
	if math.Abs(m.crop.w()-1) > 1e-9 || math.Abs(m.crop.h()-1/frac) > 1e-9 {
		t.Fatalf("deepest crop = %.6f x %.6f, want 1 x %.6f", m.crop.w(), m.crop.h(), 1/frac)
	}
	deepest := m.crop
	pressZoom(&m, 'z')
	if m.crop != deepest {
		t.Fatalf("zoom past the ceiling moved the crop: %+v -> %+v", deepest, m.crop)
	}
}

func TestZoomInAfterResizeFillsBox(t *testing.T) {
	m, _ := extremeAspectModel(t)
	for range 40 {
		pressZoom(&m, 'z')
	}
	// A resize can leave the previous fill crop letterboxed in the new box; the
	// next zoom-in must still fill it.
	m.width, m.height = 70, 50
	m.l = computeLayout(70, 50)
	pressZoom(&m, 'z')
	sx, sy := displayedSpan(m.crop, m.curSize, m.l.previewW*10, m.l.previewH*20)
	if math.Abs(sx-1) > 1e-3 || math.Abs(sy-1) > 1e-3 {
		t.Fatalf("zoom-in after resize left the view letterboxed: spans %.4f x %.4f", sx, sy)
	}
}

func TestZoomOutFromFillCeilingRetraces(t *testing.T) {
	m, frac := extremeAspectModel(t)
	for range 40 {
		pressZoom(&m, 'z')
	}
	before := zoomMagnification(m.crop, frac)
	pressZoom(&m, 'Z')
	if got := before / zoomMagnification(m.crop, frac); math.Abs(got-zoomStep) > 1e-9 {
		t.Fatalf("first zoom-out divided magnification by %.6f, want %.2f", got, zoomStep)
	}
	for range 60 {
		pressZoom(&m, 'Z')
	}
	if !m.crop.isFull() {
		t.Fatalf("zoom-out never reached the full image: %+v", m.crop)
	}
}

// Regression (#355): behind a remote tmux the viewer's CSI 16 t is answered by
// tmux itself, which knows nothing about the mirroring terminal's real cell and
// replies with its own default (16x32). The crop is then cut to the wrong box
// aspect and kitty, fitting it into the real cell grid, pillarboxes it once zoom
// reaches the packed phase. Measuring the mirroring client's cell fixes the
// geometry; this drives the whole zoom range against the real 15x28 cell.
func TestMirroredCarouselZoomFillsRealBox(t *testing.T) {
	const realW, realH = 15, 28
	cases := []struct {
		name string
		pane image.Point
		img  image.Point
	}{
		{"mirror pane, landscape", image.Pt(97, 62), image.Pt(1020, 394)},
		{"tall narrow box, landscape", image.Pt(40, 90), image.Pt(1600, 900)},
		{"wide box, portrait", image.Pt(120, 24), image.Pt(300, 900)},
	}
	zoomAll := func(t *testing.T, cellW, cellH int, pane, img image.Point) (worst float64) {
		t.Helper()
		m := mouseModel(pane.X, pane.Y, 0, 1)
		m.ready = true
		m.cellW, m.cellH = cellW, cellH
		m.curSize = img
		m.curImgPath = m.images[0].Path
		m.crop = fullCrop()
		boxW, boxH := m.l.previewW*realW, m.l.previewH*realH
		bindsX := boxAspectFrac(img.X, img.Y, boxW, boxH) <= 1
		worst = 1
		for range 30 {
			pressZoom(&m, 'z')
			sx, sy := displayedSpan(m.crop, img, boxW, boxH)
			worst = min(worst, map[bool]float64{true: sx, false: sy}[bindsX])
		}
		return worst
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := func() (int, int, bool) { return realW, realH, true }
			query := func() (int, int) { return 16, 32 }
			w, h := measureCellPx(true, "", client, query)
			if got := zoomAll(t, w, h, tc.pane, tc.img); got < 0.99 {
				t.Errorf("measured cell %dx%d: binding axis covered only %.3f of the real box", w, h, got)
			}
			if tc.pane.X == 97 {
				if got := zoomAll(t, 16, 32, tc.pane, tc.img); got > 0.97 {
					t.Errorf("setup: tmux's default 16x32 should pillarbox the 97x62 mirror pane, covered %.3f", got)
				}
			}
		})
	}
}
