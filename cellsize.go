package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
)

// parseCellPxReply extracts the cell size from a CSI 16 t reply, which has the
// form ESC [ 6 ; <height> ; <width> t — note height first.
func parseCellPxReply(s string) (w, h int, ok bool) {
	const prefix = "\x1b[6;"
	_, after, ok := strings.Cut(s, prefix)
	if !ok {
		return 0, 0, false
	}
	rest := after
	before0, _, ok0 := strings.Cut(rest, "t")
	if !ok0 {
		return 0, 0, false
	}
	parts := strings.Split(before0, ";")
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

// maxCellPx bounds a cell dimension taken from the override or from tmux: the
// preview box is previewW*cell px wide, and an absurd value (a typo'd extra
// zero) would ask fitToBox for a multi-terabyte raster.
const maxCellPx = 512

func plausibleCellPx(w, h int) bool {
	return w > 0 && h > 0 && w <= maxCellPx && h <= maxCellPx
}

// parseCellPxEnv reads an AEYE_CELL_PX override of the form "<w>x<h>".
func parseCellPxEnv(s string) (w, h int, ok bool) {
	ws, hs, ok := strings.Cut(strings.TrimSpace(s), "x")
	if !ok {
		return 0, 0, false
	}
	w, err1 := strconv.Atoi(ws)
	h, err2 := strconv.Atoi(hs)
	if err1 != nil || err2 != nil || !plausibleCellPx(w, h) {
		return 0, 0, false
	}
	return w, h, true
}

// parseClientCellPx takes the first client in `list-clients -F '#{client_cell_width}
// #{client_cell_height}'` output that reports a plausible cell size — not
// necessarily the one displaying this pane when several relays are attached. A
// client that doesn't know its pixels prints empty or non-positive fields.
func parseClientCellPx(out string) (w, h int, ok bool) {
	for line := range strings.SplitSeq(out, "\n") {
		ws, hs, _ := strings.Cut(strings.TrimSpace(line), " ")
		w, err1 := strconv.Atoi(ws)
		h, err2 := strconv.Atoi(hs)
		if err1 == nil && err2 == nil && plausibleCellPx(w, h) {
			return w, h, true
		}
	}
	return 0, 0, false
}

// tmuxRelayCellPx is the cell size the relay's mirroring client reports to this
// pane's tmux session.
func tmuxRelayCellPx() (int, int, bool) {
	pane := os.Getenv("TMUX_PANE")
	if os.Getenv("TMUX") == "" || pane == "" {
		return 0, 0, false
	}
	out, err := exec.Command("tmux", "list-clients", "-t", pane, "-F", "#{client_cell_width} #{client_cell_height}").Output() //nolint:gosec // fixed tmux binary; args built by the tool, not shell-interpreted
	if err != nil {
		return 0, 0, false
	}
	return parseClientCellPx(string(out))
}

// measureCellPx resolves the terminal's cell size in pixels. Behind a relay,
// CSI 16 t never reaches the real terminal: this pane's tmux answers it with a
// default (16x32) for the control-mode client that mirrors it, and a box shaped
// by the wrong cell aspect makes kitty pillarbox the packed zoom crop. So an
// explicit AEYE_CELL_PX wins, then the size the mirroring client reports to
// tmux, and only then the terminal query.
func measureCellPx(relayed bool, override string, client func() (int, int, bool), query func() (int, int)) (int, int) {
	if w, h, ok := parseCellPxEnv(override); ok {
		tracef("cell size source=AEYE_CELL_PX")
		return w, h
	}
	if relayed {
		if w, h, ok := client(); ok {
			tracef("cell size source=tmux client")
			return w, h
		}
	}
	tracef("cell size source=terminal query")
	return query()
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
	defer tty.Close() //nolint:errcheck // closing /dev/tty on exit; nothing to recover
	old, err := term.MakeRaw(tty.Fd())
	if err != nil {
		return cellPxW, cellPxH
	}
	defer term.Restore(tty.Fd(), old) //nolint:errcheck // best-effort terminal restore in defer

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
