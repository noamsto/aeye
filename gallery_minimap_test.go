package main

import (
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMinimapFit(t *testing.T) {
	box := image.Pt(180, 200)
	tests := []struct {
		name     string
		src, box image.Point
		want     image.Point
	}{
		// scale = min(180/1000, 200/250) = 0.18 -> 180 x 45.
		{"wide letterbox", image.Pt(1000, 250), box, image.Pt(180, 45)},
		// scale = min(180/250, 200/1000) = 0.2 -> 50 x 200.
		{"tall letterbox", image.Pt(250, 1000), box, image.Pt(50, 200)},
		// scale = 0.045 -> 180 x 4.5, which rounds half away from zero to 5.
		{"extreme wide", image.Pt(4000, 100), box, image.Pt(180, 5)},
		// scale = 0.05 -> 4.5 x 200 -> 5 x 200.
		{"extreme tall", image.Pt(100, 4000), box, image.Pt(5, 200)},
		{"zero src", image.Pt(0, 100), box, image.Point{}},
		{"zero box", image.Pt(100, 100), image.Pt(180, 0), image.Point{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := minimapFit(tt.src, tt.box); got != tt.want {
				t.Errorf("minimapFit(%v, %v) = %v, want %v", tt.src, tt.box, got, tt.want)
			}
		})
	}
}

func TestMinimapRect(t *testing.T) {
	tests := []struct {
		name string
		fit  image.Point
		crop cropFrac
		want image.Rectangle
	}{
		// 0.25*180=45 .. 0.5*180=90 across; full height 45.
		{"wide letterbox", image.Pt(180, 45), cropFrac{0.25, 0, 0.5, 1}, image.Rect(45, 0, 90, 45)},
		// 0.5*200=100 .. 0.75*200=150 down; full width 50.
		{"tall letterbox", image.Pt(50, 200), cropFrac{0, 0.5, 1, 0.75}, image.Rect(0, 100, 50, 150)},
		// x: 0.4375*180=78.75 -> 79, width 0.125*180=22.5 -> 23; height 5 < 6 but fit.Y is 5, so it spans full.
		{"extreme wide", image.Pt(180, 5), cropFrac{0.4375, 0, 0.5625, 1}, image.Rect(79, 0, 102, 5)},
		// y: 0.4375*200=87.5 -> 88, height 0.125*200=25; width 5 < 6 but fit.X is 5, so it spans full.
		{"extreme tall", image.Pt(5, 200), cropFrac{0, 0.4375, 1, 0.5625}, image.Rect(0, 88, 5, 113)},
		// 1.8 px -> 2 px on each axis; grown to 6 around the centre round(0.5*180)=90 -> 87..93.
		{"min-side widening", image.Pt(180, 180), cropFrac{0.495, 0.495, 0.505, 0.505}, image.Rect(87, 87, 93, 93)},
		// 2 px at the origin; centre round(0.9)=1, so 1-3=-2 clamps to 0 and the rect is 0..6.
		{"clamp at edge", image.Pt(180, 180), cropFrac{0, 0, 0.01, 0.01}, image.Rect(0, 0, 6, 6)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := minimapRect(tt.fit, tt.crop); got != tt.want {
				t.Errorf("minimapRect(%v, %+v) = %v, want %v", tt.fit, tt.crop, got, tt.want)
			}
		})
	}

	t.Run("pan keeps size", func(t *testing.T) {
		fit := image.Pt(180, 45)
		// Both crops are 0.25 wide = 45 px; only the origin rounds differently.
		for _, c := range []cropFrac{{0.25, 0, 0.5, 1}, {0.2537, 0, 0.5037, 1}} {
			if got := minimapRect(fit, c).Dx(); got != 45 {
				t.Errorf("minimapRect(%v, %+v).Dx() = %d, want 45", fit, c, got)
			}
		}
	})
}

func TestMinimapBoxUsesMeasuredCell(t *testing.T) {
	m := &galleryModel{cellW: 7, cellH: 15}
	m.l.stripW, m.l.stripH = 18, 10
	// 18*7=126 wide, 10*15=150 tall.
	box := m.minimapBox()
	if want := image.Pt(126, 150); box != want {
		t.Fatalf("minimapBox() = %v, want %v", box, want)
	}
	// scale = min(126/1000, 150/250) = 0.126 -> 126 x 31.5, which rounds to 32.
	if got, want := minimapFit(image.Pt(1000, 250), box), image.Pt(126, 32); got != want {
		t.Errorf("minimapFit = %v, want %v", got, want)
	}

	// Unmeasured cell falls back to cellPxW x cellPxH = 10 x 20: 18*10=180, 10*20=200.
	u := &galleryModel{}
	u.l.stripW, u.l.stripH = 18, 10
	if got, want := u.minimapBox(), image.Pt(180, 200); got != want {
		t.Errorf("unmeasured minimapBox() = %v, want %v", got, want)
	}
}

func TestDrawViewport(t *testing.T) {
	grey := color.RGBA{128, 128, 128, 255}
	black := color.RGBA{0, 0, 0, 255}
	white := color.RGBA{255, 255, 255, 255}
	dst := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for i := 0; i < len(dst.Pix); i += 4 {
		copy(dst.Pix[i:], []byte{128, 128, 128, 255})
	}

	drawViewport(dst, image.Rect(4, 4, 14, 14))

	for _, tt := range []struct {
		x, y int
		want color.RGBA
	}{
		{4, 4, black}, {13, 9, black}, {9, 13, black}, {4, 13, black},
		{5, 5, white}, {12, 9, white}, {9, 12, white}, {12, 12, white},
		{9, 9, grey}, {3, 3, grey}, {14, 14, grey},
	} {
		if got := dst.RGBAAt(tt.x, tt.y); got != tt.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", tt.x, tt.y, got, tt.want)
		}
	}

	drawViewport(dst, image.Rect(-3, -3, 5, 25))
	drawViewport(dst, image.Rect(18, 18, 40, 40))
	drawViewport(dst, image.Rect(-9, -9, -2, -2))
}

func TestMinimapPathsDistinct(t *testing.T) {
	m := &galleryModel{pane: "%41"}
	if m.minimapRawPath() == m.zoomRawPath() {
		t.Errorf("minimapRawPath collides with zoomRawPath: %s", m.minimapRawPath())
	}
	if m.minimapPNGPath() == m.zoomScratchPath() {
		t.Errorf("minimapPNGPath collides with zoomScratchPath: %s", m.minimapPNGPath())
	}
	if !strings.HasSuffix(m.minimapRawPath(), ".raw") {
		t.Errorf("minimapRawPath = %s, want .raw suffix", m.minimapRawPath())
	}
	if !strings.HasSuffix(m.minimapPNGPath(), ".png") {
		t.Errorf("minimapPNGPath = %s, want .png suffix", m.minimapPNGPath())
	}
}

// kittyStore is one parsed a=T transmit: its control keys and decoded file path.
type kittyStore struct {
	keys map[string]string
	path string
}

// apcBodies returns the body of every kitty APC in buf, in order. TMUX must be
// unset so the APCs are not passthrough-wrapped.
func apcBodies(buf []byte) []string {
	var out []string
	rest := string(buf)
	for {
		_, after, ok := strings.Cut(rest, "\x1b_G")
		if !ok {
			return out
		}
		body, next, ok := strings.Cut(after, "\x1b\\")
		if !ok {
			return out
		}
		out = append(out, body)
		rest = next
	}
}

// apcKeys splits an APC body into its control keys and payload.
func apcKeys(body string) (map[string]string, string) {
	ctrl, payload, _ := strings.Cut(body, ";")
	keys := map[string]string{}
	for kv := range strings.SplitSeq(ctrl, ",") {
		k, v, _ := strings.Cut(kv, "=")
		keys[k] = v
	}
	return keys, payload
}

// storesFor returns every a=T store for image id in buf, in order.
func storesFor(t *testing.T, buf []byte, id int) []kittyStore {
	t.Helper()
	var out []kittyStore
	for _, body := range apcBodies(buf) {
		keys, payload := apcKeys(body)
		if keys["i"] != strconv.Itoa(id) || keys["a"] != "T" {
			continue
		}
		p, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			t.Fatalf("store payload %q is not base64: %v", payload, err)
		}
		out = append(out, kittyStore{keys: keys, path: string(p)})
	}
	return out
}

// added is what the model wrote after the recorder held before bytes.
func added(rec *transmitRecorder, before int) []byte {
	rec.Len()
	return rec.buf.Bytes()[before:]
}

func writeSizedPNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		copy(img.Pix[i:], []byte{128, 128, 128, 255})
	}
	path := filepath.Join(t.TempDir(), "sized.png")
	f, err := os.Create(path) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck // test cleanup
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

func newMinimapModel(t *testing.T, rec *transmitRecorder) *galleryModel {
	t.Helper()
	t.Setenv("TMUX", "")
	// The scratch rasters are keyed by pane under os.TempDir(); a private dir
	// keeps concurrent test processes from overwriting or removing each other's.
	t.Setenv("TMPDIR", t.TempDir())
	m := newTransmitModel(t, rec, 3)
	if len(m.images) < 2 {
		t.Fatalf("newMinimapModel needs at least 2 images, got %d", len(m.images))
	}
	p := writeSizedPNG(t, 400, 100)
	m.images[1] = imageEntry{Path: p, Mtime: 20}
	m.cursor, m.crop = 1, fullCrop()
	m.curImgPath, m.curSize = p, image.Pt(400, 100)
	m.curImg = image.NewRGBA(image.Rect(0, 0, 400, 100))
	return m
}

func selectedSlotID(m *galleryModel) int {
	return m.stripID(m.cursor - stripStart(m.cursor, m.l.stripCols, len(m.images)))
}

func assertKey(t *testing.T, s kittyStore, key string, want int) {
	t.Helper()
	if got := s.keys[key]; got != strconv.Itoa(want) {
		t.Errorf("store key %s = %q, want %d", key, got, want)
	}
}

// onlyStore fails unless buf holds exactly one a=T store for id.
func onlyStore(t *testing.T, buf []byte, id int) kittyStore {
	t.Helper()
	got := storesFor(t, buf, id)
	if len(got) != 1 {
		t.Fatalf("stores for id %d = %d, want 1", id, len(got))
	}
	return got[0]
}

func assertPlain(t *testing.T, s kittyStore, m *galleryModel) {
	t.Helper()
	assertKey(t, s, "f", 100)
	if m.cursor >= len(m.images) {
		t.Fatalf("assertPlain: cursor %d out of range (%d images)", m.cursor, len(m.images))
	}
	if want := cachedPNG(m.images[m.cursor].Path, m.l.stripW, m.l.stripH); s.path != want {
		t.Errorf("plain store path = %s, want %s", s.path, want)
	}
}

func readRaw(t *testing.T, path string, fit image.Point) *image.RGBA {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // scratch path the model wrote
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != fit.X*fit.Y*4 {
		t.Fatalf("raw file is %d bytes, want %d for %v", len(b), fit.X*fit.Y*4, fit)
	}
	return &image.RGBA{Pix: b, Stride: fit.X * 4, Rect: image.Rect(0, 0, fit.X, fit.Y)}
}

// assertOverlay checks s is the raw overlay store for m's current crop and that
// the raster it points at carries the viewport rectangle. It returns that rectangle.
func assertOverlay(t *testing.T, m *galleryModel, s kittyStore) image.Rectangle {
	t.Helper()
	fit := minimapFit(m.curSize, m.minimapBox())
	assertKey(t, s, "f", 32)
	assertKey(t, s, "s", fit.X)
	assertKey(t, s, "v", fit.Y)
	assertKey(t, s, "c", m.l.stripW)
	assertKey(t, s, "r", m.l.stripH)
	if s.path != m.minimapRawPath() {
		t.Errorf("overlay path = %s, want %s", s.path, m.minimapRawPath())
	}
	img := readRaw(t, s.path, fit)
	r := minimapRect(fit, m.crop)
	black, white := color.RGBA{0, 0, 0, 255}, color.RGBA{255, 255, 255, 255}
	if got := img.RGBAAt(r.Min.X, r.Min.Y); got != black {
		t.Errorf("pixel at rect Min %v = %v, want black", r.Min, got)
	}
	if got := img.RGBAAt(r.Min.X+1, r.Min.Y+1); got != white {
		t.Errorf("pixel at rect Min+(1,1) = %v, want white", got)
	}
	found := false
	for y := 0; y < fit.Y; y++ {
		for x := 0; x < fit.X; x++ {
			if image.Pt(x, y).In(r.Inset(-2)) {
				continue
			}
			found = true
			if c := img.RGBAAt(x, y); c == black || c == white {
				t.Fatalf("pixel (%d,%d) outside %v = %v, want the grey thumb", x, y, r, c)
			}
		}
	}
	if !found {
		t.Fatalf("rect %v covers the whole %v raster", r, fit)
	}
	return r
}

func zoomIn(m *galleryModel) {
	m.zoomBy(zoomStep)
	m.transmitPreviewOnly()
}

func TestMinimapAbsentWhenFull(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newMinimapModel(t, rec)

	m.transmitView()
	assertPlain(t, onlyStore(t, added(rec, 0), selectedSlotID(m)), m)

	before := rec.Len()
	m.syncMinimap()
	if n := len(added(rec, before)); n != 0 {
		t.Errorf("syncMinimap at full crop wrote %d bytes, want 0", n)
	}
}

func TestMinimapOverlayOnZoom(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newMinimapModel(t, rec)
	m.transmitView()

	before := rec.Len()
	zoomIn(m)
	if m.crop.isFull() {
		t.Fatal("zoomBy left the crop full")
	}
	out := added(rec, before)
	s := onlyStore(t, out, selectedSlotID(m))
	assertOverlay(t, m, s)
	if s.path == m.zoomRawPath() || s.path == m.zoomScratchPath() {
		t.Errorf("overlay shares a scratch file with the preview: %s", s.path)
	}
	for i := range m.l.stripCols {
		if id := m.stripID(i); id != selectedSlotID(m) {
			if got := storesFor(t, out, id); len(got) != 0 {
				t.Errorf("strip id %d re-stored %d times, want 0", id, len(got))
			}
		}
	}
}

func TestMinimapFollowsPan(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newMinimapModel(t, rec)
	m.transmitView()
	zoomIn(m)
	fit := minimapFit(m.curSize, m.minimapBox())
	old := minimapRect(fit, m.crop)

	before := rec.Len()
	m.panBy(0.3, 0)
	m.transmitPreviewOnly()
	got := assertOverlay(t, m, onlyStore(t, added(rec, before), selectedSlotID(m)))
	if got.Min == old.Min {
		t.Errorf("rect Min stayed at %v after a pan", old.Min)
	}
}

func TestMinimapDedupe(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newMinimapModel(t, rec)
	m.transmitView()
	zoomIn(m)

	before := rec.Len()
	m.transmitPreviewOnly()
	if got := storesFor(t, added(rec, before), selectedSlotID(m)); len(got) != 0 {
		t.Errorf("an unchanged crop re-stored the thumb %d times", len(got))
	}
}

func TestMinimapFollowsWheel(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newMinimapModel(t, rec)
	m.transmitView()

	before := rec.Len()
	pr := m.previewRect()
	mm, _ := m.handleMouse(tea.MouseWheelMsg{X: pr.x + pr.w/2, Y: pr.y + pr.h/2, Button: tea.MouseWheelUp})
	if mm.crop.isFull() {
		t.Fatal("wheel up did not zoom")
	}
	assertOverlay(t, &mm, onlyStore(t, added(rec, before), selectedSlotID(m)))
}

func TestMinimapFollowsDrag(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newMinimapModel(t, rec)
	m.transmitView()
	zoomIn(m)
	fit := minimapFit(m.curSize, m.minimapBox())
	old := minimapRect(fit, m.crop)

	pr := m.previewRect()
	cx, cy := pr.x+pr.w/2, pr.y+pr.h/2
	m2, _ := m.handleMouse(tea.MouseClickMsg{X: cx, Y: cy, Button: tea.MouseLeft})
	before := rec.Len()
	m3, _ := m2.handleMouse(tea.MouseMotionMsg{X: cx + 5, Y: cy + 3, Button: tea.MouseLeft})
	got := assertOverlay(t, &m3, onlyStore(t, added(rec, before), selectedSlotID(m)))
	if got.Min == old.Min {
		t.Errorf("rect Min stayed at %v after a drag", old.Min)
	}
}

func TestMinimapReturnsToPlain(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newMinimapModel(t, rec)
	m.transmitView()
	zoomIn(m)

	before := rec.Len()
	m.crop = fullCrop()
	m.transmitPreviewOnly()
	assertPlain(t, onlyStore(t, added(rec, before), selectedSlotID(m)), m)
}

func TestMinimapShownInFillAndRegion(t *testing.T) {
	for _, tt := range []struct {
		name string
		set  func(*galleryModel)
	}{
		{"fill", func(m *galleryModel) { m.toggleFill() }},
		{"region", func(m *galleryModel) { m.crop = cropFrac{0.1, 0.2, 0.4, 0.6} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := newTransmitRecorder(t)
			m := newMinimapModel(t, rec)
			m.transmitView()
			before := rec.Len()
			tt.set(m)
			if m.crop.isFull() {
				t.Fatal("crop is still full")
			}
			m.transmitPreviewOnly()
			assertOverlay(t, m, onlyStore(t, added(rec, before), selectedSlotID(m)))
		})
	}
}

func TestMinimapRestoreViewWhileZoomed(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newMinimapModel(t, rec)
	m.transmitView()
	zoomIn(m)

	before := rec.Len()
	m.restoreView()
	out := added(rec, before)
	sel := selectedSlotID(m)
	assertOverlay(t, m, onlyStore(t, out, sel))
	for i := range m.l.stripCols {
		id := m.stripID(i)
		if id == sel || i >= len(m.images) {
			continue
		}
		assertKey(t, onlyStore(t, out, id), "f", 100)
	}

	before = rec.Len()
	m.syncMinimap()
	if n := len(added(rec, before)); n != 0 {
		t.Errorf("syncMinimap after restoreView wrote %d bytes, want 0", n)
	}
}

func TestMinimapSelectionWithRememberedCrop(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newMinimapModel(t, rec)
	m.crop = cropFrac{0.1, 0.2, 0.4, 0.6}

	m.transmitView()
	assertOverlay(t, m, onlyStore(t, added(rec, 0), selectedSlotID(m)))
}

func TestMinimapWhilePixelsPending(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newMinimapModel(t, rec)
	m.curImg = nil
	m.transmitView()

	before := rec.Len()
	m.zoomBy(zoomStep)
	m.transmitPreviewOnly()
	out := added(rec, before)
	assertOverlay(t, m, onlyStore(t, out, selectedSlotID(m)))
	if got := storesFor(t, out, m.previewID()); len(got) != 0 {
		t.Errorf("preview re-stored %d times while its pixels are pending", len(got))
	}
}

func TestMinimapBridgedUsesPNG(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newMinimapModel(t, rec)
	m.bridged = true
	m.transmitView()

	before := rec.Len()
	zoomIn(m)
	s := onlyStore(t, added(rec, before), selectedSlotID(m))
	assertKey(t, s, "f", 100)
	if s.path != m.minimapPNGPath() {
		t.Errorf("bridged overlay path = %s, want %s", s.path, m.minimapPNGPath())
	}
}

func TestMinimapBrokenBaseNotRetried(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newMinimapModel(t, rec)
	bad := filepath.Join(t.TempDir(), "bad.png")
	if err := os.WriteFile(bad, []byte("not a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(m.images) < 2 {
		t.Fatalf("TestMinimapBrokenBaseNotRetried: need at least 2 images, got %d", len(m.images))
	}
	m.images[1].Path, m.curImgPath = bad, bad
	m.transmitView()

	before := rec.Len()
	zoomIn(m)
	assertPlain(t, onlyStore(t, added(rec, before), selectedSlotID(m)), m)

	before = rec.Len()
	m.panBy(0.3, 0)
	m.transmitPreviewOnly()
	if got := storesFor(t, added(rec, before), selectedSlotID(m)); len(got) != 0 {
		t.Errorf("a broken base was retried: %d stores on the next pan", len(got))
	}
}

func TestMinimapClearedOnDecodeFailure(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newMinimapModel(t, rec)
	m.transmitView()

	before := rec.Len()
	m.crop = cropFrac{0.1, 0.2, 0.4, 0.6}
	m.transmitPreviewOnly()
	assertOverlay(t, m, onlyStore(t, added(rec, before), selectedSlotID(m)))

	before = rec.Len()
	next, _ := m.handle(decodedMsg{gen: m.decodeGen, path: m.curImgPath})
	got, ok := next.(galleryModel)
	if !ok {
		t.Fatalf("handle returned %T, want galleryModel", next)
	}
	if !got.crop.isFull() {
		t.Fatalf("decode failure left the crop at %+v", got.crop)
	}
	assertPlain(t, onlyStore(t, added(rec, before), selectedSlotID(&got)), &got)
}
