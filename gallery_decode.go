package main

import (
	"context"
	"image"
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
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

// decodeDebounce delays the decode past a key-repeat burst (25-30 steps/s): the
// PNG decoder allocates its full output buffer before reading a pixel, so a held
// key must start one decode for the final selection, not one per step.
const decodeDebounce = 50 * time.Millisecond

// decodeKickMsg starts the decode for gen once the debounce has passed. Stale
// kicks (a later selection or resize re-requested) are dropped.
type decodeKickMsg struct{ gen uint64 }

// decodedMsg carries a finished decode back to the loop. img is nil on failure
// or cancellation; src is the decoded source size (before the working copy) and
// scale the workingScale the copy was built at.
type decodedMsg struct {
	gen   uint64
	path  string
	img   image.Image
	src   image.Point
	scale float64
}

// ctxReader fails reads once ctx is cancelled, so a superseded decode stops at
// its next read (image.Decode buffers, so about every 4 KB) instead of running
// to completion.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// decodeWorking decodes path and returns its working copy for a boxW x boxH px
// preview box, with the source size and the scale the copy was built at. img is
// nil when the decode fails or ctx is cancelled.
func decodeWorking(ctx context.Context, path string, boxW, boxH int) (img image.Image, src image.Point, scale float64) {
	f, err := os.Open(path) //nolint:gosec // path is the user's own image/manifest file
	if err != nil {
		return nil, image.Point{}, 0
	}
	defer f.Close() //nolint:errcheck // read-only file or already-failing path; close error is not actionable
	full, _, err := image.Decode(ctxReader{ctx, f})
	if err != nil || ctx.Err() != nil {
		return nil, image.Point{}, 0
	}
	src = full.Bounds().Size()
	scale = workingScale(src, boxW, boxH)
	return workingCopy(full, scale), src, scale
}

// decodeCmd runs decodeWorking off the loop. It captures only values, never the
// model, so the result can't race the state Update goes on mutating.
func decodeCmd(ctx context.Context, gen uint64, path string, boxW, boxH int) tea.Cmd {
	return func() tea.Msg {
		img, src, scale := decodeWorking(ctx, path, boxW, boxH)
		return decodedMsg{gen: gen, path: path, img: img, src: src, scale: scale}
	}
}

// cancelDecode aborts the in-flight decode, if any.
func (m *galleryModel) cancelDecode() {
	if m.decodeCancel != nil {
		m.decodeCancel()
		m.decodeCancel = nil
	}
}

// requestDecode supersedes any earlier decode: it is cancelled, and bumping
// decodeGen makes its kick or result arrive stale. Update's armDecode schedules
// the new one.
func (m *galleryModel) requestDecode() {
	m.cancelDecode()
	m.decodeGen++
}

// armDecode returns the debounced kick for the latest decode request, once per
// generation. It waits for ready (the working copy is capped against the preview
// box) and for a known size (an unreadable header can't decode either).
func (m *galleryModel) armDecode() tea.Cmd {
	if !m.ready || m.curSize == (image.Point{}) || m.decodeArmedGen == m.decodeGen {
		return nil
	}
	m.decodeArmedGen = m.decodeGen
	g := m.decodeGen
	return tea.Tick(decodeDebounce, func(time.Time) tea.Msg { return decodeKickMsg{gen: g} })
}

// previewBoxPx is the preview box in pixels.
func (m *galleryModel) previewBoxPx() (int, int) {
	return m.l.previewW * m.cellWpx(), m.l.previewH * m.cellHpx()
}
