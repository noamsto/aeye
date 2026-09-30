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
// axis, the letterbox shortfall on the other. Computed from the source size and
// box pixels alone, so it checks the crop against what the terminal shows rather
// than against zoomBy's own aspect predicate.
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
							requested *= 1.25
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

func TestZoomOutFromFillCeilingRetraces(t *testing.T) {
	m, frac := extremeAspectModel(t)
	for range 40 {
		pressZoom(&m, 'z')
	}
	before := zoomMagnification(m.crop, frac)
	pressZoom(&m, 'Z')
	if got := before / zoomMagnification(m.crop, frac); math.Abs(got-1.25) > 1e-9 {
		t.Fatalf("first zoom-out divided magnification by %.6f, want 1.25", got)
	}
	for range 60 {
		pressZoom(&m, 'Z')
	}
	if !m.crop.isFull() {
		t.Fatalf("zoom-out never reached the full image: %+v", m.crop)
	}
}
