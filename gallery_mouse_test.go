package main

import (
	"image"
	"math"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// A 120x40 pane: computeLayout gives a known layout we can hit-test against.
func mouseModel(width, height, cursor, nimages int) galleryModel {
	m := galleryModel{
		width:  width,
		height: height,
		cursor: cursor,
		images: make([]imageEntry, nimages),
	}
	m.l = computeLayout(width, height)
	return m
}

func TestRectContains(t *testing.T) {
	r := rect{x: 5, y: 10, w: 4, h: 3}
	cases := []struct {
		x, y int
		want bool
	}{
		{5, 10, true}, {8, 12, true}, {9, 10, false}, {5, 13, false}, {4, 10, false},
	}
	for _, c := range cases {
		if got := r.contains(c.x, c.y); got != c.want {
			t.Errorf("contains(%d,%d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

func TestFilmstripCellRectsFullWindow(t *testing.T) {
	m := mouseModel(120, 40, 0, 50) // many images → full window of stripCols cells
	rects := m.filmstripCellRects()
	if len(rects) != m.l.stripCols {
		t.Fatalf("got %d cells, want stripCols=%d", len(rects), m.l.stripCols)
	}
	cellW := m.l.stripW + 2
	for i := 1; i < len(rects); i++ {
		gap := rects[i].x - (rects[i-1].x + cellW)
		if gap != stripGutter {
			t.Errorf("cell %d gap = %d, want %d", i, gap, stripGutter)
		}
		if rects[i].w != cellW {
			t.Errorf("cell %d width = %d, want %d", i, rects[i].w, cellW)
		}
	}
	// Row sits in the filmstrip band: top = height - stripH - 4.
	if rects[0].y != m.height-m.l.stripH-4 {
		t.Errorf("cell y = %d, want %d", rects[0].y, m.height-m.l.stripH-4)
	}
	if rects[0].h != m.l.stripH+2 {
		t.Errorf("cell height = %d, want %d", rects[0].h, m.l.stripH+2)
	}
}

func TestFilmstripCellRectsPartialWindow(t *testing.T) {
	m := mouseModel(120, 40, 0, 3) // fewer images than stripCols → 3 cells
	if got := len(m.filmstripCellRects()); got != 3 {
		t.Errorf("partial window cells = %d, want 3", got)
	}
}

func TestFilmstripHit(t *testing.T) {
	m := mouseModel(120, 40, 0, 50)
	rects := m.filmstripCellRects()
	mid := rects[2]
	idx, ok := m.filmstripHit(mid.x+1, mid.y+1)
	if !ok || idx != 2 { // cursor 0 → stripStart 0 → cell 2 is image 2
		t.Errorf("hit cell 2 = (%d,%v), want (2,true)", idx, ok)
	}
	cellW := m.l.stripW + 2
	if idx, ok := m.filmstripHit(rects[0].x+1, rects[0].y+1); !ok || idx != 0 {
		t.Errorf("hit cell 0 = (%d,%v), want (0,true)", idx, ok)
	}
	last := len(rects) - 1
	if idx, ok := m.filmstripHit(rects[last].x+1, rects[last].y+1); !ok || idx != last {
		t.Errorf("hit last cell = (%d,%v), want (%d,true)", idx, ok, last)
	}
	if _, ok := m.filmstripHit(rects[0].x+cellW, rects[0].y+1); ok {
		t.Error("click in gutter between cells must not hit any cell")
	}
	if _, ok := m.filmstripHit(mid.x+1, m.height-1); ok {
		t.Error("click in legend must not hit a cell")
	}
}

func TestPreviewRectInsideBand(t *testing.T) {
	m := mouseModel(120, 40, 0, 5)
	pr := m.previewRect()
	if pr.w != m.l.previewW || pr.h != m.l.previewH {
		t.Errorf("preview inner = %dx%d, want %dx%d", pr.w, pr.h, m.l.previewW, m.l.previewH)
	}
	if pr.y < 2 || pr.y+pr.h > m.height-m.l.stripH-4 {
		t.Errorf("preview rect rows %d..%d escape the preview band", pr.y, pr.y+pr.h)
	}
}

func TestOverFilmstripBand(t *testing.T) {
	m := mouseModel(120, 40, 0, 50)
	top := m.height - m.l.stripH - 4
	if !m.overFilmstripBand(top) || !m.overFilmstripBand(top+m.l.stripH+1) {
		t.Error("filmstrip band rows should report true")
	}
	if m.overFilmstripBand(2) || m.overFilmstripBand(m.height-1) {
		t.Error("preview/legend rows should report false")
	}
	if m.overFilmstripBand(top + m.l.stripH + 2) {
		t.Error("row just below the band should be false")
	}
}

func TestPreviewRectDegeneratePane(t *testing.T) {
	m := mouseModel(30, 12, 0, 5) // tiny pane → box can't fit the band
	pr := m.previewRect()
	if pr != (rect{}) {
		t.Errorf("degenerate pane previewRect = %+v, want zero rect", pr)
	}
	if pr.contains(15, 6) {
		t.Error("zero rect must not contain any point")
	}
}

func TestZoomAtKeepsPointStationary(t *testing.T) {
	m := mouseModel(120, 40, 0, 5)
	m.crop = cropFrac{0, 0, 1, 1}
	pr := m.previewRect()
	// Pointer near the top-left quarter of the preview.
	sx, sy := pr.x+pr.w/4, pr.y+pr.h/4
	fx := (float64(sx-pr.x) + 0.5) / float64(pr.w)
	fy := (float64(sy-pr.y) + 0.5) / float64(pr.h)
	wantX := m.crop.x0 + fx*m.crop.w()
	wantY := m.crop.y0 + fy*m.crop.h()
	m.zoomAt(sx, sy, 1.25)
	// The image point under the pointer must map back to the same screen frac.
	gotX := m.crop.x0 + fx*m.crop.w()
	gotY := m.crop.y0 + fy*m.crop.h()
	if !approx(gotX, wantX) || !approx(gotY, wantY) {
		t.Errorf("anchor drifted: got (%v,%v) want (%v,%v)", gotX, gotY, wantX, wantY)
	}
	if m.crop.isFull() {
		t.Error("zoom-in must shrink the crop")
	}
}

func TestZoomAtZoomOutToFull(t *testing.T) {
	m := mouseModel(120, 40, 0, 5)
	m.crop = cropFrac{0.25, 0.25, 0.75, 0.75} // zoomed-in starting state
	pr := m.previewRect()
	sx, sy := pr.x+pr.w/2, pr.y+pr.h/2
	for i := 0; i < 11; i++ {
		m.zoomAt(sx, sy, 1/1.25)
	}
	if !m.crop.isFull() {
		t.Errorf("repeated zoom-out must reach fullCrop, got %+v", m.crop)
	}
}

func TestDragPansWhenZoomed(t *testing.T) {
	m := mouseModel(120, 40, 0, 1)
	m.ready = true
	m.crop = cropFrac{0.25, 0.25, 0.75, 0.75} // zoomed in so panBy is live
	pr := m.previewRect()
	cx, cy := pr.x+pr.w/2, pr.y+pr.h/2
	// Click in the preview starts a drag.
	m2, _ := m.handleMouse(tea.MouseClickMsg{X: cx, Y: cy, Button: tea.MouseLeft})
	if !m2.dragging {
		t.Fatal("click in preview should start dragging")
	}
	before := m2.crop
	// Drag right+down by a few cells → crop must move.
	m3, _ := m2.handleMouse(tea.MouseMotionMsg{X: cx + 5, Y: cy + 3, Button: tea.MouseLeft})
	// Dragging right+down moves the image with the cursor → crop x0/y0 decrease.
	if m3.crop.x0 >= before.x0 || m3.crop.y0 >= before.y0 {
		t.Errorf("rightward/downward drag should decrease crop x0/y0: before %+v got %+v", before, m3.crop)
	}
	// Release ends the drag.
	m4, _ := m3.handleMouse(tea.MouseReleaseMsg{X: cx + 5, Y: cy + 3, Button: tea.MouseLeft})
	if m4.dragging {
		t.Error("release should end dragging")
	}
}

func TestDragIgnoredAtFullCrop(t *testing.T) {
	m := mouseModel(120, 40, 0, 1)
	m.ready = true
	m.crop = fullCrop()
	pr := m.previewRect()
	cx, cy := pr.x+pr.w/2, pr.y+pr.h/2
	m2, _ := m.handleMouse(tea.MouseClickMsg{X: cx, Y: cy, Button: tea.MouseLeft})
	if !m2.dragging {
		t.Fatal("click in preview should start dragging even at full crop")
	}
	before := m2.crop
	m3, _ := m2.handleMouse(tea.MouseMotionMsg{X: cx + 5, Y: cy + 3, Button: tea.MouseLeft})
	if m3.crop != before {
		t.Error("drag at full crop must not pan")
	}
}

func TestHandleMouseFilmstripClick(t *testing.T) {
	m := mouseModel(120, 40, 0, 50)
	m.ready = true
	rects := m.filmstripCellRects()
	cell3 := rects[3]
	m2, _ := m.handleMouse(tea.MouseClickMsg{X: cell3.x + 1, Y: cell3.y + 1, Button: tea.MouseLeft})
	if m2.cursor != 3 {
		t.Errorf("filmstrip click: cursor = %d, want 3", m2.cursor)
	}
}

func TestHandleMouseWheelZoom(t *testing.T) {
	m := mouseModel(120, 40, 0, 5)
	m.ready = true
	m.crop = fullCrop()
	pr := m.previewRect()
	m2, _ := m.handleMouse(tea.MouseWheelMsg{X: pr.x + pr.w/2, Y: pr.y + pr.h/2, Button: tea.MouseWheelUp})
	if m2.crop.isFull() {
		t.Error("wheel up over preview should zoom in (crop no longer full)")
	}
}

func TestHandleMouseWheelFilmstrip(t *testing.T) {
	m := mouseModel(120, 40, 5, 50)
	m.ready = true
	y := m.filmstripCellRects()[0].y
	down, _ := m.handleMouse(tea.MouseWheelMsg{X: 1, Y: y, Button: tea.MouseWheelDown})
	if down.cursor != 6 {
		t.Errorf("wheel down over filmstrip: cursor = %d, want 6", down.cursor)
	}
	up, _ := m.handleMouse(tea.MouseWheelMsg{X: 1, Y: y, Button: tea.MouseWheelUp})
	if up.cursor != 4 {
		t.Errorf("wheel up over filmstrip: cursor = %d, want 4", up.cursor)
	}
}

func TestDragFramesAreThrottledWithATrailingFlush(t *testing.T) {
	// A drag delivers motion faster than a frame costs. Without throttling the work
	// queues up and the image trails the cursor; without the trailing flush the final
	// position of a burst would never paint.
	m := &galleryModel{l: layout{previewW: 20, previewH: 10}}

	m.lastPanAt = time.Time{} // never painted: the first frame must go out immediately
	if cmd := m.transmitPanFrame(); cmd != nil {
		t.Error("first drag frame was throttled; it must paint immediately")
	}

	m.lastPanAt = time.Now() // a frame just went out: the next must be deferred
	cmd := m.transmitPanFrame()
	if cmd == nil {
		t.Fatal("a frame inside panFrameGap painted immediately; it must defer")
	}
	// A tea.Tick Cmd is single-use: its timer channel drains on the first call, so
	// invoking one twice blocks forever. Evaluate exactly once.
	msg := cmd()
	if _, ok := msg.(panFlushMsg); !ok {
		t.Errorf("deferred frame returned %T, want a panFlushMsg so the last position lands", msg)
	}
}

func TestOnlyTheNewestPanFlushSurvives(t *testing.T) {
	m := &galleryModel{l: layout{previewW: 20, previewH: 10}, lastPanAt: time.Now()}
	first, second := m.transmitPanFrame(), m.transmitPanFrame()
	firstMsg, secondMsg := first().(panFlushMsg), second().(panFlushMsg)
	if firstMsg.gen == secondMsg.gen {
		t.Error("two deferred frames share a generation; the stale one would repaint")
	}
	if secondMsg.gen != m.panGen {
		t.Errorf("newest deferred gen = %d, live gen = %d", secondMsg.gen, m.panGen)
	}
}

// geomModel is the pinned geometry fixture: previewRect() resolves to the
// known rect {10,5,100,50} and the default (10x20) cell size gives a 1000x1000
// pixel box. computeLayout(120,70) would NOT reproduce this layout (stripH
// clamps to 12, previewW to 118), so this sets l directly instead of reusing
// mouseModel's computeLayout path. width/height and crop must both be set
// explicitly: without width/height previewRect() returns rect{}, and the zero
// cropFrac{} has h()==0 so displayRatio() is NaN — either way every assertion
// below would silently become a miss instead of a real failure.
func geomModel(srcW, srcH int, crop cropFrac) *galleryModel {
	return &galleryModel{
		width:  120,
		height: 70,
		l:      layout{previewW: 100, previewH: 50, stripH: 8},
		curImg: image.NewRGBA(image.Rect(0, 0, srcW, srcH)),
		crop:   crop,
	}
}

// TestLetterboxSpansMatchRasterAspect asserts through m.displayRatio() (never a
// ratio recomputed by the test — that would let the cellW:7 row pass without the
// shipped code ever reading the measured cell size). The expectation comes from
// an independent code path: cropRaster's actual scaled pixel dims, fit to the
// box the way kitty fits a placement (scale by min(boxW/dstW, boxH/dstH)).
func TestLetterboxSpansMatchRasterAspect(t *testing.T) {
	cases := []struct {
		name         string
		srcW, srcH   int
		crop         cropFrac
		fill         bool // true: crop is overwritten with baseFillCrop() once the model exists
		cellW, cellH int  // 0 = default (10x20) cell size
	}{
		// Crops chosen so crop.w()*srcW and crop.h()*srcH are whole numbers, so
		// cropPixels' rounding can't eat into the tolerance below.
		{"portrait crop", 600, 900, cropFrac{0.25, 0.3, 0.75, 0.7}, false, 0, 0},     // 300x360px -> spanX 0.8333 spanY 1
		{"landscape crop", 1600, 900, cropFrac{0.25, 0.25, 0.75, 0.75}, false, 0, 0}, // 800x450px -> spanX 1 spanY 0.5625
		{"square crop", 800, 800, cropFrac{0.25, 0.25, 0.5, 0.75}, false, 0, 0},      // 200x400px -> spanX 0.5 spanY 1
		{"portrait fill", 600, 900, fullCrop(), true, 0, 0},                          // baseFillCrop -> 600x600px -> spans 1,1
		{"landscape fill", 1600, 900, fullCrop(), true, 0, 0},                        // baseFillCrop -> 900x900px -> spans 1,1
		// Non-square cell: the box becomes 700x750, so boxAspectFrac is 1.4 (not
		// 1.5) and spanX becomes 0.8929 (not 0.8333) — a 0.06 gap against a
		// 1/700 tolerance, 40x over. An implementation hardcoding the
		// cellPxW/cellPxH constants instead of reading cellWpx()/cellHpx() fails
		// this row by that margin.
		{"portrait crop, measured cell size", 600, 900, cropFrac{0.25, 0.3, 0.75, 0.7}, false, 7, 15},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := geomModel(c.srcW, c.srcH, c.crop)
			m.cellW, m.cellH = c.cellW, c.cellH
			if c.fill {
				m.crop = m.baseFillCrop()
			}
			boxW, boxH := m.l.previewW*m.cellWpx(), m.l.previewH*m.cellHpx()
			dst := m.cropRaster(m.curImg, m.l.previewW, m.l.previewH)
			if dst == nil {
				t.Fatal("cropRaster returned nil; this row's crop must not be full")
			}
			dstW, dstH := dst.Bounds().Dx(), dst.Bounds().Dy()
			k := math.Min(float64(boxW)/float64(dstW), float64(boxH)/float64(dstH))
			wantSpanX := float64(dstW) * k / float64(boxW)
			wantSpanY := float64(dstH) * k / float64(boxH)

			spanX, spanY := letterboxSpans(m.displayRatio())
			if tolX := 1 / float64(boxW); math.Abs(spanX-wantSpanX) > tolX {
				t.Errorf("spanX = %v, want %v (tol %v)", spanX, wantSpanX, tolX)
			}
			if tolY := 1 / float64(boxH); math.Abs(spanY-wantSpanY) > tolY {
				t.Errorf("spanY = %v, want %v (tol %v)", spanY, wantSpanY, tolY)
			}
		})
	}
}

// TestCropFillsBox cross-checks the letterbox decision against production code.
// cropFillsBox uses a relative band on the crop's
// own aspect (|w/h - want| < want*1e-3), not the absolute 1-span<=1e-3 band
// checks above — a different formula, so the fixtures here are picked to be
// unambiguous under either one rather than re-deriving cropFillsBox's band from
// letterboxSpans.
func TestCropFillsBox(t *testing.T) {
	cases := []struct {
		name       string
		srcW, srcH int
		fill       bool // true: baseFillCrop (fills by construction); false: fullCrop
		want       bool
	}{
		{"portrait fill crop fills the box", 600, 900, true, true},
		{"landscape fill crop fills the box", 1600, 900, true, true},
		// Full crop on these sources letterboxes by ~33 and ~22 cells respectively
		// — comfortably more than the >=1-cell margin the spec requires, so this
		// is a miss a click could actually land in.
		{"portrait full crop leaves a margin", 600, 900, false, false},
		{"landscape full crop leaves a margin", 1600, 900, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := geomModel(c.srcW, c.srcH, fullCrop())
			if c.fill {
				m.crop = m.baseFillCrop()
			}
			if got := m.cropFillsBox(); got != c.want {
				t.Errorf("cropFillsBox() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestImageFracAtMarginBoundary pins the centering. The hit/miss row
// and column indices are pinned constants computed from the fixture's geometry
// — never discovered by walking imageFracAt's own output, which would validate
// a top-anchored (offY=0) bug just as happily as a correct implementation, so
// long as it produced *some* contiguous hit run.
func TestImageFracAtMarginBoundary(t *testing.T) {
	// Landscape 1600x900, full crop: letterboxed top/bottom, spanY=0.5625,
	// offY=(1-spanY)/2=0.21875. by=(i+0.5)/50:
	//   i=11 -> v=(0.23-0.21875)/0.5625 =  0.020 (hit, just inside the top margin)
	//   i=10 -> v=(0.21-0.21875)/0.5625 = -0.016 (miss, just above the image)
	//   i=38 -> v=(0.77-0.21875)/0.5625 =  0.980 (hit, just inside the bottom margin)
	//   i=39 -> v=(0.79-0.21875)/0.5625 =  1.016 (miss, just below the image)
	landscape := geomModel(1600, 900, fullCrop())
	pr := landscape.previewRect() // {10,5,100,50}
	midX := pr.x + pr.w/2         // spanX=1 here, so x is unconstrained — keep it inside the box
	for _, tc := range []struct {
		row  int
		want bool
	}{{11, true}, {38, true}, {10, false}, {39, false}} {
		if _, _, ok := landscape.imageFracAt(midX, pr.y+tc.row); ok != tc.want {
			t.Errorf("landscape row %d: ok = %v, want %v", tc.row, ok, tc.want)
		}
	}

	// Portrait 600x900, full crop: pillarboxed left/right, spanX=0.6667,
	// offX=(1-spanX)/2=0.16667. bx=(i+0.5)/100:
	//   i=17 -> u=(0.175-0.16667)/0.6667 =  0.0125 (hit)
	//   i=16 -> u=(0.165-0.16667)/0.6667 = -0.0025 (miss)
	//   i=82 -> u=(0.825-0.16667)/0.6667 =  0.9875 (hit)
	//   i=83 -> u=(0.835-0.16667)/0.6667 =  1.0025 (miss)
	portrait := geomModel(600, 900, fullCrop())
	pr = portrait.previewRect()
	midY := pr.y + pr.h/2 // spanY=1 here, so y is unconstrained
	for _, tc := range []struct {
		col  int
		want bool
	}{{17, true}, {82, true}, {16, false}, {83, false}} {
		if _, _, ok := portrait.imageFracAt(pr.x+tc.col, midY); ok != tc.want {
			t.Errorf("portrait col %d: ok = %v, want %v", tc.col, ok, tc.want)
		}
	}
}

// TestImageFracAtFrameRegionInverse checks imageFracAt inverts
// frameRegion's crop for all three of its branches. Regions are pinned
// off-centre deliberately — a centred region's crop composes so the
// centre point lands "inside" even if the inverse ignored the crop entirely,
// which would make the assertion pass for a broken implementation.
func TestImageFracAtFrameRegionInverse(t *testing.T) {
	const srcW, srcH = 600, 900 // targetFrac = boxAspectFrac(600,900,1000,1000) = 1.5
	cases := []struct {
		name string
		r    region
	}{
		// rw=0.22,rh=0.88; rw/rh=0.25<1.5 so cropW=rh*1.5=1.32>1 -> clamped to
		// rw=0.22 (the cropW>1 overflow branch). Neither x0 nor y0 clamps to an
		// edge (crop {0.29,0.06,0.51,0.94}).
		{"overflow (cropW>1 clamp)", region{x0: 0.3, y0: 0.1, x1: 0.5, y1: 0.9}},
		// rw=0.22,rh=0.088; rw/rh=2.5>1.5 so cropH=rw/1.5 (the fill-height branch).
		{"fill-height", region{x0: 0.55, y0: 0.6, x1: 0.75, y1: 0.68}},
		// rw=0.11,rh=0.33; rw/rh=0.333<1.5 so cropW=rh*1.5, cropW<=1 (fill-width).
		{"fill-width", region{x0: 0.55, y0: 0.5, x1: 0.65, y1: 0.8}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := geomModel(srcW, srcH, fullCrop())
			boxW, boxH := m.l.previewW*m.cellWpx(), m.l.previewH*m.cellHpx()
			m.crop = frameRegion(c.r, srcW, srcH, boxW, boxH)
			pr := m.previewRect()
			fx, fy, ok := m.imageFracAt(pr.x+pr.w/2, pr.y+pr.h/2)
			if !ok {
				t.Fatal("preview center must land on the image, not a letterbox margin")
			}
			if fx < c.r.x0 || fx > c.r.x1 || fy < c.r.y0 || fy > c.r.y1 {
				t.Errorf("center (%.4f,%.4f) outside framed region %+v", fx, fy, c.r)
			}
		})
	}
}

// diagramModel is the click-behaviour fixture: a diagram entry (Vector set, so
// regionClick's curVector guard passes) on the geomModel layout, with
// m.regions pre-set directly so ensureRegions no-ops instead of trying to
// parse a real SVG (fixture gotcha, not what these tests exercise). Two root
// regions, well separated: "C" is a container with one child "C.leaf"; "L" is
// a leaf. Both are large enough that a one-cell pan during a drag can't push a
// hit point out of its region (the zoomed drag row relies on this).
func diagramModel(srcW, srcH int, crop cropFrac, regionPath []string, regionIdx int) *galleryModel {
	m := geomModel(srcW, srcH, crop)
	m.ready = true
	m.l.stripW, m.l.stripCols = stripThumbW, 5 // the cross-press rows need a real filmstrip cell
	m.images = []imageEntry{{Vector: "diagram.svg"}}
	m.regions = newRegionTree([]region{
		{path: "C", x0: 0.1, y0: 0.1, x1: 0.5, y1: 0.5},
		{path: "C.leaf", x0: 0.15, y0: 0.15, x1: 0.45, y1: 0.45},
		{path: "L", x0: 0.6, y0: 0.6, x1: 0.9, y1: 0.9},
	})
	m.regionPath, m.regionIdx = regionPath, regionIdx
	return m
}

// TestRegionClickTable exercises regionClick directly. All
// fixtures use a square 1000x1000 source against the 1000x1000 default box, so
// ratio==1 and there is no letterboxing to compose except in the margin row —
// screen fraction bx/by then equals image fraction fx/fy directly under a full
// crop, which keeps the arithmetic in each row's comment checkable by hand.
func TestRegionClickTable(t *testing.T) {
	t.Run("container click drills", func(t *testing.T) {
		m := diagramModel(1000, 1000, fullCrop(), nil, -1)
		// i=29,j=14 -> fx=0.295,fy=0.29: inside C=[0.1,0.5]^2 only.
		if !m.regionClick(10+29, 5+14) {
			t.Fatal("click on a container reported no change")
		}
		if !slices.Equal(m.regionPath, []string{"C"}) || m.regionIdx != 0 {
			t.Errorf("regionPath=%v regionIdx=%d, want [C] 0", m.regionPath, m.regionIdx)
		}
	})

	t.Run("leaf click focuses without drilling", func(t *testing.T) {
		m := diagramModel(1000, 1000, fullCrop(), nil, -1)
		// i=74,j=37 -> fx=0.745,fy=0.75: inside L=[0.6,0.9]^2 only; L has no children.
		if !m.regionClick(10+74, 5+37) {
			t.Fatal("click on a leaf reported no change")
		}
		if len(m.regionPath) != 0 || m.regionIdx != 1 {
			t.Errorf("regionPath=%v regionIdx=%d, want [] 1", m.regionPath, m.regionIdx)
		}
	})

	t.Run("empty space inside the image steps back", func(t *testing.T) {
		m := diagramModel(1000, 1000, fullCrop(), []string{"C"}, 0) // drilled into C, focused on C.leaf
		// i=1,j=0 -> fx=0.015,fy=0.01: on the image, outside C.leaf=[0.15,0.45]^2.
		if !m.regionClick(10+1, 5+0) {
			t.Fatal("empty-space click while drilled in reported no change")
		}
		if len(m.regionPath) != 0 || m.regionIdx != 0 {
			t.Errorf("regionPath=%v regionIdx=%d, want [] 0 (back to C at root)", m.regionPath, m.regionIdx)
		}
	})

	t.Run("letterbox margin click also steps back", func(t *testing.T) {
		// Landscape source: full crop letterboxes row 0 (see the i=10 miss in
		// TestImageFracAtMarginBoundary,
		// deeper still) — imageFracAt reports ok=false, a branch no imageFracAt-
		// level test alone covers since it never calls regionClick.
		m := diagramModel(1600, 900, fullCrop(), []string{"C"}, 0)
		if !m.regionClick(10+50, 5+0) {
			t.Fatal("margin click while drilled in reported no change")
		}
		if len(m.regionPath) != 0 || m.regionIdx != 0 {
			t.Errorf("regionPath=%v regionIdx=%d, want [] 0", m.regionPath, m.regionIdx)
		}
	})

	t.Run("empty space at root in region mode exits", func(t *testing.T) {
		m := diagramModel(1000, 1000, fullCrop(), nil, 0) // regionIdx 0: the zero value already means "in region mode"
		// i=54,j=25 -> fx=0.545,fy=0.51: the gap between C and L.
		if !m.regionClick(10+54, 5+25) {
			t.Fatal("empty-space click at root reported no change")
		}
		if m.regionIdx != -1 || len(m.regionPath) != 0 {
			t.Errorf("regionIdx=%d regionPath=%v, want -1 [] (exited)", m.regionIdx, m.regionPath)
		}
	})

	t.Run("whitespace click outside region mode leaves crop alone", func(t *testing.T) {
		zoomed := cropFrac{0.2, 0.2, 0.8, 0.8}
		m := diagramModel(1000, 1000, zoomed, nil, -1)
		// i=58,j=25 -> fx=0.551,fy=0.506 composed through the zoomed crop: the
		// same C/L gap, now reached via a wheel-zoomed crop instead of full.
		if m.regionClick(10+58, 5+25) {
			t.Error("whitespace click outside region mode reported a change")
		}
		if m.crop != zoomed {
			t.Errorf("crop = %+v, want unchanged %+v", m.crop, zoomed)
		}
	})
}

// TestRegionClickViaDrag checks click-vs-drag classification through the
// full handleMouse press/motion/release cycle. Every row that expects "drills
// nothing" releases on a point that IS a hittable region (the same one
// TestRegionClickTable's first row drills on) — an implementation that never
// sets dragMoved would drill here too, which is what makes the row falsify it
// instead of passing vacuously.
func TestRegionClickViaDrag(t *testing.T) {
	press := func(m *galleryModel, x, y int) *galleryModel {
		m2, _ := m.handleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		return &m2
	}
	motion := func(m *galleryModel, x, y int) *galleryModel {
		m2, _ := m.handleMouse(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})
		return &m2
	}
	release := func(m *galleryModel, x, y int) *galleryModel {
		m2, _ := m.handleMouse(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
		return &m2
	}

	t.Run("drag over a hittable region at full crop drills nothing", func(t *testing.T) {
		m := diagramModel(1000, 1000, fullCrop(), nil, -1)
		m = press(m, 39, 19)   // i=29,j=14 -> fx=0.295,fy=0.29, inside C
		m = motion(m, 40, 19)  // one cell: dragMoved is set outside the isFull guard
		m = release(m, 39, 19) // release back on the same hittable point
		if len(m.regionPath) != 0 || m.regionIdx != -1 {
			t.Errorf("regionPath=%v regionIdx=%d, want unchanged [] -1", m.regionPath, m.regionIdx)
		}
	})

	t.Run("drag over a hittable region while zoomed drills nothing", func(t *testing.T) {
		m := diagramModel(1000, 1000, cropFrac{0.2, 0.2, 0.8, 0.8}, nil, -1)
		// i=16,j=8 -> fx=0.299,fy=0.302 through the zoomed crop, inside C. panBy
		// compensates the 1-cell x motion exactly, so the same screen point still
		// maps inside C after the pan — crop moves, but region state must not.
		m = press(m, 26, 13)
		m = motion(m, 27, 13)
		m = release(m, 27, 13)
		if len(m.regionPath) != 0 || m.regionIdx != -1 {
			t.Errorf("regionPath=%v regionIdx=%d, want unchanged [] -1", m.regionPath, m.regionIdx)
		}
	})

	t.Run("press-release with no motion drills", func(t *testing.T) {
		m := diagramModel(1000, 1000, fullCrop(), nil, -1)
		m = press(m, 39, 19)
		m = release(m, 39, 19)
		if !slices.Equal(m.regionPath, []string{"C"}) || m.regionIdx != 0 {
			t.Errorf("regionPath=%v regionIdx=%d, want [C] 0", m.regionPath, m.regionIdx)
		}
	})

	t.Run("a click after a drag still drills (dragMoved clears on press)", func(t *testing.T) {
		m := diagramModel(1000, 1000, fullCrop(), nil, -1)
		m = press(m, 39, 19)
		m = motion(m, 40, 19)
		m = release(m, 39, 19) // the drag itself: drills nothing
		if len(m.regionPath) != 0 {
			t.Fatalf("fixture bug: the drag itself drilled, regionPath=%v", m.regionPath)
		}
		m = press(m, 39, 19)
		m = release(m, 39, 19) // a plain click on the same model
		if !slices.Equal(m.regionPath, []string{"C"}) || m.regionIdx != 0 {
			t.Errorf("regionPath=%v regionIdx=%d, want [C] 0", m.regionPath, m.regionIdx)
		}
	})

	t.Run("press on filmstrip, release in preview drills nothing", func(t *testing.T) {
		m := diagramModel(1000, 1000, fullCrop(), nil, -1)
		cell := m.filmstripCellRects()[0]
		m = press(m, cell.x+1, cell.y+1) // filmstripHit wins over the preview branch: dragging never starts
		m = release(m, 39, 19)           // otherwise a hittable preview point
		if len(m.regionPath) != 0 || m.regionIdx != -1 {
			t.Errorf("regionPath=%v regionIdx=%d, want unchanged [] -1", m.regionPath, m.regionIdx)
		}
	})

	t.Run("press in preview, release over filmstrip drills nothing", func(t *testing.T) {
		m := diagramModel(1000, 1000, fullCrop(), nil, -1)
		cell := m.filmstripCellRects()[0]
		m = press(m, 39, 19)
		m = release(m, cell.x+1, cell.y+1) // outside previewRect: the release guard fails
		if len(m.regionPath) != 0 || m.regionIdx != -1 {
			t.Errorf("regionPath=%v regionIdx=%d, want unchanged [] -1", m.regionPath, m.regionIdx)
		}
	})
}

// TestRegionClickGuards checks the guards. The first two rows assert the guarded state
// is left exactly as it was (not merely "no panic"): a non-diagram image or an
// undecoded one must not step back a level or reset the zoom just because a
// click landed in the preview.
func TestRegionClickGuards(t *testing.T) {
	t.Run("non-diagram image is inert", func(t *testing.T) {
		m := diagramModel(1000, 1000, cropFrac{0.3, 0.3, 0.6, 0.6}, []string{"C"}, 2)
		m.images[0].Vector = "" // curVector() == ""
		wantCrop, wantPath, wantIdx := m.crop, slices.Clone(m.regionPath), m.regionIdx
		if m.regionClick(60, 30) {
			t.Error("regionClick on a non-diagram entry reported a change")
		}
		if m.crop != wantCrop || !slices.Equal(m.regionPath, wantPath) || m.regionIdx != wantIdx {
			t.Errorf("state mutated: crop=%+v path=%v idx=%d", m.crop, m.regionPath, m.regionIdx)
		}
	})

	t.Run("nil curImg is inert", func(t *testing.T) {
		m := diagramModel(1000, 1000, cropFrac{0.3, 0.3, 0.6, 0.6}, []string{"C"}, 2)
		m.curImg = nil
		wantCrop, wantPath, wantIdx := m.crop, slices.Clone(m.regionPath), m.regionIdx
		if m.regionClick(60, 30) {
			t.Error("regionClick with no decoded image reported a change")
		}
		if m.crop != wantCrop || !slices.Equal(m.regionPath, wantPath) || m.regionIdx != wantIdx {
			t.Errorf("state mutated: crop=%+v path=%v idx=%d", m.crop, m.regionPath, m.regionIdx)
		}
	})

	t.Run("degenerate pane does not panic", func(t *testing.T) {
		m := mouseModel(30, 12, 0, 5) // TestPreviewRectDegeneratePane: previewRect() == rect{} here
		m.images[0].Vector = "diagram.svg"
		m.curImg = image.NewRGBA(image.Rect(0, 0, 100, 100))
		m.regions = newRegionTree([]region{{path: "C", x0: 0, y0: 0, x1: 1, y1: 1}})
		m.regionIdx = 0 // >=0: reaches the step-back branch, which is unreachable through handleMouse
		if pr := m.previewRect(); pr != (rect{}) {
			t.Fatalf("fixture bug: previewRect = %+v, want zero rect", pr)
		}
		m.regionClick(0, 0) // must not panic
	})
}
