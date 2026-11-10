package main

import (
	"crypto/sha1" //nolint:gosec // sha1 only names cache files; not a security use
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// legendSuffix names a diagram's sidecar legend: <image stem>.legend.svg, beside
// the image. A producer keeps a long key out of the picture this way, and the
// gallery shows it on demand.
const legendSuffix = ".legend.svg"

// legendMaxScale bounds how far a small legend is blown up to fill its corner.
const legendMaxScale = 2.0

// legendSidecar is the legend file for the image at imagePath, or "" when it has
// none. A themed image (foo-dark.png) prefers its own legend (foo-dark.legend.svg)
// over the shared one (foo.legend.svg), so the overlay follows the variant the
// gallery resolved for the image.
func legendSidecar(imagePath string) string {
	stem := strings.TrimSuffix(imagePath, filepath.Ext(imagePath))
	cands := []string{stem + legendSuffix}
	for _, theme := range []string{"light", "dark"} {
		if before, ok := strings.CutSuffix(stem, "-"+theme); ok {
			cands = append(cands, before+legendSuffix)
		}
	}
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && fi.Mode().IsRegular() {
			return c
		}
	}
	return ""
}

// legendKey is everything that decides the overlay's pixels and cell box.
type legendKey struct {
	svg          string
	mtime        int64
	boxCols      int
	boxRows      int
	cellW, cellH int
	cols, rows   int // resulting cell size; zero in a key built before the render
}

// legendState is the on-demand legend overlay: on is the user's toggle, stored
// is what the kitty store holds under legendID (zero key when nothing).
type legendState struct {
	on     bool
	stored legendKey
	failed legendKey // last inputs renderLegend could not turn into an overlay
}

// legendID is the image id of the overlay: the one id of the pane's block that
// no filmstrip slot reaches (computeLayout keeps slots below altOffset-1).
func (m *galleryModel) legendID() int {
	return paneImageIDBase(m.pane) + altOffset - 1
}

// legendBox is the corner of the preview the overlay may take, in cells.
func (m *galleryModel) legendBox() (cols, rows int) {
	return max(1, m.l.previewW*2/5), max(1, m.l.previewH*2/5)
}

// legendCells fits an svgW×svgH legend into a box of cells, keeping its aspect.
func legendCells(svgW, svgH float64, boxCols, boxRows, cellW, cellH int) (cols, rows int) {
	s := min(float64(boxCols*cellW)/svgW, float64(boxRows*cellH)/svgH, legendMaxScale)
	cols = max(1, min(boxCols, int(svgW*s)/cellW))
	rows = max(1, min(boxRows, int(svgH*s)/cellH))
	return cols, rows
}

// toggleLegend flips the overlay. With no sidecar it only says so: the key
// never half-enables a legend that cannot show.
func (m *galleryModel) toggleLegend() {
	switch {
	case m.legend.on:
		m.legend.on = false
	case m.backend != backendKitty:
		m.status = "Legend overlay needs a kitty-graphics terminal"
		return
	case len(m.images) == 0 || legendSidecar(m.images[m.cursor].Path) == "":
		m.status = "No legend for this image"
		return
	default:
		m.legend.on = true
		m.legend.failed = legendKey{}
	}
	m.syncLegend()
	if m.legend.on && m.legend.failed != (legendKey{}) {
		m.legend.on = false
	}
}

// syncLegend brings the stored overlay in line with the selection: stores the
// current image's legend when it should show, deletes it when it should not.
// Cheap when nothing changed (one stat), so the transmit paths call it freely.
func (m *galleryModel) syncLegend() {
	if m.backend != backendKitty || m.tty == nil || len(m.images) == 0 {
		return
	}
	var svg string
	if m.legend.on {
		svg = legendSidecar(m.images[m.cursor].Path)
	}
	if svg == "" {
		m.dropLegend()
		return
	}
	fi, err := os.Stat(svg)
	if err != nil {
		m.dropLegend()
		return
	}
	boxCols, boxRows := m.legendBox()
	key := legendKey{svg: svg, mtime: fi.ModTime().UnixNano(), boxCols: boxCols, boxRows: boxRows,
		cellW: m.cellWpx(), cellH: m.cellHpx()}
	if m.legend.stored.sameInputs(key) {
		return
	}
	if m.legend.failed == key {
		// Another image's overlay must not stay up over this one.
		m.dropLegend()
		return
	}
	out, cols, rows := renderLegend(key)
	if out == "" {
		// Remembered, so a sidecar that cannot render is not re-read and
		// re-rasterized on every pan frame.
		m.legend.failed = key
		m.status = "Legend failed to render"
		m.dropLegend()
		return
	}
	key.cols, key.rows = cols, rows
	if _, err := fmt.Fprint(m.tty, transmitVirtual(m.legendID(), out, cols, rows)); err == nil {
		m.legend.stored = key
	}
}

// dropLegend removes a stored overlay.
func (m *galleryModel) dropLegend() {
	if m.legend.stored == (legendKey{}) {
		return
	}
	fmt.Fprint(m.tty, deleteImage(m.legendID())) //nolint:errcheck // best-effort tty escape-sequence write
	m.legend.stored = legendKey{}
}

// sameInputs reports whether k would render what s already holds.
func (s legendKey) sameInputs(k legendKey) bool {
	s.cols, s.rows = 0, 0
	return s == k
}

// renderLegend rasterizes key's sidecar to a PNG exactly cols×rows cells large,
// the legend anchored to its bottom-right corner on a transparent canvas so the
// kitty cell box never stretches it. Returns "" when resvg or the file fails.
func renderLegend(key legendKey) (path string, cols, rows int) {
	data, err := os.ReadFile(key.svg) //nolint:gosec // path is the sidecar of the user's own image
	if err != nil {
		return "", 0, 0
	}
	w, h, err := svgSize(data)
	if err != nil {
		tracef("legend %s: %v", key.svg, err)
		return "", 0, 0
	}
	cols, rows = legendCells(w, h, key.boxCols, key.boxRows, key.cellW, key.cellH)
	boxW, boxH := cols*key.cellW, rows*key.cellH

	// One file per sidecar: a new framing or mtime evicts the older ones.
	stem := filepath.Join(imgCacheDir, fmt.Sprintf("legend-%x", sha1.Sum([]byte(key.svg)))) //nolint:gosec // sha1 only names cache files; not a security use
	out := fmt.Sprintf("%s-%d-%dx%d.png", stem, key.mtime, boxW, boxH)
	if _, err := os.Stat(out); err == nil { //nolint:gosec // out is under the cache dir, named by a hash of the sidecar path
		return out, cols, rows
	}
	if err := os.MkdirAll(imgCacheDir, 0o755); err != nil { //nolint:gosec // cache dir holds user-viewable images, not secrets
		return "", 0, 0
	}
	olds, _ := filepath.Glob(stem + "-*")
	for _, o := range olds {
		os.Remove(o) //nolint:errcheck,gosec // best-effort temp cleanup
	}
	raw, err := os.CreateTemp(imgCacheDir, "tmp-legend-*.png")
	if err != nil {
		return "", 0, 0
	}
	raw.Close()                 //nolint:errcheck,gosec // only the name is needed; resvg writes it
	defer os.Remove(raw.Name()) //nolint:errcheck // best-effort temp cleanup
	args := append(resvgFontArgs(), "--width", strconv.Itoa(boxW), "--height", strconv.Itoa(boxH), key.svg, raw.Name())
	if err := exec.Command(resvgBin(), args...).Run(); err != nil { //nolint:gosec // args built by the tool, not shell-interpreted
		tracef("legend %s: resvg: %v", key.svg, err)
		return "", 0, 0
	}
	f, err := os.Open(raw.Name()) //nolint:gosec // scratch PNG this function just rendered
	if err != nil {
		return "", 0, 0
	}
	defer f.Close() //nolint:errcheck // read-only file
	img, err := png.Decode(f)
	if err != nil {
		return "", 0, 0
	}
	fit := fitToBox(img, boxW, boxH)
	canvas := image.NewNRGBA(image.Rect(0, 0, boxW, boxH))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.NRGBA{}), image.Point{}, draw.Src)
	fb := fit.Bounds()
	draw.Draw(canvas, image.Rect(boxW-fb.Dx(), boxH-fb.Dy(), boxW, boxH), fit, fb.Min, draw.Src)
	// Padded over the raw render's own temp file, then renamed into place, so a
	// reader never sees a half-written cache file.
	dst, err := os.Create(raw.Name())
	if err != nil {
		return "", 0, 0
	}
	if err := png.Encode(dst, canvas); err != nil {
		dst.Close() //nolint:errcheck,gosec // already failing; the encode error wins
		return "", 0, 0
	}
	if err := dst.Close(); err != nil {
		return "", 0, 0
	}
	if err := os.Rename(raw.Name(), out); err != nil { //nolint:gosec // out is under the cache dir, named by a hash of the sidecar path
		return "", 0, 0
	}
	return out, cols, rows
}

// placeholderBlockOver is placeholderBlock for the preview with a second image
// (the legend) covering its bottom-right ovCols×ovRows cells. Each cell names
// the image it belongs to, so the legend sits over the preview without touching
// it: the preview's own crop, pan and zoom are not involved.
func placeholderBlockOver(id, w, h, ovID, ovCols, ovRows int) string {
	// A stored overlay can outlive a shrink of the preview (a failed re-store).
	ovCols, ovRows = min(ovCols, w), min(ovRows, h)
	var b strings.Builder
	for r := range h {
		cur := -1
		for c := range w {
			cid, rr, cc := id, r, c
			if r >= h-ovRows && c >= w-ovCols {
				cid, rr, cc = ovID, r-(h-ovRows), c-(w-ovCols)
			}
			if cid != cur {
				if cur != -1 {
					b.WriteString("\x1b[39m")
				}
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm", (cid>>16)&0xff, (cid>>8)&0xff, cid&0xff)
				cur = cid
			}
			b.WriteString(placeholderRune)
			b.WriteRune(rowColDiacritics[rr])
			b.WriteRune(rowColDiacritics[cc])
		}
		b.WriteString("\x1b[39m")
		if r < h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
