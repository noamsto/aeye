package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestWorkingScale(t *testing.T) {
	tests := []struct {
		name       string
		src        image.Point
		boxW, boxH int
		want       float64
	}{
		{"typical screenshot fits", image.Pt(3456, 2234), 1220, 814, 1},
		{"huge image on both axes is capped", image.Pt(16000, 9000), 1220, 814, zoomMax * max(1220.0/16000, 814.0/9000)},
		{"tall page keeps full resolution", image.Pt(1440, 14000), 2000, 1000, 1},
		{"zero box", image.Pt(3456, 2234), 0, 0, 1},
		{"zero box width", image.Pt(3456, 2234), 0, 814, 1},
		{"unknown source", image.Point{}, 1220, 814, 1},
		{"negative box", image.Pt(3456, 2234), -1, 814, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := workingScale(tc.src, tc.boxW, tc.boxH); !approx(got, tc.want) {
				t.Errorf("workingScale(%v, %d, %d) = %v, want %v", tc.src, tc.boxW, tc.boxH, got, tc.want)
			}
		})
	}
}

// workingDims is the size workingCopy would allocate, computed without allocating.
func workingDims(src image.Point, s float64) image.Rectangle {
	if s >= 1 {
		return image.Rectangle{Max: src}
	}
	return image.Rect(0, 0, max(1, int(float64(src.X)*s)), max(1, int(float64(src.Y)*s)))
}

// Fill on an extreme aspect is the case a min()-based cap would break: the crop
// is a thin band whose source-pixel width must survive the working copy.
func TestFillKeepsSourcePixelRatioOnExtremeAspect(t *testing.T) {
	src := image.Pt(1440, 14000)
	m := &galleryModel{
		curSize: src,
		l:       layout{previewW: 200, previewH: 50},
		cellW:   10,
		cellH:   20,
		crop:    fullCrop(),
	}
	m.toggleFill()
	if m.crop.isFull() {
		t.Fatal("fill crop must be a band, not the full image")
	}

	s := workingScale(src, m.l.previewW*m.cellWpx(), m.l.previewH*m.cellHpx())
	inSrc := cropPixels(image.Rectangle{Max: src}, m.crop)
	inWork := cropPixels(workingDims(src, s), m.crop)
	if inWork.Dx() != inSrc.Dx() || inWork.Dy() != inSrc.Dy() {
		t.Errorf("crop is %v px on the working copy, %v on the source (s=%v)", inWork.Size(), inSrc.Size(), s)
	}
}

// The derivation, not the formula: for every crop whose longer side is at least
// 1/zoomMax, the working copy is at least as sharp as the render the box can
// show, s >= min(1, rx/w, ry/h).
func TestWorkingScaleCoversEveryNonRegionCrop(t *testing.T) {
	// hash is an inline integer mix (splitmix64 finaliser) so the sweep is
	// deterministic without math/rand.
	hash := func(v uint64) uint64 {
		v += 0x9e3779b97f4a7c15
		v = (v ^ v>>30) * 0xbf58476d1ce4e5b9
		v = (v ^ v>>27) * 0x94d049bb133111eb
		return v ^ v>>31
	}
	checked := 0
	for i := range uint64(400) {
		src := image.Pt(1+int(hash(i*4)%20000), 1+int(hash(i*4+1)%20000))
		boxW, boxH := 1+int(hash(i*4+2)%4000), 1+int(hash(i*4+3)%3000)
		rx, ry := float64(boxW)/float64(src.X), float64(boxH)/float64(src.Y)
		s := workingScale(src, boxW, boxH)
		for j := range uint64(64) {
			w := float64(1+hash(i<<8|j)%1000) / 1000
			h := float64(1+hash(i<<8|j|1<<20)%1000) / 1000
			if max(w, h) < 1/zoomMax {
				continue
			}
			checked++
			if need := min(1, rx/w, ry/h); s < need-1e-9 {
				t.Fatalf("src %v box %dx%d crop %.3fx%.3f: s=%v < needed %v", src, boxW, boxH, w, h, s, need)
			}
		}
	}
	if checked < 5000 {
		t.Fatalf("only %d crops exercised", checked)
	}
}

func TestWorkingCopy(t *testing.T) {
	t.Run("NRGBA64 narrows to RGBA", func(t *testing.T) {
		src := image.NewNRGBA64(image.Rect(0, 0, 4, 3))
		src.SetNRGBA64(1, 2, color.NRGBA64{R: 0xffff, G: 0x8080, B: 0, A: 0xffff})
		got, ok := workingCopy(src, 1).(*image.RGBA)
		if !ok {
			t.Fatalf("got %T, want *image.RGBA", workingCopy(src, 1))
		}
		if got.Bounds() != src.Bounds() {
			t.Errorf("bounds = %v, want %v", got.Bounds(), src.Bounds())
		}
		if px := got.RGBAAt(1, 2); px != (color.RGBA{R: 0xff, G: 0x80, B: 0, A: 0xff}) {
			t.Errorf("pixel = %v", px)
		}
	})
	t.Run("RGBA64 narrows to RGBA", func(t *testing.T) {
		src := image.NewRGBA64(image.Rect(2, 3, 6, 6))
		got, ok := workingCopy(src, 1).(*image.RGBA)
		if !ok {
			t.Fatalf("got %T, want *image.RGBA", workingCopy(src, 1))
		}
		if got.Bounds() != src.Bounds() {
			t.Errorf("bounds = %v, want %v", got.Bounds(), src.Bounds())
		}
	})
	t.Run("Gray16 narrows to Gray", func(t *testing.T) {
		src := image.NewGray16(image.Rect(0, 0, 4, 3))
		src.SetGray16(3, 1, color.Gray16{Y: 0xffff})
		got, ok := workingCopy(src, 1).(*image.Gray)
		if !ok {
			t.Fatalf("got %T, want *image.Gray", workingCopy(src, 1))
		}
		if got.Bounds() != src.Bounds() {
			t.Errorf("bounds = %v, want %v", got.Bounds(), src.Bounds())
		}
		if px := got.GrayAt(3, 1); px.Y != 0xff {
			t.Errorf("pixel = %v", px)
		}
	})
	t.Run("8-bit and YCbCr are returned as-is", func(t *testing.T) {
		for _, src := range []image.Image{
			image.NewRGBA(image.Rect(0, 0, 4, 3)),
			image.NewYCbCr(image.Rect(0, 0, 4, 3), image.YCbCrSubsampleRatio420),
		} {
			if got := workingCopy(src, 1); got != src {
				t.Errorf("%T was copied, want the same value", src)
			}
		}
	})
	t.Run("scaling yields an RGBA of the scaled size", func(t *testing.T) {
		for _, src := range []image.Image{
			image.NewNRGBA64(image.Rect(0, 0, 8, 6)),
			image.NewRGBA(image.Rect(0, 0, 8, 6)),
		} {
			got, ok := workingCopy(src, 0.5).(*image.RGBA)
			if !ok {
				t.Fatalf("%T: got %T, want *image.RGBA", src, workingCopy(src, 0.5))
			}
			if want := image.Rect(0, 0, 4, 3); got.Bounds() != want {
				t.Errorf("%T: bounds = %v, want %v", src, got.Bounds(), want)
			}
		}
	})
	t.Run("scaled size never collapses to zero", func(t *testing.T) {
		got := workingCopy(image.NewRGBA(image.Rect(0, 0, 4, 3)), 0.01)
		if got.Bounds().Dx() < 1 || got.Bounds().Dy() < 1 {
			t.Errorf("bounds = %v", got.Bounds())
		}
	})
}

// encode16BitPNG is a w x h 16-bit PNG — the depth this decode path exists for —
// filled with a colour derived from seed so fixtures differ.
func encode16BitPNG(t *testing.T, w, h int, seed uint16) []byte {
	t.Helper()
	img := image.NewNRGBA64(image.Rect(0, 0, w, h))
	c := color.NRGBA64{R: seed, G: ^seed, B: seed / 2, A: 0xffff}
	for y := range h {
		for x := range w {
			img.SetNRGBA64(x, y, c)
		}
	}
	return encodePNG(t, img)
}

// encodeNoisePNG is a 16-bit PNG that barely compresses, so decoding it takes
// many reads rather than one buffered chunk.
func encodeNoisePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	return encodePNG(t, noiseNRGBA64(w, h, 1))
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var (
	sizeA = image.Pt(400, 300)
	sizeB = image.Pt(320, 240)
	sizeC = image.Pt(200, 150)
)

// newDecodeModel builds a kitty model over one 16-bit PNG per size, selecting
// the first, the way a pane starts: ensureDecoded before sizing, then the first
// WindowSizeMsg (which arms the initial decode). Nothing is decoded yet.
func newDecodeModel(t *testing.T, sizes ...image.Point) galleryModel {
	t.Helper()
	dir := t.TempDir()
	tty, err := os.CreateTemp(dir, "tty")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tty.Close() }) //nolint:errcheck,gosec // test cleanup
	m := galleryModel{
		pane:    "dec-" + strings.ReplaceAll(t.Name(), "/", "_"),
		backend: backendKitty,
		tty:     tty,
		crop:    fullCrop(),
		cellW:   10,
		cellH:   20,
	}
	for i, sz := range sizes {
		p := filepath.Join(dir, fmt.Sprintf("img%d.png", i))
		if err := os.WriteFile(p, encode16BitPNG(t, sz.X, sz.Y, uint16(i+1)*0x3000), 0o600); err != nil {
			t.Fatal(err)
		}
		m.images = append(m.images, imageEntry{Path: p})
	}
	for _, p := range []string{m.zoomRawPath(), m.zoomScratchPath()} {
		os.Remove(p)                       //nolint:errcheck,gosec // stale scratch from an earlier run
		t.Cleanup(func() { os.Remove(p) }) //nolint:errcheck,gosec // test cleanup
	}
	m.ensureDecoded()
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	return m
}

func send(t *testing.T, m galleryModel, msg tea.Msg) (galleryModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(galleryModel), cmd
}

// runDecode stands in for the debounce tick: it delivers the kick for the
// latest request and runs the decode synchronously, returning its result
// undelivered.
func runDecode(t *testing.T, m galleryModel) (galleryModel, decodedMsg) {
	t.Helper()
	m, cmd := send(t, m, decodeKickMsg{gen: m.decodeGen})
	if cmd == nil {
		t.Fatal("kick for the current request started no decode")
	}
	res, ok := cmd().(decodedMsg)
	if !ok {
		t.Fatal("decode cmd did not return a decodedMsg")
	}
	return m, res
}

// land runs the latest decode and delivers its result.
func land(t *testing.T, m galleryModel) galleryModel {
	t.Helper()
	m, res := runDecode(t, m)
	m, _ = send(t, m, res)
	return m
}

func ttyLen(t *testing.T, m galleryModel) int64 {
	t.Helper()
	fi, err := m.tty.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

var (
	keyNext   = tea.KeyPressMsg{Text: "l", Code: 'l'}
	keyPrev   = tea.KeyPressMsg{Text: "h", Code: 'h'}
	keyZoom   = tea.KeyPressMsg{Text: "z", Code: 'z'}
	keyUnzoom = tea.KeyPressMsg{Text: "0", Code: '0'}
)

func TestSwitchDefersDecode(t *testing.T) {
	m := land(t, newDecodeModel(t, sizeA, sizeB))
	if m.curImg == nil {
		t.Fatal("initial selection did not land")
	}
	gen := m.decodeGen
	m, cmd := send(t, m, keyNext)
	if m.curImg != nil {
		t.Error("switch decoded synchronously (or kept the previous image)")
	}
	if m.curSize != sizeB {
		t.Errorf("curSize = %v, want %v", m.curSize, sizeB)
	}
	if m.decodeGen == gen {
		t.Error("switch did not request a decode")
	}
	if cmd == nil {
		t.Error("switch scheduled no decode")
	}
}

// A zoom/pan pressed before the pixels exist applies to the geometry at once,
// stores nothing while pending, and is painted when the decode lands.
func TestZoomDuringDecodeWindowApplies(t *testing.T) {
	cases := []struct {
		name  string
		input func(m galleryModel) []tea.Msg
	}{
		{"key zoom", func(galleryModel) []tea.Msg { return []tea.Msg{keyZoom} }},
		{"wheel zoom", func(m galleryModel) []tea.Msg {
			pr := m.previewRect()
			return []tea.Msg{tea.MouseWheelMsg{X: pr.x + pr.w/2, Y: pr.y + pr.h/2, Button: tea.MouseWheelUp}}
		}},
		// One zoom step crops a single axis, so pan along both to be sure one moves.
		{"pan after zoom", func(galleryModel) []tea.Msg {
			return []tea.Msg{keyZoom, keyNext, tea.KeyPressMsg{Text: "j", Code: 'j'}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := land(t, newDecodeModel(t, sizeA, sizeB))
			m, _ = send(t, m, keyNext)
			before := ttyLen(t, m)
			var crops []cropFrac
			for _, msg := range tc.input(m) {
				m, _ = send(t, m, msg)
				crops = append(crops, m.crop)
			}
			if m.crop.isFull() {
				t.Fatal("input during the decode window did not change the crop")
			}
			if len(crops) > 1 && crops[0] == crops[len(crops)-1] {
				t.Fatal("pan during the decode window did not move the crop")
			}
			if got := ttyLen(t, m); got != before {
				t.Fatalf("stored %d bytes while pixels were pending", got-before)
			}
			crop := m.crop

			m = land(t, m)
			if m.curImg == nil {
				t.Fatal("decode did not land")
			}
			if ttyLen(t, m) == before {
				t.Error("landing did not re-store the zoomed preview")
			}
			if _, err := os.Stat(m.zoomRawPath()); err != nil {
				t.Errorf("zoomed frame not rendered: %v", err)
			}
			if m.crop != crop {
				t.Errorf("landing changed the crop: %+v, want %+v", m.crop, crop)
			}
		})
	}
}

// Zooming back out while pending must re-store the full frame: on a d2 entry a
// sharp zoomed vector frame may be up.
func TestZoomOutToFullWhilePendingRestores(t *testing.T) {
	m := land(t, newDecodeModel(t, sizeA, sizeB))
	m, _ = send(t, m, keyNext)
	m, _ = send(t, m, keyZoom)
	before := ttyLen(t, m)
	m, _ = send(t, m, keyUnzoom)
	if !m.crop.isFull() {
		t.Fatalf("0 left the crop at %+v", m.crop)
	}
	if ttyLen(t, m) == before {
		t.Error("zoom-out to full while pending stored nothing")
	}
}

func TestStaleDecodeDropped(t *testing.T) {
	t.Run("A result after switching to B", func(t *testing.T) {
		m := newDecodeModel(t, sizeA, sizeB)
		genA := m.decodeGen
		m, resA := runDecode(t, m)
		m, _ = send(t, m, keyNext)
		m, _ = send(t, m, resA)
		if m.curImg != nil {
			t.Fatal("A's result landed on B")
		}
		if _, cmd := send(t, m, decodeKickMsg{gen: genA}); cmd != nil {
			t.Error("A's stale kick started a decode")
		}
		if m = land(t, m); m.curSize != sizeB || m.curImg == nil {
			t.Errorf("B did not land: size %v", m.curSize)
		}
	})
	t.Run("A B C with results out of order", func(t *testing.T) {
		m := newDecodeModel(t, sizeA, sizeB, sizeC)
		m, _ = send(t, m, keyNext)
		m, resB := runDecode(t, m)
		m, _ = send(t, m, keyNext)
		m, resC := runDecode(t, m)
		m, _ = send(t, m, resC)
		if m.curImg != resC.img {
			t.Fatal("C's result was not accepted")
		}
		m, _ = send(t, m, resB)
		if m.curImg != resC.img || m.curSize != sizeC {
			t.Errorf("B's late result replaced C's (size %v)", m.curSize)
		}
	})
	t.Run("A B A drops the first A by generation", func(t *testing.T) {
		m := newDecodeModel(t, sizeA, sizeB)
		m, resA1 := runDecode(t, m)
		m, _ = send(t, m, keyNext)
		m, _ = send(t, m, keyPrev)
		if m.curImgPath != resA1.path {
			t.Fatal("did not return to A")
		}
		m, _ = send(t, m, resA1)
		if m.curImg != nil {
			t.Fatal("first A's result landed on the second A request")
		}
		m, resA2 := runDecode(t, m)
		if m, _ = send(t, m, resA2); m.curImg != resA2.img {
			t.Error("second A's result was not accepted")
		}
	})
}

// cancelAfter cancels its context once the first chunk has been read, the way
// requestDecode lands in the middle of a running decode.
type cancelAfter struct {
	r      io.Reader
	cancel context.CancelFunc
}

func (c *cancelAfter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.cancel()
	return n, err
}

func TestDecodeCancelAbortsPromptly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.png")
	data := encode16BitPNG(t, sizeA.X, sizeA.Y, 0x4000)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("cancelled before the decode", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if img, _, _ := decodeWorking(ctx, path, 1000, 800); img != nil {
			t.Error("cancelled decode returned an image")
		}
	})
	t.Run("cancelled mid-decode stops reading", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		src := bytes.NewReader(encodeNoisePNG(t, sizeA.X, sizeA.Y))
		_, _, err := image.Decode(ctxReader{ctx, &cancelAfter{r: src, cancel: cancel}})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if src.Len() == 0 {
			t.Error("decode read the whole file after cancellation")
		}
	})
	t.Run("requestDecode cancels the running decode", func(t *testing.T) {
		cancelled := false
		m := galleryModel{decodeCancel: func() { cancelled = true }}
		m.requestDecode()
		if !cancelled {
			t.Error("stored cancel not called")
		}
		if m.decodeCancel != nil {
			t.Error("cancel kept after use")
		}
	})
}

func TestDecodeErrorResetsGeometry(t *testing.T) {
	m := land(t, newDecodeModel(t, sizeA))
	data := encode16BitPNG(t, sizeB.X, sizeB.Y, 0x5000)
	idat := bytes.Index(data, []byte("IDAT"))
	if idat < 0 {
		t.Fatal("fixture has no IDAT chunk")
	}
	broken := filepath.Join(t.TempDir(), "broken.png")
	if err := os.WriteFile(broken, data[:idat+16], 0o600); err != nil {
		t.Fatal(err)
	}
	m.images = append(m.images, imageEntry{Path: broken})

	m, _ = send(t, m, keyNext)
	if m.curSize != sizeB {
		t.Fatalf("curSize = %v, want the header's %v", m.curSize, sizeB)
	}
	m, _ = send(t, m, keyZoom)
	if m.crop.isFull() {
		t.Fatal("zoom did not apply to the known size")
	}
	m = land(t, m)
	if m.curSize != (image.Point{}) {
		t.Errorf("curSize = %v after a failed decode, want zero", m.curSize)
	}
	if !m.crop.isFull() {
		t.Errorf("crop = %+v after a failed decode, want full", m.crop)
	}
	if m.curImg != nil {
		t.Error("failed decode left an image")
	}
}

// A d2 zoom has a sharp resvg render scheduled for the crop; the bitmap landing
// must not overwrite it.
func TestD2LandingSkipsBitmapRestore(t *testing.T) {
	m := land(t, newDecodeModel(t, sizeA, sizeB))
	if len(m.images) < 2 {
		t.Fatalf("model has %d images, want 2", len(m.images))
	}
	m.images[1].Vector = filepath.Join(t.TempDir(), "b.svg")
	m, _ = send(t, m, keyNext)
	m, _ = send(t, m, keyZoom)
	before := ttyLen(t, m)
	m = land(t, m)
	if m.curImg == nil {
		t.Fatal("decode did not land")
	}
	if got := ttyLen(t, m); got != before {
		t.Errorf("d2 landing re-stored %d bitmap bytes", got-before)
	}
}

func TestResizeRequestsSharperCopy(t *testing.T) {
	for _, tc := range []struct {
		scale float64
		want  bool
	}{{0.5, true}, {1, false}} {
		t.Run(fmt.Sprintf("scale %v", tc.scale), func(t *testing.T) {
			m := galleryModel{
				backend:  backendKitty,
				ready:    true,
				cellW:    10,
				cellH:    20,
				crop:     fullCrop(),
				curImg:   image.NewRGBA(image.Rect(0, 0, 4, 4)),
				curSize:  image.Pt(16000, 9000),
				curScale: tc.scale,
			}
			m.l = computeLayout(80, 30)
			gen := m.decodeGen
			m, _ = send(t, m, tea.WindowSizeMsg{Width: 200, Height: 60})
			if got := m.decodeGen != gen; got != tc.want {
				t.Errorf("re-requested = %v, want %v", got, tc.want)
			}
			if m.curImg == nil {
				t.Error("resize dropped the current copy before the sharper one landed")
			}
		})
	}
}

// Symbols caches on the crop: a zoom pressed while pending must not cache the
// unzoomed block under the zoomed key, or the zoom never shows after landing.
func TestSymbolsZoomDuringDecodeWindow(t *testing.T) {
	resetSymbolsCache(t)
	var previewSrc []string
	m := land(t, newDecodeModel(t, sizeA, sizeB))
	m.backend = backendSymbols
	previewSize := fmt.Sprintf("%dx%d", m.l.previewW, m.l.previewH)
	runChafa = func(args []string) ([]byte, error) {
		if args[3] == previewSize {
			previewSrc = append(previewSrc, args[len(args)-1])
		}
		return []byte("ART\n"), nil
	}
	clearSymbols := func() {
		symbolsCacheMu.Lock()
		symbolsCache = map[string]string{}
		symbolsCacheMu.Unlock()
	}

	m, _ = send(t, m, keyNext)
	clearSymbols()
	previewSrc = nil
	m, _ = send(t, m, keyZoom)
	m.View()
	if len(m.images) < 2 {
		t.Fatalf("model has %d images, want 2", len(m.images))
	}
	if want := []string{m.images[1].Path}; !equalStrings(previewSrc, want) {
		t.Fatalf("pending zoom rendered %q, want the original %q", previewSrc, want)
	}

	m = land(t, m)
	previewSrc = nil
	m.View()
	if want := []string{m.zoomScratchPath()}; !equalStrings(previewSrc, want) {
		t.Errorf("after landing rendered %q, want the zoom %q", previewSrc, want)
	}
}

func equalStrings(a, b []string) bool {
	return strings.Join(a, "\x00") == strings.Join(b, "\x00")
}

// A capped copy decoded for a small box must be re-requested when the box grew
// before it landed.
func TestLandingReRequestsWhenBoxGrew(t *testing.T) {
	src := image.Pt(16000, 9000)
	for _, tc := range []struct {
		name string
		grow bool
	}{{"box grew", true}, {"same box", false}} {
		t.Run(tc.name, func(t *testing.T) {
			m := newDecodeModel(t, sizeA)
			m, _ = send(t, m, decodeKickMsg{gen: m.decodeGen})
			bw, bh := m.previewBoxPx()
			scale := workingScale(src, bw, bh)
			if scale >= 1 {
				t.Fatalf("fixture box %dx%d does not cap %v", bw, bh, src)
			}
			if tc.grow {
				m, _ = send(t, m, tea.WindowSizeMsg{Width: 200, Height: 60})
			}
			gen := m.decodeGen
			img := image.NewRGBA(image.Rect(0, 0, 4, 4))
			m, _ = send(t, m, decodedMsg{gen: gen, path: m.curImgPath, img: img, src: src, scale: scale})
			if m.curImg != img || m.curSize != src {
				t.Fatalf("landing not applied: size %v", m.curSize)
			}
			if got := m.decodeGen != gen; got != tc.grow {
				t.Errorf("re-requested = %v, want %v", got, tc.grow)
			}
		})
	}
}

func TestArmDecodeOncePerGen(t *testing.T) {
	m := land(t, newDecodeModel(t, sizeA, sizeB))
	m, cmd := send(t, m, keyNext)
	if cmd == nil || m.decodeArmedGen != m.decodeGen {
		t.Fatal("switch did not arm its decode")
	}
	if _, cmd = send(t, m, updateProbeMsg{}); cmd != nil {
		t.Error("an unrelated message armed the same request again")
	}

	unready := m
	unready.ready = false
	unready.decodeGen++
	if unready.armDecode() != nil {
		t.Error("armed before the box is sized")
	}
	unsized := m
	unsized.curSize = image.Point{}
	unsized.decodeGen++
	if unsized.armDecode() != nil {
		t.Error("armed for an unreadable header")
	}
}
