package main

import (
	"crypto/sha1"
	"fmt"
	"image"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/image/draw"
)

// vectorTargetW is the width to rasterize the whole SVG canvas at so the kept
// crop fills the preview box. Bounded by preview-box × zoom (≈ 1/min crop side),
// independent of the source's intrinsic resolution. A small over-estimate keeps
// both axes sharp.
func vectorTargetW(boxW, boxH int, c cropFrac) int {
	minSide := math.Min(c.w(), c.h())
	if minSide < 1e-6 {
		minSide = 1e-6
	}
	px := float64(boxW)
	if boxH > boxW {
		px = float64(boxH)
	}
	return int(math.Ceil(px / minSide))
}

// renderVector rasterizes the SVG to a scratch PNG at targetW pixels wide via
// resvg, caching on (vector path, mtime, targetW). Returns the PNG path, or ""
// if resvg is absent or the render fails (caller falls back to the bitmap crop).
func renderVector(vector string, targetW int) string {
	fi, err := os.Stat(vector)
	if err != nil {
		return ""
	}
	bin := os.Getenv("AGENT_CAROUSEL_RESVG")
	if bin == "" {
		bin = "resvg"
	}
	// One cached PNG per (svg, mtime); the zoom level (targetW) is the filename
	// suffix so we can evict prior zoom levels — /tmp then holds at most one
	// vector scratch per diagram, not one per zoom step.
	stem := fmt.Sprintf("agent-carousel-vec-%x", sha1.Sum([]byte(fmt.Sprintf("%s|%d", vector, fi.ModTime().UnixNano()))))
	out := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d.png", stem, targetW))
	if _, err := os.Stat(out); err == nil {
		return out
	}
	if _, err := exec.LookPath(bin); err != nil {
		return ""
	}
	// Evict stale zoom levels for this svg before rendering the new one.
	olds, _ := filepath.Glob(filepath.Join(os.TempDir(), stem+"-*.png"))
	for _, o := range olds {
		os.Remove(o)
	}
	if err := exec.Command(bin, "--width", strconv.Itoa(targetW), vector, out).Run(); err != nil {
		os.Remove(out)
		return ""
	}
	return out
}

// vectorReadyMsg carries a finished vector raster back to Update. vector/targetW
// identify which request it answers, so a stale render (the selection or zoom
// moved on while resvg ran) is ignored.
type vectorReadyMsg struct {
	vector  string
	targetW int
	raster  image.Image
}

// renderVectorCmd rasterizes off the event loop (resvg subprocess) and decodes
// the result, so the TUI never blocks on a render. Returns nil on any failure.
func renderVectorCmd(vector string, targetW int) tea.Cmd {
	return func() tea.Msg {
		out := renderVector(vector, targetW)
		if out == "" {
			return nil
		}
		f, err := os.Open(out)
		if err != nil {
			return nil
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			return nil
		}
		return vectorReadyMsg{vector: vector, targetW: targetW, raster: img}
	}
}

// curVector returns the selected entry's vector source path, or "" if it has none.
func (m *galleryModel) curVector() string {
	if len(m.images) == 0 {
		return ""
	}
	return m.images[m.cursor].Vector
}

// kickVector returns the async render cmd for the current d2 selection at the
// current zoom, or nil when there is nothing to sharpen (no vector / not kitty).
func (m *galleryModel) kickVector() tea.Cmd {
	v := m.curVector()
	if v == "" || m.backend != backendKitty {
		return nil
	}
	return renderVectorCmd(v, vectorTargetW(m.l.previewW*cellPxW, m.l.previewH*cellPxH, m.crop))
}

// fitToBox scales src to fit tw×th preserving aspect, upscaling if smaller — the
// rest-state path for small diagrams (vector has no upscale ceiling).
func fitToBox(src image.Image, tw, th int) image.Image {
	b := src.Bounds()
	scale := min(float64(tw)/float64(b.Dx()), float64(th)/float64(b.Dy()))
	dst := image.NewRGBA(image.Rect(0, 0, int(float64(b.Dx())*scale), int(float64(b.Dy())*scale)))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}
