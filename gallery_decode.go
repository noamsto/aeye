package main

import (
	"image"
	"os"

	"golang.org/x/image/draw"
)

// imageSize reads the pixel size from path's header without decoding pixels.
// Zero when the file is missing or its header is unreadable.
func imageSize(path string) image.Point {
	f, err := os.Open(path) //nolint:gosec // path is the user's own image/manifest file
	if err != nil {
		return image.Point{}
	}
	defer f.Close() //nolint:errcheck // read-only file or already-failing path; close error is not actionable
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return image.Point{}
	}
	return image.Pt(cfg.Width, cfg.Height)
}

// workingScale is the scale at which the retained copy of a src-sized image can
// still serve every crop losslessly in a boxW x boxH px preview box.
//
// cropRaster renders a crop (w x h source fractions) at min(1, boxW/(w*srcX),
// boxH/(h*srcY)), so a copy scaled by s loses nothing iff s is at least that.
// Every non-region crop keeps its longer side >= 1/zoomMax (zoomFloor bounds
// zoomBy, wheel and scaleCropAbout; toggleFill and baseFillCrop always keep one
// axis at 1), hence the bound is the larger of the two per-axis ratios times
// zoomMax, not the smaller: a thin strip is limited by its long axis. Region
// mode can zoom deeper but is d2-on-kitty only, and resvg re-renders that.
// Any non-positive input yields 1 (no scaling).
func workingScale(src image.Point, boxW, boxH int) float64 {
	if src.X <= 0 || src.Y <= 0 || boxW <= 0 || boxH <= 0 {
		return 1
	}
	rx, ry := float64(boxW)/float64(src.X), float64(boxH)/float64(src.Y)
	return min(1, zoomMax*max(rx, ry))
}

// workingCopy is the image retained for the selection: img scaled by s when
// s < 1, otherwise img with 16-bit pixels narrowed to 8-bit (half the memory).
// 8-bit, paletted and YCbCr decodes are returned as-is; converting them to RGBA
// would grow them.
func workingCopy(img image.Image, s float64) image.Image {
	b := img.Bounds()
	if s < 1 {
		dst := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(b.Dx())*s)), max(1, int(float64(b.Dy())*s))))
		draw.BiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		return dst
	}
	switch img.(type) {
	case *image.NRGBA64, *image.RGBA64:
		dst := image.NewRGBA(b)
		draw.Draw(dst, b, img, b.Min, draw.Src)
		return dst
	case *image.Gray16:
		dst := image.NewGray(b)
		draw.Draw(dst, b, img, b.Min, draw.Src)
		return dst
	}
	return img
}
