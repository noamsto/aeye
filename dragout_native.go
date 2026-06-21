package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"golang.org/x/image/draw"
)

// Native drag-out over kitty's OSC 72 drag-and-drop protocol:
// https://sw.kovidgoyal.net/kitty/dnd-protocol/. The outgoing-drag round-trip is:
//
//	app  → t=o:x=1                  arm: register as a drag source
//	term → t=o:x=:y=:X=:Y=          the user's drag gesture (we get this in Update)
//	app  → t=o:o=3 ; text/uri-list  offer copy|move of a file URI
//	app  → t=p:x=0:m=0 ; <base64>   the file:// URI payload (single chunk, m=0 ends)
//	app  → t=P:x=-1                 initiate the OS drag
//	term → t=E ; OK                 (or a POSIX error name)
//
// OSC 72 can't traverse tmux, so this tier is only selected in a bare kitty.

// probeDragProtocol reports whether the terminal supports OSC 72, using the same
// raw-mode /dev/tty handshake as probeSixel: write the DnD query then a DA1
// request and see which reply lands first. Any failure (no tty, timeout) is a
// safe "unsupported".
func probeDragProtocol() bool {
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

	if _, err := tty.WriteString("\x1b]72;t=q\x1b\\\x1b[c"); err != nil {
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
		return parseDragDA(s)
	case <-time.After(150 * time.Millisecond):
		return false
	}
}

// parseDragDA reports DnD support from the probe reply: an OSC 72 response
// ("\x1b]72;") must appear before the DA1 terminator 'c'. A terminal without DnD
// answers only DA1, so 'c' comes first (or there is no reply at all).
func parseDragDA(resp string) bool {
	osc := strings.Index(resp, "\x1b]72;")
	if osc < 0 {
		return false
	}
	c := strings.IndexByte(resp, 'c')
	return c < 0 || osc < c
}

// fileURI is the absolute file:// URI for path, percent-encoded as drop targets
// expect (e.g. spaces as %20).
func fileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return (&url.URL{Scheme: "file", Path: abs}).String()
}

func dragArmSeq() string      { return "\x1b]72;t=o:x=1\x1b\\" }
func dragOfferSeq() string    { return "\x1b]72;t=o:o=3;text/uri-list\x1b\\" }
func dragInitiateSeq() string { return "\x1b]72;t=P:x=-1\x1b\\" }

// dragDataSeq carries the text/uri-list body — the file:// URI plus the CRLF the
// format requires — base64-encoded as a single complete chunk (m=0).
func dragDataSeq(uri string) string {
	b64 := base64.StdEncoding.EncodeToString([]byte(uri + "\r\n"))
	return "\x1b]72;t=p:x=0:m=0;" + b64 + "\x1b\\"
}

// dragIconMax bounds the drag-cursor thumbnail's longest side (px) — small
// enough to feel like an icon, not a full preview.
const dragIconMax = 128

// dragIconFrames builds the OSC 72 frames that attach a small PNG thumbnail of
// img as the icon shown under the cursor during the drag (negative index = image,
// y=100 = PNG). Returns nil if there's nothing to encode, so the caller just
// omits the icon and the terminal falls back to its default.
func dragIconFrames(img image.Image) []string {
	if img == nil {
		return nil
	}
	b := img.Bounds()
	scale := min(float64(dragIconMax)/float64(b.Dx()), float64(dragIconMax)/float64(b.Dy()), 1)
	w, h := int(float64(b.Dx())*scale), int(float64(b.Dy())*scale)
	if w < 1 || h < 1 {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	var buf bytes.Buffer
	if err := fastPNG.Encode(&buf, dst); err != nil {
		return nil
	}
	return chunkIconFrames(base64.StdEncoding.EncodeToString(buf.Bytes()), w, h)
}

// chunkIconFrames splits the base64 icon into <=4096-byte OSC 72 chunks. The
// first frame carries the format/size metadata; m=1 marks more-to-come, m=0 the
// final chunk.
func chunkIconFrames(b64 string, w, h int) []string {
	const chunk = 4096
	var frames []string
	first := true
	for len(b64) > 0 {
		n := min(chunk, len(b64))
		piece := b64[:n]
		b64 = b64[n:]
		mflag := "1"
		if b64 == "" {
			mflag = "0"
		}
		hdr := "t=p:x=-1:m=" + mflag
		if first {
			hdr = fmt.Sprintf("t=p:x=-1:y=100:X=%d:Y=%d:m=%s", w, h, mflag)
			first = false
		}
		frames = append(frames, "\x1b]72;"+hdr+";"+piece+"\x1b\\")
	}
	return frames
}

// isDragGesture reports whether an inbound OSC 72 event is the terminal telling
// us the user started a drag (t=o with coordinates). Our own outbound t=o frames
// (the x=1 arm, the o=3 offer) never come back, so a y= field uniquely marks the
// inbound gesture.
func isDragGesture(payload string) bool {
	return strings.Contains(payload, "t=o:") && strings.Contains(payload, "y=")
}

// Inbound t=e is a drag-offer status update. x=4 is the terminal telling us the
// drag finished — y=1 means the user cancelled, anything else means it completed.
// (Other x values — accepted/data-request progress — we don't act on, since we
// pre-send the data before initiating.)
func isDragFinished(payload string) bool   { return strings.Contains(payload, "t=e:x=4") }
func dragWasCancelled(payload string) bool { return strings.Contains(payload, "t=e:x=4:y=1") }

// isDragError reports the terminal rejecting our initiate: t=E carries OK on
// success or a POSIX error name otherwise. Success is reported via t=e:x=4.
func isDragError(payload string) bool {
	return strings.Contains(payload, "t=E") && !strings.Contains(payload, "OK")
}

// oscInt reads an integer field like "x=" from an OSC 72 payload, up to the next
// delimiter. Case-sensitive, so "x=" never matches "X=".
func oscInt(payload, key string) (int, bool) {
	i := strings.Index(payload, key)
	if i < 0 {
		return 0, false
	}
	rest := payload[i+len(key):]
	end := strings.IndexAny(rest, ":;\x1b\\")
	if end < 0 {
		end = len(rest)
	}
	n, err := strconv.Atoi(rest[:end])
	return n, err == nil
}
