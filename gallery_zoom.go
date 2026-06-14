package main

import "image"

// cropFrac is the visible sub-rectangle of the source image, in source
// fractions (0..1). Full image = {0,0,1,1}. Invariant kept by the methods that
// mutate it: 0 <= x0 < x1 <= 1 and 0 <= y0 < y1 <= 1.
type cropFrac struct{ x0, y0, x1, y1 float64 }

func fullCrop() cropFrac { return cropFrac{0, 0, 1, 1} }

func (c cropFrac) w() float64  { return c.x1 - c.x0 }
func (c cropFrac) h() float64  { return c.y1 - c.y0 }
func (c cropFrac) cx() float64 { return (c.x0 + c.x1) / 2 }
func (c cropFrac) cy() float64 { return (c.y0 + c.y1) / 2 }

// isFull reports whether the crop covers (essentially) the whole image, i.e.
// nothing is zoomed. The epsilon absorbs float drift from repeated zoom-out.
func (c cropFrac) isFull() bool { return c.w() >= 0.999 && c.h() >= 0.999 }

// clampF clamps a float64 to [lo, hi].
func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// cropPixels maps a normalized crop to a pixel rectangle inside b, offset by
// b.Min so callers can sample the source directly.
func cropPixels(b image.Rectangle, c cropFrac) image.Rectangle {
	w, h := float64(b.Dx()), float64(b.Dy())
	return image.Rect(
		b.Min.X+int(c.x0*w), b.Min.Y+int(c.y0*h),
		b.Min.X+int(c.x1*w), b.Min.Y+int(c.y1*h),
	)
}
