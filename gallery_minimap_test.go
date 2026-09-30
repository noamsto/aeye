package main

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestMinimapFit(t *testing.T) {
	box := image.Pt(180, 200)
	tests := []struct {
		name     string
		src, box image.Point
		want     image.Point
	}{
		// scale = min(180/1000, 200/250) = 0.18 -> 180 x 45.
		{"wide letterbox", image.Pt(1000, 250), box, image.Pt(180, 45)},
		// scale = min(180/250, 200/1000) = 0.2 -> 50 x 200.
		{"tall letterbox", image.Pt(250, 1000), box, image.Pt(50, 200)},
		// scale = 0.045 -> 180 x 4.5, which rounds half away from zero to 5.
		{"extreme wide", image.Pt(4000, 100), box, image.Pt(180, 5)},
		// scale = 0.05 -> 4.5 x 200 -> 5 x 200.
		{"extreme tall", image.Pt(100, 4000), box, image.Pt(5, 200)},
		{"zero src", image.Pt(0, 100), box, image.Point{}},
		{"zero box", image.Pt(100, 100), image.Pt(180, 0), image.Point{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := minimapFit(tt.src, tt.box); got != tt.want {
				t.Errorf("minimapFit(%v, %v) = %v, want %v", tt.src, tt.box, got, tt.want)
			}
		})
	}
}

func TestMinimapRect(t *testing.T) {
	tests := []struct {
		name string
		fit  image.Point
		crop cropFrac
		want image.Rectangle
	}{
		// 0.25*180=45 .. 0.5*180=90 across; full height 45.
		{"wide letterbox", image.Pt(180, 45), cropFrac{0.25, 0, 0.5, 1}, image.Rect(45, 0, 90, 45)},
		// 0.5*200=100 .. 0.75*200=150 down; full width 50.
		{"tall letterbox", image.Pt(50, 200), cropFrac{0, 0.5, 1, 0.75}, image.Rect(0, 100, 50, 150)},
		// x: 0.4375*180=78.75 -> 79, width 0.125*180=22.5 -> 23; height 5 < 6 but fit.Y is 5, so it spans full.
		{"extreme wide", image.Pt(180, 5), cropFrac{0.4375, 0, 0.5625, 1}, image.Rect(79, 0, 102, 5)},
		// y: 0.4375*200=87.5 -> 88, height 0.125*200=25; width 5 < 6 but fit.X is 5, so it spans full.
		{"extreme tall", image.Pt(5, 200), cropFrac{0, 0.4375, 1, 0.5625}, image.Rect(0, 88, 5, 113)},
		// 1.8 px -> 2 px on each axis; grown to 6 around the centre round(0.5*180)=90 -> 87..93.
		{"min-side widening", image.Pt(180, 180), cropFrac{0.495, 0.495, 0.505, 0.505}, image.Rect(87, 87, 93, 93)},
		// 2 px at the origin; centre round(0.9)=1, so 1-3=-2 clamps to 0 and the rect is 0..6.
		{"clamp at edge", image.Pt(180, 180), cropFrac{0, 0, 0.01, 0.01}, image.Rect(0, 0, 6, 6)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := minimapRect(tt.fit, tt.crop); got != tt.want {
				t.Errorf("minimapRect(%v, %+v) = %v, want %v", tt.fit, tt.crop, got, tt.want)
			}
		})
	}

	t.Run("pan keeps size", func(t *testing.T) {
		fit := image.Pt(180, 45)
		// Both crops are 0.25 wide = 45 px; only the origin rounds differently.
		for _, c := range []cropFrac{{0.25, 0, 0.5, 1}, {0.2537, 0, 0.5037, 1}} {
			if got := minimapRect(fit, c).Dx(); got != 45 {
				t.Errorf("minimapRect(%v, %+v).Dx() = %d, want 45", fit, c, got)
			}
		}
	})
}

func TestMinimapBoxUsesMeasuredCell(t *testing.T) {
	m := &galleryModel{cellW: 7, cellH: 15}
	m.l.stripW, m.l.stripH = 18, 10
	// 18*7=126 wide, 10*15=150 tall.
	box := m.minimapBox()
	if want := image.Pt(126, 150); box != want {
		t.Fatalf("minimapBox() = %v, want %v", box, want)
	}
	// scale = min(126/1000, 150/250) = 0.126 -> 126 x 31.5, which rounds to 32.
	if got, want := minimapFit(image.Pt(1000, 250), box), image.Pt(126, 32); got != want {
		t.Errorf("minimapFit = %v, want %v", got, want)
	}

	// Unmeasured cell falls back to cellPxW x cellPxH = 10 x 20: 18*10=180, 10*20=200.
	u := &galleryModel{}
	u.l.stripW, u.l.stripH = 18, 10
	if got, want := u.minimapBox(), image.Pt(180, 200); got != want {
		t.Errorf("unmeasured minimapBox() = %v, want %v", got, want)
	}
}

func TestDrawViewport(t *testing.T) {
	grey := color.RGBA{128, 128, 128, 255}
	black := color.RGBA{0, 0, 0, 255}
	white := color.RGBA{255, 255, 255, 255}
	dst := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for i := 0; i < len(dst.Pix); i += 4 {
		copy(dst.Pix[i:], []byte{128, 128, 128, 255})
	}

	drawViewport(dst, image.Rect(4, 4, 14, 14))

	for _, tt := range []struct {
		x, y int
		want color.RGBA
	}{
		{4, 4, black}, {13, 9, black}, {9, 13, black}, {4, 13, black},
		{5, 5, white}, {12, 9, white}, {9, 12, white}, {12, 12, white},
		{9, 9, grey}, {3, 3, grey}, {14, 14, grey},
	} {
		if got := dst.RGBAAt(tt.x, tt.y); got != tt.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", tt.x, tt.y, got, tt.want)
		}
	}

	drawViewport(dst, image.Rect(-3, -3, 5, 25))
	drawViewport(dst, image.Rect(18, 18, 40, 40))
	drawViewport(dst, image.Rect(-9, -9, -2, -2))
}

func TestMinimapPathsDistinct(t *testing.T) {
	m := &galleryModel{pane: "%41"}
	if m.minimapRawPath() == m.zoomRawPath() {
		t.Errorf("minimapRawPath collides with zoomRawPath: %s", m.minimapRawPath())
	}
	if m.minimapPNGPath() == m.zoomScratchPath() {
		t.Errorf("minimapPNGPath collides with zoomScratchPath: %s", m.minimapPNGPath())
	}
	if !strings.HasSuffix(m.minimapRawPath(), ".raw") {
		t.Errorf("minimapRawPath = %s, want .raw suffix", m.minimapRawPath())
	}
	if !strings.HasSuffix(m.minimapPNGPath(), ".png") {
		t.Errorf("minimapPNGPath = %s, want .png suffix", m.minimapPNGPath())
	}
}
