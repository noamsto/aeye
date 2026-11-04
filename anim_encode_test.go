package main

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func animTestImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 7, 255})
		}
	}
	return img
}

func animClone(img *image.RGBA) *image.RGBA {
	c := *img
	c.Pix = bytes.Clone(img.Pix)
	return &c
}

func animPoke(img *image.RGBA, pts ...image.Point) {
	for _, p := range pts {
		img.SetRGBA(p.X, p.Y, color.RGBA{250, 250, 250, 255})
	}
}

func TestDiffRects(t *testing.T) {
	tests := []struct {
		name string
		w, h int
		pts  []image.Point
		want []image.Rectangle
	}{
		{"identical", 64, 48, nil, nil},
		{"one pixel", 64, 48, []image.Point{{20, 5}}, []image.Rectangle{image.Rect(16, 0, 32, 16)}},
		{"two separated", 64, 48, []image.Point{{1, 1}, {50, 40}},
			[]image.Rectangle{image.Rect(0, 0, 16, 16), image.Rect(48, 32, 64, 48)}},
		{"horizontal run", 64, 48, []image.Point{{1, 1}, {17, 1}, {33, 1}},
			[]image.Rectangle{image.Rect(0, 0, 48, 16)}},
		{"stacked identical runs", 64, 48, []image.Point{{1, 1}, {17, 1}, {1, 17}, {17, 17}},
			[]image.Rectangle{image.Rect(0, 0, 32, 32)}},
		{"stacked different runs", 64, 48, []image.Point{{1, 1}, {17, 1}, {1, 17}},
			[]image.Rectangle{image.Rect(0, 0, 32, 16), image.Rect(0, 16, 16, 32)}},
		{"clamped edge", 40, 20, []image.Point{{39, 19}},
			[]image.Rectangle{image.Rect(32, 16, 40, 20)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := animTestImage(tc.w, tc.h)
			c := animClone(b)
			animPoke(c, tc.pts...)
			got := diffRects(b, c)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestEncodeAnimPartial(t *testing.T) {
	f0 := animTestImage(64, 48)
	f1 := animClone(f0)
	animPoke(f1, image.Pt(20, 5))
	f2 := animClone(f0)

	blob, p := encodeAnim([]*image.RGBA{f0, f1, f2}, false)
	if p.W != 64 || p.H != 48 || len(p.Frames) != 2 {
		t.Fatalf("plan = %+v", p)
	}
	fr := p.Frames[0]
	if fr.Full || len(fr.Rects) != 1 {
		t.Fatalf("frame 1 = %+v", fr)
	}
	r := fr.Rects[0]
	if r.X != 16 || r.Y != 0 || r.W != 16 || r.H != 16 || r.Size != 16*16*4 {
		t.Fatalf("rect = %+v", r)
	}
	for y := 0; y < r.H; y++ {
		got := blob[r.Off+int64(y*r.W*4) : r.Off+int64((y+1)*r.W*4)]
		o := f1.PixOffset(r.X, r.Y+y)
		if !bytes.Equal(got, f1.Pix[o:o+r.W*4]) {
			t.Fatalf("row %d mismatch", y)
		}
	}

	fr = p.Frames[1]
	if fr.Full || len(fr.Rects) != 1 {
		t.Fatalf("identical frame = %+v", fr)
	}
	r = fr.Rects[0]
	if r.X != 0 || r.Y != 0 || r.W != 1 || r.H != 1 || r.Size != 4 {
		t.Fatalf("identical rect = %+v", r)
	}
	if !bytes.Equal(blob[r.Off:r.Off+4], f0.Pix[:4]) {
		t.Fatal("identical frame pixel not copied from frame 0")
	}
}

func TestEncodeAnimFullFallback(t *testing.T) {
	f0 := animTestImage(64, 48)

	// 7 of 12 tiles is over half the area.
	big := animClone(f0)
	var pts []image.Point
	for i := 0; i < 7; i++ {
		pts = append(pts, image.Pt(i%4*16+1, i/4*16+1))
	}
	animPoke(big, pts...)
	_, p := encodeAnim([]*image.RGBA{f0, big}, false)
	assertFullFrame(t, p.Frames[0], 64, 48)

	// More than animMaxRects disjoint tiles, well under half the area.
	w, h := 16*40, 16*40
	g0 := animTestImage(w, h)
	many := animClone(g0)
	pts = nil
	for ty := 0; ty < 40 && len(pts) <= animMaxRects; ty += 2 {
		for tx := 0; tx < 40 && len(pts) <= animMaxRects; tx += 2 {
			pts = append(pts, image.Pt(tx*16+1, ty*16+1))
		}
	}
	animPoke(many, pts...)
	_, p = encodeAnim([]*image.RGBA{g0, many}, false)
	assertFullFrame(t, p.Frames[0], w, h)

	small := animClone(f0)
	animPoke(small, image.Pt(1, 1))
	_, p = encodeAnim([]*image.RGBA{f0, small, small}, true)
	for _, fr := range p.Frames {
		assertFullFrame(t, fr, 64, 48)
	}
}

func assertFullFrame(t *testing.T, fr animFrame, w, h int) {
	t.Helper()
	if !fr.Full || len(fr.Rects) != 1 {
		t.Fatalf("frame = %+v, want full", fr)
	}
	r := fr.Rects[0]
	if r.X != 0 || r.Y != 0 || r.W != w || r.H != h || r.Size != int64(w*h*4) {
		t.Fatalf("rect = %+v", r)
	}
}

func TestAnimFrameCount(t *testing.T) {
	for _, tc := range []struct{ w, h, want int }{
		{1000, 1000, 12},
		{1700, 1600, 9},
		{2000, 2000, 0},
	} {
		if got := animFrameCount(tc.w, tc.h); got != tc.want {
			t.Errorf("animFrameCount(%d,%d) = %d, want %d", tc.w, tc.h, got, tc.want)
		}
	}
}
