package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
)

// parseSixelDA reports whether a Primary Device Attributes reply advertises
// sixel support (attribute 4). Reply form: ESC [ ? p1 ; p2 ; ... c
func parseSixelDA(resp string) bool {
	i := strings.IndexByte(resp, '?')
	j := strings.LastIndexByte(resp, 'c')
	if i < 0 || j < 0 || j < i {
		return false
	}
	for _, p := range strings.Split(resp[i+1:j], ";") {
		if p == "4" {
			return true
		}
	}
	return false
}

// probeSixel asks the terminal whether it can render sixel by writing a Primary
// Device Attributes query (ESC [ c) to /dev/tty and checking the reply for the
// sixel attribute (4). It runs in raw mode with a short deadline and fully drains
// the reply to the 'c' terminator, so a late response can't leak onto bubbletea's
// stdin and be misparsed as keystrokes. Any failure (no tty, timeout, no reply)
// returns false, so we never emit sixel bytes to an unconfirmed terminal.
//
// Inside tmux, tmux answers this query reflecting its own sixel capability
// (present when tmux is built --enable-sixel over a sixel-capable outer terminal).
func probeSixel() bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	old, err := term.MakeRaw(tty.Fd())
	if err != nil {
		return false
	}
	defer term.Restore(tty.Fd(), old)

	if _, err := tty.WriteString("\x1b[c"); err != nil {
		return false
	}

	ch := make(chan string, 1)
	go func() {
		var buf []byte
		b := make([]byte, 1)
		for {
			n, err := tty.Read(b)
			if n > 0 {
				buf = append(buf, b[0])
				if b[0] == 'c' {
					break
				}
			}
			if err != nil {
				break
			}
		}
		ch <- string(buf)
	}()

	select {
	case s := <-ch:
		return parseSixelDA(s)
	case <-time.After(150 * time.Millisecond):
		// Deferred tty.Close() unblocks the goroutine's Read, so it can't leak.
		return false
	}
}

// rasterPaintMsg fires after the debounce window to repaint the sixel overlay.
// Only the latest generation survives, so a navigation burst paints once, on the
// final framing — mirroring the vectorKickMsg debounce.
type rasterPaintMsg struct{ gen uint64 }

// rasterPaintDebounce delays the repaint just past bubbletea's frame flush, so the
// sixel lands on top of the freshly-drawn blank holes rather than being clobbered
// by the renderer. Short enough not to feel laggy; tune on-host if needed.
const rasterPaintDebounce = 50 * time.Millisecond

// schedulePaint arms a debounced sixel repaint, bumping the generation so an
// earlier tick arrives stale and is dropped. Returns nil off the raster backend,
// so callers can batch it unconditionally.
func (m *galleryModel) schedulePaint() tea.Cmd {
	if m.backend != backendRaster {
		return nil
	}
	m.rasterGen++
	g := m.rasterGen
	return tea.Tick(rasterPaintDebounce, func(time.Time) tea.Msg { return rasterPaintMsg{gen: g} })
}

// rasterArgs builds the chafa command for a format ("iterm" = OSC 1337, "sixels" =
// sixel) sized to cols×rows cells. Factored out for testing the format selection
// without executing chafa (mirrors symbolsArgs).
func rasterArgs(format, pngPath string, cols, rows int) []string {
	return []string{"-f", format, "--size", fmt.Sprintf("%dx%d", cols, rows), pngPath}
}

// renderRaster rasterizes a PNG to a real-pixel payload (sixel or OSC 1337, per
// format) sized to cols×rows cells via chafa, or "" on failure. chafa sizes both
// formats by --size in cells, so the image fills the cell box without tuning. chafa
// wraps output in cursor hide/show (ESC [?25l … ESC [?25h); strip them so they don't
// fight the TUI's cursor management (same as symbolsBlock).
func renderRaster(format, pngPath string, cols, rows int) string {
	out, err := exec.Command("chafa", rasterArgs(format, pngPath, cols, rows)...).Output()
	if err != nil {
		return ""
	}
	s := string(out)
	s = strings.ReplaceAll(s, "\x1b[?25l", "")
	s = strings.ReplaceAll(s, "\x1b[?25h", "")
	return strings.TrimRight(s, "\n")
}

// paintRasterAt writes a raw terminal graphics payload (sixel or OSC 1337) at rect r's
// top-left, bracketed by save/restore-cursor so the out-of-band paint never disturbs
// bubbletea's cursor. Cursor coordinates are 1-based; rects are 0-based (origin
// top-left). No-op on an empty payload (chafa failed / nothing to draw).
func paintRasterAt(w io.Writer, r rect, payload string) {
	if payload == "" {
		return
	}
	fmt.Fprintf(w, "\x1b7\x1b[%d;%dH%s\x1b8", r.y+1, r.x+1, payload)
}

// paintPreview paints the selected image into the preview rect, honoring the
// current zoom/crop (mirrors transmitView's source selection).
func (m *galleryModel) paintPreview() {
	r := m.previewRect()
	if r.w == 0 || r.h == 0 {
		return
	}
	var src string
	if m.curImg != nil && !m.crop.isFull() {
		src = m.renderZoom(r.w, r.h)
	} else {
		src = cachedPNG(m.images[m.cursor].Path, r.w, r.h)
	}
	paintRasterAt(m.tty, r, renderRaster(m.rasterFormat, src, r.w, r.h))
}

// paintStrip paints each visible filmstrip thumbnail into the inner area of its
// cell rect (inset by 1 to clear the lipgloss border, which is drawn as text).
func (m *galleryModel) paintStrip() {
	start := stripStart(m.cursor, m.l.stripCols, len(m.images))
	for i, cell := range m.filmstripCellRects() {
		inner := rect{x: cell.x + 1, y: cell.y + 1, w: m.l.stripW, h: m.l.stripH}
		png := cachedPNG(m.images[start+i].Path, inner.w, inner.h)
		paintRasterAt(m.tty, inner, renderRaster(m.rasterFormat, png, inner.w, inner.h))
	}
}

// paintRaster paints the whole view (preview + filmstrip) out-of-band on the tty.
// The raster analog of transmitView; called only from the debounced rasterPaintMsg
// handler, after bubbletea has flushed the blank-hole frame.
func (m *galleryModel) paintRaster() {
	if m.backend != backendRaster || m.tty == nil || len(m.images) == 0 {
		return
	}
	m.paintPreview()
	m.paintStrip()
}
