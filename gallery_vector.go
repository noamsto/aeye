package main

import (
	"crypto/sha1"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/image/draw"
)

// cropViewBox rewrites the SVG's outer viewBox to the crop sub-rectangle so
// resvg rasterizes only the visible window — keeping render cost flat at any
// zoom depth instead of scaling with 1/crop. The crop is normalized over the
// full canvas, the same space the bitmap layer samples, so the sharp render
// lands pixel-aligned over the instant preview. Returns (svg, false) unchanged
// when there's no viewBox to rewrite.
func cropViewBox(svg []byte, c cropFrac) ([]byte, bool) {
	loc := svgViewBoxRe.FindSubmatchIndex(svg)
	if loc == nil {
		return svg, false
	}
	num := func(pair int) float64 {
		v, _ := strconv.ParseFloat(string(svg[loc[2*pair]:loc[2*pair+1]]), 64)
		return v
	}
	minX, minY, vbW, vbH := num(1), num(2), num(3), num(4)
	repl := fmt.Sprintf(`viewBox="%.4f %.4f %.4f %.4f"`,
		minX+c.x0*vbW, minY+c.y0*vbH, c.w()*vbW, c.h()*vbH)
	out := make([]byte, 0, len(svg)+len(repl))
	out = append(out, svg[:loc[0]]...)
	out = append(out, repl...)
	out = append(out, svg[loc[1]:]...)
	return out, true
}

// renderVector rasterizes the crop window of the SVG to a scratch PNG at targetW
// pixels wide via resvg, caching on (svg, mtime, crop, targetW). Returns the PNG
// path, or "" if resvg is absent or the render fails (caller falls back to the
// bitmap crop). When zoomed, only the crop region is rendered (see cropViewBox).
func renderVector(vector string, crop cropFrac, targetW int) string {
	fi, err := os.Stat(vector)
	if err != nil {
		return ""
	}
	bin := os.Getenv("AEYE_RESVG")
	if bin == "" {
		bin = "resvg"
	}
	// One cached PNG per (svg, mtime); crop + width are the filename suffix so we
	// can evict prior framings — /tmp then holds at most one vector scratch per
	// diagram, not one per pan/zoom step.
	stem := fmt.Sprintf("aeye-vec-%x", sha1.Sum([]byte(fmt.Sprintf("%s|%d", vector, fi.ModTime().UnixNano()))))
	out := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%.4f_%.4f_%.4f_%.4f-%d.png",
		stem, crop.x0, crop.y0, crop.x1, crop.y1, targetW))
	if _, err := os.Stat(out); err == nil {
		return out
	}
	if _, err := exec.LookPath(bin); err != nil {
		return ""
	}
	// Evict stale framings for this svg before rendering the new one.
	olds, _ := filepath.Glob(filepath.Join(os.TempDir(), stem+"-*.png"))
	for _, o := range olds {
		os.Remove(o)
	}
	src := vector
	if !crop.isFull() {
		data, err := os.ReadFile(vector)
		if err != nil {
			return ""
		}
		if cropped, ok := cropViewBox(data, crop); ok {
			tmp := filepath.Join(os.TempDir(), stem+"-crop.svg")
			if err := os.WriteFile(tmp, cropped, 0o644); err != nil {
				return ""
			}
			defer os.Remove(tmp)
			src = tmp
		}
	}
	if err := exec.Command(bin, "--width", strconv.Itoa(targetW), src, out).Run(); err != nil {
		os.Remove(out)
		return ""
	}
	return out
}

// vectorReadyMsg carries a finished crop raster back to Update. vector/crop
// identify which request it answers, so a stale render (the selection or framing
// moved on while resvg ran) is ignored.
type vectorReadyMsg struct {
	vector string
	crop   cropFrac
	raster image.Image
}

// renderVectorCmd rasterizes off the event loop (resvg subprocess) and decodes
// the result, so the TUI never blocks on a render. Returns nil on any failure.
func renderVectorCmd(vector string, crop cropFrac, targetW int) tea.Cmd {
	return func() tea.Msg {
		out := renderVector(vector, crop, targetW)
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
		return vectorReadyMsg{vector: vector, crop: crop, raster: img}
	}
}

// vectorKickMsg fires after the debounce window; only the latest generation's
// message survives to actually kick a render.
type vectorKickMsg struct{ gen uint64 }

// vectorDebounce coalesces a pan/zoom keystroke burst into a single sharp render
// of the final framing — below the threshold where waiting feels laggy, while
// the instant bitmap crop covers the gap.
const vectorDebounce = 70 * time.Millisecond

// curVector returns the selected entry's vector source path, or "" if it has none.
func (m *galleryModel) curVector() string {
	if len(m.images) == 0 {
		return ""
	}
	return m.images[m.cursor].Vector
}

// scheduleVector arms a debounced sharp re-render: it bumps the generation and
// fires a tick carrying it. A later keystroke bumps again, so an earlier tick
// arrives stale and is dropped — only the final framing reaches resvg. Returns
// nil when there's nothing to sharpen (no vector / not kitty).
func (m *galleryModel) scheduleVector() tea.Cmd {
	if m.curVector() == "" || m.backend != backendKitty {
		return nil
	}
	m.vecGen++
	g := m.vecGen
	return tea.Tick(vectorDebounce, func(time.Time) tea.Msg { return vectorKickMsg{gen: g} })
}

// kickVector returns the async render cmd for the current d2 selection at the
// current crop, or nil when there is nothing to sharpen (no vector / not kitty).
func (m *galleryModel) kickVector() tea.Cmd {
	v := m.curVector()
	if v == "" || m.backend != backendKitty {
		return nil
	}
	return renderVectorCmd(v, m.crop, m.l.previewW*cellPxW)
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
