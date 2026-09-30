package main

import (
	"image"
	"image/color"
	"testing"
)

func TestWorkingScale(t *testing.T) {
	tests := []struct {
		name       string
		src        image.Point
		boxW, boxH int
		want       float64
	}{
		{"typical screenshot fits", image.Pt(3456, 2234), 1220, 814, 1},
		{"huge image on both axes is capped", image.Pt(16000, 9000), 1220, 814, zoomMax * max(1220.0/16000, 814.0/9000)},
		{"tall page keeps full resolution", image.Pt(1440, 14000), 2000, 1000, 1},
		{"zero box", image.Pt(3456, 2234), 0, 0, 1},
		{"zero box width", image.Pt(3456, 2234), 0, 814, 1},
		{"unknown source", image.Point{}, 1220, 814, 1},
		{"negative box", image.Pt(3456, 2234), -1, 814, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := workingScale(tc.src, tc.boxW, tc.boxH); !approx(got, tc.want) {
				t.Errorf("workingScale(%v, %d, %d) = %v, want %v", tc.src, tc.boxW, tc.boxH, got, tc.want)
			}
		})
	}
}

// workingDims is the size workingCopy would allocate, computed without allocating.
func workingDims(src image.Point, s float64) image.Rectangle {
	if s >= 1 {
		return image.Rectangle{Max: src}
	}
	return image.Rect(0, 0, max(1, int(float64(src.X)*s)), max(1, int(float64(src.Y)*s)))
}

// Fill on an extreme aspect is the case a min()-based cap would break: the crop
// is a thin band whose source-pixel width must survive the working copy.
func TestFillKeepsSourcePixelRatioOnExtremeAspect(t *testing.T) {
	src := image.Pt(1440, 14000)
	m := &galleryModel{
		curSize: src,
		l:       layout{previewW: 200, previewH: 50},
		cellW:   10,
		cellH:   20,
		crop:    fullCrop(),
	}
	m.toggleFill()
	if m.crop.isFull() {
		t.Fatal("fill crop must be a band, not the full image")
	}

	s := workingScale(src, m.l.previewW*m.cellWpx(), m.l.previewH*m.cellHpx())
	inSrc := cropPixels(image.Rectangle{Max: src}, m.crop)
	inWork := cropPixels(workingDims(src, s), m.crop)
	if inWork.Dx() != inSrc.Dx() || inWork.Dy() != inSrc.Dy() {
		t.Errorf("crop is %v px on the working copy, %v on the source (s=%v)", inWork.Size(), inSrc.Size(), s)
	}
}

// The derivation, not the formula: for every crop whose longer side is at least
// 1/zoomMax, the working copy is at least as sharp as the render the box can
// show, s >= min(1, rx/w, ry/h).
func TestWorkingScaleCoversEveryNonRegionCrop(t *testing.T) {
	// hash is an inline integer mix (splitmix64 finaliser) so the sweep is
	// deterministic without math/rand.
	hash := func(v uint64) uint64 {
		v += 0x9e3779b97f4a7c15
		v = (v ^ v>>30) * 0xbf58476d1ce4e5b9
		v = (v ^ v>>27) * 0x94d049bb133111eb
		return v ^ v>>31
	}
	checked := 0
	for i := range uint64(400) {
		src := image.Pt(1+int(hash(i*4)%20000), 1+int(hash(i*4+1)%20000))
		boxW, boxH := 1+int(hash(i*4+2)%4000), 1+int(hash(i*4+3)%3000)
		rx, ry := float64(boxW)/float64(src.X), float64(boxH)/float64(src.Y)
		s := workingScale(src, boxW, boxH)
		for j := range uint64(64) {
			w := float64(1+hash(i<<8|j)%1000) / 1000
			h := float64(1+hash(i<<8|j|1<<20)%1000) / 1000
			if max(w, h) < 1/zoomMax {
				continue
			}
			checked++
			if need := min(1, rx/w, ry/h); s < need-1e-9 {
				t.Fatalf("src %v box %dx%d crop %.3fx%.3f: s=%v < needed %v", src, boxW, boxH, w, h, s, need)
			}
		}
	}
	if checked < 5000 {
		t.Fatalf("only %d crops exercised", checked)
	}
}

func TestWorkingCopy(t *testing.T) {
	t.Run("NRGBA64 narrows to RGBA", func(t *testing.T) {
		src := image.NewNRGBA64(image.Rect(0, 0, 4, 3))
		src.SetNRGBA64(1, 2, color.NRGBA64{R: 0xffff, G: 0x8080, B: 0, A: 0xffff})
		got, ok := workingCopy(src, 1).(*image.RGBA)
		if !ok {
			t.Fatalf("got %T, want *image.RGBA", workingCopy(src, 1))
		}
		if got.Bounds() != src.Bounds() {
			t.Errorf("bounds = %v, want %v", got.Bounds(), src.Bounds())
		}
		if px := got.RGBAAt(1, 2); px != (color.RGBA{R: 0xff, G: 0x80, B: 0, A: 0xff}) {
			t.Errorf("pixel = %v", px)
		}
	})
	t.Run("RGBA64 narrows to RGBA", func(t *testing.T) {
		src := image.NewRGBA64(image.Rect(2, 3, 6, 6))
		got, ok := workingCopy(src, 1).(*image.RGBA)
		if !ok {
			t.Fatalf("got %T, want *image.RGBA", workingCopy(src, 1))
		}
		if got.Bounds() != src.Bounds() {
			t.Errorf("bounds = %v, want %v", got.Bounds(), src.Bounds())
		}
	})
	t.Run("Gray16 narrows to Gray", func(t *testing.T) {
		src := image.NewGray16(image.Rect(0, 0, 4, 3))
		src.SetGray16(3, 1, color.Gray16{Y: 0xffff})
		got, ok := workingCopy(src, 1).(*image.Gray)
		if !ok {
			t.Fatalf("got %T, want *image.Gray", workingCopy(src, 1))
		}
		if got.Bounds() != src.Bounds() {
			t.Errorf("bounds = %v, want %v", got.Bounds(), src.Bounds())
		}
		if px := got.GrayAt(3, 1); px.Y != 0xff {
			t.Errorf("pixel = %v", px)
		}
	})
	t.Run("8-bit and YCbCr are returned as-is", func(t *testing.T) {
		for _, src := range []image.Image{
			image.NewRGBA(image.Rect(0, 0, 4, 3)),
			image.NewYCbCr(image.Rect(0, 0, 4, 3), image.YCbCrSubsampleRatio420),
		} {
			if got := workingCopy(src, 1); got != src {
				t.Errorf("%T was copied, want the same value", src)
			}
		}
	})
	t.Run("scaling yields an RGBA of the scaled size", func(t *testing.T) {
		for _, src := range []image.Image{
			image.NewNRGBA64(image.Rect(0, 0, 8, 6)),
			image.NewRGBA(image.Rect(0, 0, 8, 6)),
		} {
			got, ok := workingCopy(src, 0.5).(*image.RGBA)
			if !ok {
				t.Fatalf("%T: got %T, want *image.RGBA", src, workingCopy(src, 0.5))
			}
			if want := image.Rect(0, 0, 4, 3); got.Bounds() != want {
				t.Errorf("%T: bounds = %v, want %v", src, got.Bounds(), want)
			}
		}
	})
	t.Run("scaled size never collapses to zero", func(t *testing.T) {
		got := workingCopy(image.NewRGBA(image.Rect(0, 0, 4, 3)), 0.01)
		if got.Bounds().Dx() < 1 || got.Bounds().Dy() < 1 {
			t.Errorf("bounds = %v", got.Bounds())
		}
	})
}
