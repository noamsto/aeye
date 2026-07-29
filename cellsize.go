package main

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
)

// parseCellPxReply extracts the cell size from a CSI 16 t reply, which has the
// form ESC [ 6 ; <height> ; <width> t — note height first.
func parseCellPxReply(s string) (w, h int, ok bool) {
	const prefix = "\x1b[6;"
	i := strings.Index(s, prefix)
	if i < 0 {
		return 0, 0, false
	}
	rest := s[i+len(prefix):]
	end := strings.IndexByte(rest, 't')
	if end < 0 {
		return 0, 0, false
	}
	parts := strings.Split(rest[:end], ";")
	if len(parts) != 2 {
		return 0, 0, false
	}
	hh, err1 := strconv.Atoi(parts[0])
	ww, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || ww <= 0 || hh <= 0 {
		return 0, 0, false
	}
	return ww, hh, true
}

// queryCellPx asks the terminal for its cell size in pixels, falling back to the
// cellPxW/cellPxH estimates. Mirrors probeSixel's raw-mode query/read/timeout
// shape (gallery_raster.go:40): the deferred Close unblocks the reader goroutine
// on timeout, so it cannot leak.
func queryCellPx() (int, int) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return cellPxW, cellPxH
	}
	defer tty.Close()
	old, err := term.MakeRaw(tty.Fd())
	if err != nil {
		return cellPxW, cellPxH
	}
	defer term.Restore(tty.Fd(), old)

	if _, err := tty.WriteString("\x1b[16t"); err != nil {
		return cellPxW, cellPxH
	}

	ch := make(chan string, 1)
	go func() {
		var buf []byte
		b := make([]byte, 1)
		for {
			n, err := tty.Read(b)
			if n > 0 {
				buf = append(buf, b[0])
				if b[0] == 't' {
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
		if w, h, ok := parseCellPxReply(s); ok {
			return w, h
		}
	case <-time.After(150 * time.Millisecond):
	}
	return cellPxW, cellPxH
}

// cellWpx/cellHpx are the measured cell size, falling back to the cellPxW/cellPxH
// estimates when a model was built without a measurement. Crop geometry divides by
// these, so a zero would panic — and a model literal that forgets the field is easy
// to write, so the fallback lives here rather than at each call site.
func (m *galleryModel) cellWpx() int {
	if m.cellW > 0 {
		return m.cellW
	}
	return cellPxW
}

func (m *galleryModel) cellHpx() int {
	if m.cellH > 0 {
		return m.cellH
	}
	return cellPxH
}
