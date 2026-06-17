package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

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

// renderSixel rasterizes a PNG to a sixel payload sized to cols×rows cells via
// chafa, or "" on failure. chafa's non-tty default cell geometry is 10×20 px,
// matching cellPxW/cellPxH, so the image fills the cell box without tuning.
// chafa wraps output in cursor hide/show (ESC [?25l … ESC [?25h); strip them so
// they don't fight the TUI's cursor management (same as symbolsBlock).
func renderSixel(pngPath string, cols, rows int) string {
	out, err := exec.Command("chafa", "-f", "sixels", "--size", fmt.Sprintf("%dx%d", cols, rows), pngPath).Output()
	if err != nil {
		return ""
	}
	s := string(out)
	s = strings.ReplaceAll(s, "\x1b[?25l", "")
	s = strings.ReplaceAll(s, "\x1b[?25h", "")
	return strings.TrimRight(s, "\n")
}

// paintSixelAt writes a sixel payload at rect r's top-left, bracketed by
// save/restore-cursor so the out-of-band paint never disturbs bubbletea's cursor.
// Cursor coordinates are 1-based; rects are 0-based (origin top-left). No-op on
// an empty payload (chafa failed / nothing to draw).
func paintSixelAt(w io.Writer, r rect, sixel string) {
	if sixel == "" {
		return
	}
	fmt.Fprintf(w, "\x1b7\x1b[%d;%dH%s\x1b8", r.y+1, r.x+1, sixel)
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
	paintSixelAt(m.tty, r, renderSixel(src, r.w, r.h))
}

// paintStrip paints each visible filmstrip thumbnail into the inner area of its
// cell rect (inset by 1 to clear the lipgloss border, which is drawn as text).
func (m *galleryModel) paintStrip() {
	start := stripStart(m.cursor, m.l.stripCols, len(m.images))
	for i, cell := range m.filmstripCellRects() {
		inner := rect{x: cell.x + 1, y: cell.y + 1, w: m.l.stripW, h: m.l.stripH}
		png := cachedPNG(m.images[start+i].Path, inner.w, inner.h)
		paintSixelAt(m.tty, inner, renderSixel(png, inner.w, inner.h))
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
