package main

import (
	"bytes"
	"image"
)

const (
	animTile        = 16
	animBudgetBytes = 96 << 20
	animMaxRects    = 64
)

type animRect struct {
	X, Y, W, H int
	Off, Size  int64
}

// animFrame is one loop frame; Full means Rects holds the single whole-frame rect.
type animFrame struct {
	Full  bool
	Rects []animRect
}

// animPlan.Frames[i] is loop frame i+1; frame 0 is the root and not listed.
type animPlan struct {
	W, H   int
	Frames []animFrame
}

// animSet names the root raw RGBA file and the rect blob file of one framing.
type animSet struct {
	Root, Blob string
	Plan       animPlan
}

// animFrameCount returns how many frames fit the RGBA budget, or 0 when fewer
// than animFrameMin would, meaning the diagram should stay static.
func animFrameCount(boxW, boxH int) int {
	n := min(animFrameMax, animBudgetBytes/(boxW*boxH*4))
	if n < animFrameMin {
		return 0
	}
	return n
}

// diffRects compares base and cur on an animTile grid and returns the changed
// area as rectangles in top-to-bottom, left-to-right order.
func diffRects(base, cur *image.RGBA) []image.Rectangle {
	b := base.Bounds()
	rects := []image.Rectangle{}
	for ty := b.Min.Y; ty < b.Max.Y; ty += animTile {
		y1 := min(ty+animTile, b.Max.Y)
		rowStart := len(rects)
		runStart := -1
		for tx := b.Min.X; tx < b.Max.X+animTile; tx += animTile {
			changed := tx < b.Max.X && tileDiffers(base, cur, tx, ty, min(tx+animTile, b.Max.X), y1)
			if changed && runStart < 0 {
				runStart = tx
			}
			if !changed && runStart >= 0 {
				run := image.Rect(runStart, ty, min(tx, b.Max.X), y1)
				runStart = -1
				if i := mergeAbove(rects[:rowStart], run); i >= 0 {
					rects[i].Max.Y = y1
					continue
				}
				rects = append(rects, run)
			}
		}
	}
	return rects
}

// mergeAbove returns the index in above of the rect that run extends
// vertically, or -1.
func mergeAbove(above []image.Rectangle, run image.Rectangle) int {
	for i, r := range above {
		if r.Min.X == run.Min.X && r.Max.X == run.Max.X && r.Max.Y == run.Min.Y {
			return i
		}
	}
	return -1
}

func tileDiffers(base, cur *image.RGBA, x0, y0, x1, y1 int) bool {
	for y := y0; y < y1; y++ {
		o := base.PixOffset(x0, y)
		p := cur.PixOffset(x0, y)
		n := (x1 - x0) * 4
		if !bytes.Equal(base.Pix[o:o+n], cur.Pix[p:p+n]) {
			return true
		}
	}
	return false
}

// encodeAnim diffs every frame against frames[0] and packs the changed
// rectangles' RGBA rows into one blob. A frame with too much change, or any
// frame when forceFull is set, is stored whole.
func encodeAnim(frames []*image.RGBA, forceFull bool) (blob []byte, p animPlan) {
	b := frames[0].Bounds()
	p = animPlan{W: b.Dx(), H: b.Dy()}
	blob = []byte{}
	add := func(img *image.RGBA, r image.Rectangle) animRect {
		off := int64(len(blob))
		for y := r.Min.Y; y < r.Max.Y; y++ {
			o := img.PixOffset(r.Min.X, y)
			blob = append(blob, img.Pix[o:o+r.Dx()*4]...)
		}
		return animRect{r.Min.X, r.Min.Y, r.Dx(), r.Dy(), off, int64(len(blob)) - off}
	}
	for _, cur := range frames[1:] {
		rects := diffRects(frames[0], cur)
		area := 0
		for _, r := range rects {
			area += r.Dx() * r.Dy()
		}
		switch {
		case forceFull || area*2 > p.W*p.H || len(rects) > animMaxRects:
			p.Frames = append(p.Frames, animFrame{Full: true, Rects: []animRect{add(cur, b)}})
		case len(rects) == 0:
			p.Frames = append(p.Frames, animFrame{Rects: []animRect{add(frames[0], image.Rect(0, 0, 1, 1))}})
		default:
			f := animFrame{}
			for _, r := range rects {
				f.Rects = append(f.Rects, add(cur, r))
			}
			p.Frames = append(p.Frames, f)
		}
	}
	return blob, p
}
