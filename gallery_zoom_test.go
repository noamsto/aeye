package main

import (
	"image"
	"testing"
)

func approx(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

func TestFullCrop(t *testing.T) {
	c := fullCrop()
	if c.x0 != 0 || c.y0 != 0 || c.x1 != 1 || c.y1 != 1 {
		t.Errorf("fullCrop = %+v", c)
	}
	if !c.isFull() {
		t.Error("fullCrop must report isFull")
	}
}

func TestCropFracDims(t *testing.T) {
	c := cropFrac{0.2, 0.1, 0.8, 0.6}
	if !approx(c.w(), 0.6) || !approx(c.h(), 0.5) {
		t.Errorf("w/h = %v,%v", c.w(), c.h())
	}
	if !approx(c.cx(), 0.5) || !approx(c.cy(), 0.35) {
		t.Errorf("cx/cy = %v,%v", c.cx(), c.cy())
	}
	if c.isFull() {
		t.Error("sub-rect must not report isFull")
	}
}

func TestClampF(t *testing.T) {
	if clampF(5, 1, 8) != 5 || clampF(0, 1, 8) != 1 || clampF(9, 1, 8) != 8 {
		t.Error("clampF bounds wrong")
	}
}

func TestCropPixels(t *testing.T) {
	b := image.Rect(0, 0, 100, 80)
	r := cropPixels(b, cropFrac{0.25, 0.25, 0.75, 0.75})
	if r != image.Rect(25, 20, 75, 60) {
		t.Errorf("cropPixels = %v, want (25,20)-(75,60)", r)
	}
	if r := cropPixels(b, fullCrop()); r != b {
		t.Errorf("full cropPixels = %v, want %v", r, b)
	}
}

func TestCropPixelsOffsetBounds(t *testing.T) {
	b := image.Rect(10, 20, 110, 100) // 100x80 at origin (10,20)
	r := cropPixels(b, cropFrac{0, 0, 0.5, 0.5})
	if r != image.Rect(10, 20, 60, 60) {
		t.Errorf("offset cropPixels = %v, want (10,20)-(60,60)", r)
	}
}
