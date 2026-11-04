package main

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // mirrors the cache-file naming; not a security use
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/noamsto/themestate"
)

func TestAnimModeFor(t *testing.T) {
	for _, tc := range []struct {
		term, env string
		want      animMode
	}{
		{"xterm-kitty", "", animPartial},
		{"xterm-kitty", "full", animFull},
		{"xterm-kitty", "0", animOff},
		{"xterm-ghostty", "", animOff},
		{"tmux-256color", "", animOff},
	} {
		if got := animModeFor(tc.term, tc.env); got != tc.want {
			t.Errorf("animModeFor(%q, %q) = %d, want %d", tc.term, tc.env, got, tc.want)
		}
	}
}

const animFixtureSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 30">` +
	`<path class="connection animated-connection" d="M0 15 L40 15" ` +
	`style="stroke-width:2;stroke-dasharray:6,4;stroke-dashoffset:-100;"/></svg>`

// resetAnimCaches clears the package caches so each test starts cold.
func resetAnimCaches(t *testing.T) {
	t.Helper()
	animatesCache.Lock()
	animatesCache.m = map[string]animatesResult{}
	animatesCache.Unlock()
	animSetCache.Lock()
	animSetCache.m = map[string]animSetEntry{}
	animSetCache.Unlock()
}

// writeAnimPNG writes a 40×30 white PNG; block paints an 8×8 black square so the
// two fixtures differ in one small region.
func writeAnimPNG(t *testing.T, path string, block bool) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for y := range 30 {
		for x := range 40 {
			c := color.RGBA{255, 255, 255, 255}
			if block && x >= 8 && x < 16 && y >= 8 && y < 16 {
				c = color.RGBA{0, 0, 0, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
}

// animStub installs an AEYE_RESVG stand-in that copies fixture A for frame 0
// (the only frame whose dashoffset is zero) and fixture B otherwise, logging one
// line per call. Returns the animated SVG path and the log path.
func animStub(t *testing.T) (svg, log string) {
	t.Helper()
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")
	writeAnimPNG(t, a, false)
	writeAnimPNG(t, b, true)
	log = filepath.Join(dir, "calls.log")
	script := filepath.Join(dir, "resvg")
	body := fmt.Sprintf("#!/bin/sh\nfor a; do in=$out; out=$a; done\necho \"$*\" >> %q\n"+
		"if grep -q 'stroke-dashoffset:0.0000' \"$in\"; then cp %q \"$out\"; else cp %q \"$out\"; fi\n", log, a, b)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	t.Setenv("AEYE_RESVG", script)
	t.Setenv("AEYE_D2_FONT_DIR", "")
	t.Setenv("TMPDIR", t.TempDir())
	svg = filepath.Join(dir, "d.svg")
	if err := os.WriteFile(svg, []byte(animFixtureSVG), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	resetAnimCaches(t)
	return svg, log
}

func logLines(t *testing.T, log string) int {
	t.Helper()
	data, err := os.ReadFile(log) //nolint:gosec // test fixture path
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(data, []byte("\n"))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestRenderAnimFrames(t *testing.T) {
	svg, log := animStub(t)
	ctx := context.Background()
	const boxW, boxH = 40, 30
	n := animFrameCount(boxW, boxH)

	set := renderAnimFrames(ctx, svg, fullCrop(), boxW, boxH, animPartial)
	if set == nil {
		t.Fatal("renderAnimFrames = nil")
	}
	if !exists(set.Root) || !exists(set.Blob) {
		t.Fatalf("root %q / blob %q missing", set.Root, set.Blob)
	}
	for _, f := range []string{set.Root, set.Blob} {
		if !strings.HasPrefix(filepath.Base(f), "aeye-anim-") {
			t.Errorf("%s lacks the aeye-anim- prefix", f)
		}
	}
	if len(set.Plan.Frames) != n-1 {
		t.Fatalf("len(Plan.Frames) = %d, want %d", len(set.Plan.Frames), n-1)
	}
	if set.Plan.Frames[0].Full {
		t.Error("an 8×8 change should encode as a partial frame")
	}
	if got := logLines(t, log); got != n {
		t.Fatalf("resvg calls = %d, want %d", got, n)
	}
	calls, err := os.ReadFile(log) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("--width %d --height %d ", boxW, boxH); !strings.Contains(string(calls), want) {
		t.Errorf("resvg not fitted to the box (%q):\n%s", want, calls)
	}

	if again := renderAnimFrames(ctx, svg, fullCrop(), boxW, boxH, animPartial); again != set {
		t.Error("second call missed the cache")
	}
	if got := logLines(t, log); got != n {
		t.Errorf("cache hit ran resvg: calls = %d, want %d", got, n)
	}

	fi, err := os.Stat(svg)
	if err != nil {
		t.Fatal(err)
	}
	sha := sha1.Sum([]byte(fmt.Sprintf("%s|%d", svg, fi.ModTime().UnixNano()))) //nolint:gosec // mirrors the cache-file naming
	vec := filepath.Join(os.TempDir(), fmt.Sprintf("aeye-vec-%x-0.0000_0.0000_1.0000_1.0000-40.png", sha))
	if err := os.WriteFile(vec, nil, 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	half := cropFrac{0, 0, 0.5, 0.5}
	cropped := renderAnimFrames(ctx, svg, half, boxW, boxH, animPartial)
	if cropped == nil {
		t.Fatal("cropped render = nil")
	}
	if exists(set.Root) || exists(set.Blob) {
		t.Error("new framing did not evict the previous framing's files")
	}
	if !exists(vec) {
		t.Error("eviction removed an aeye-vec- file")
	}
	if !exists(cropped.Root) || !exists(cropped.Blob) {
		t.Error("cropped root/blob missing")
	}

	full := renderAnimFrames(ctx, svg, half, boxW, boxH, animFull)
	if full == nil {
		t.Fatal("full-mode render = nil")
	}
	for i, f := range full.Plan.Frames {
		if !f.Full {
			t.Errorf("animFull frame %d is partial", i)
		}
	}

	if got := renderAnimFrames(ctx, svg, fullCrop(), boxW, boxH, animOff); got != nil {
		t.Error("animOff rendered frames")
	}

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	other := cropFrac{0.25, 0.25, 0.75, 0.75}
	if got := renderAnimFrames(cctx, svg, other, boxW, boxH, animPartial); got != nil {
		t.Error("cancelled ctx returned a set")
	}
	key := animKey{id: 1, vector: svg, crop: other, boxW: boxW, boxH: boxH}
	if msg := renderAnimCmd(cctx, 1, key, animPartial)(); msg != nil {
		t.Errorf("cancelled renderAnimCmd msg = %#v, want nil", msg)
	}
	if msg, ok := renderAnimCmd(ctx, 7, key, animPartial)().(animReadyMsg); !ok || msg.gen != 7 || msg.key != key || msg.set == nil {
		t.Errorf("renderAnimCmd msg = %#v, want animReadyMsg for gen 7", msg)
	}

	if !vectorAnimates(svg) {
		t.Error("vectorAnimates = false for an animated edge")
	}
	static := filepath.Join(t.TempDir(), "s.svg")
	if err := os.WriteFile(static, []byte(`<svg viewBox="0 0 10 10"><path d="M0 0 L10 10"/></svg>`), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	before := logLines(t, log)
	if vectorAnimates(static) {
		t.Error("vectorAnimates = true for a static svg")
	}
	if vectorAnimates(filepath.Join(t.TempDir(), "missing.svg")) {
		t.Error("vectorAnimates = true for a missing file")
	}
	if got := logLines(t, log); got != before {
		t.Error("vectorAnimates invoked resvg")
	}
}

func TestAnimCachesConcurrent(t *testing.T) {
	svg, _ := animStub(t)
	var wg sync.WaitGroup
	sets := make([]*animSet, 8)
	animates := make([]bool, 8)
	for i := range 8 {
		wg.Go(func() {
			animates[i] = vectorAnimates(svg)
			sets[i] = renderAnimFrames(context.Background(), svg, fullCrop(), 40, 30, animPartial)
		})
	}
	wg.Wait()
	for i := range 8 {
		if !animates[i] || sets[i] == nil {
			t.Errorf("goroutine %d: animates=%v set=%v", i, animates[i], sets[i])
		}
	}
}

const staticFixtureSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><path d="M0 0 L10 10"/></svg>`

// newAnimModel is a visible kitty carousel over n real images, every manifest
// entry carrying vector (so it survives a reload), writing graphics to a file
// recorder, already transmitted once.
func newAnimModel(t *testing.T, n int, vector string, mode animMode) (*galleryModel, *transmitRecorder) {
	t.Helper()
	t.Setenv("TMUX", "")
	t.Setenv("AEYE_BRIDGED", "")
	dir := t.TempDir()
	t.Setenv("AEYE_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0o755); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	const pane = "7"
	var manifest string
	for range n {
		manifest += fmt.Sprintf("{\"type\":\"image\",\"path\":%q,\"vector\":%q}\n", writeTestPNG(t), vector)
	}
	if err := os.WriteFile(manifestPath(pane), []byte(manifest), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	rec := newTransmitRecorder(t)
	theme := themestate.Detect()
	m := &galleryModel{
		pane:     pane,
		backend:  backendKitty,
		tty:      rec.f,
		images:   loadManifest(pane, theme),
		theme:    theme,
		mtime:    manifestMtime(pane),
		ready:    true,
		visible:  true,
		width:    100,
		height:   40,
		crop:     fullCrop(),
		animMode: mode,
	}
	m.l = computeLayout(m.width, m.height)
	warmThumbs(m)
	if len(m.images) != n {
		t.Fatalf("fixture manifest loaded %d images, want %d", len(m.images), n)
	}
	m.transmitView()
	return m, rec
}

// written returns every byte the model wrote after offset from.
func written(rec *transmitRecorder, from int) []byte {
	rec.Len()
	return bytes.Clone(rec.buf.Bytes()[from:])
}

// handleMsg runs one message through the model's handler in place.
func handleMsg(m *galleryModel, msg tea.Msg) tea.Cmd {
	next, cmd := m.handle(msg)
	*m = next.(galleryModel)
	return cmd
}

// vectorReady renders the current framing through the stub rasterizer, as
// kickVector would.
func vectorReady(t *testing.T, m *galleryModel) vectorReadyMsg {
	t.Helper()
	msg, ok := renderVectorCmd(m.curVector(), m.crop, m.l.previewW*m.cellWpx())().(vectorReadyMsg)
	if !ok {
		t.Fatal("renderVectorCmd produced no vectorReadyMsg")
	}
	return msg
}

// staticStore is what the vectorReadyMsg handler writes for a static frame.
func staticStore(m *galleryModel) string {
	return transmitVirtual(m.previewID(), m.zoomScratchPath(), m.l.previewW, m.l.previewH)
}

func currentAnimKey(m *galleryModel) animKey {
	return animKey{id: m.previewID(), vector: m.curVector(), crop: m.crop,
		boxW: m.l.previewW * m.cellWpx(), boxH: m.l.previewH * m.cellHpx()}
}

// startLoop drives a vectorReadyMsg and the animation it kicks into place.
func startLoop(t *testing.T, m *galleryModel) {
	t.Helper()
	cmd := handleMsg(m, vectorReady(t, m))
	if cmd == nil {
		t.Fatal("animated vectorReadyMsg kicked no animation render")
	}
	amsg, ok := cmd().(animReadyMsg)
	if !ok {
		t.Fatal("animation render produced no animReadyMsg")
	}
	handleMsg(m, amsg)
	if m.anim.attached != amsg.key {
		t.Fatalf("attached = %+v, want %+v", m.anim.attached, amsg.key)
	}
}

func TestVectorReadyStaticUnchanged(t *testing.T) {
	for _, mode := range []animMode{animOff, animPartial} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			animStub(t)
			svg := filepath.Join(t.TempDir(), "s.svg")
			if err := os.WriteFile(svg, []byte(staticFixtureSVG), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
				t.Fatal(err)
			}
			m, rec := newAnimModel(t, 1, svg, mode)
			msg := vectorReady(t, m)
			if msg.animates {
				t.Fatal("static svg reported as animating")
			}

			from := rec.Len()
			if cmd := handleMsg(m, msg); cmd != nil {
				t.Error("static vectorReadyMsg returned a cmd")
			}
			if got, want := string(written(rec, from)), staticStore(m); got != want {
				t.Errorf("static store bytes changed:\n got %q\nwant %q", got, want)
			}
			if m.anim.attached != (animKey{}) {
				t.Errorf("static frame left attached = %+v", m.anim.attached)
			}
		})
	}
}

func TestVectorReadyKicksAnim(t *testing.T) {
	svg, _ := animStub(t)
	m, rec := newAnimModel(t, 1, svg, animPartial)
	msg := vectorReady(t, m)
	if !msg.animates {
		t.Fatal("renderVectorCmd did not flag the animated svg")
	}

	from := rec.Len()
	cmd := handleMsg(m, msg)
	if got, want := string(written(rec, from)), staticStore(m); got != want {
		t.Errorf("animated vectorReadyMsg static store:\n got %q\nwant %q", got, want)
	}
	if cmd == nil {
		t.Fatal("animated vectorReadyMsg returned no cmd")
	}
	amsg, ok := cmd().(animReadyMsg)
	if !ok || amsg.set == nil {
		t.Fatalf("anim cmd msg = %#v, want animReadyMsg with a set", amsg)
	}
	if want := currentAnimKey(m); amsg.key != want {
		t.Errorf("animReadyMsg key = %+v, want %+v", amsg.key, want)
	}

	from = rec.Len()
	if cmd := handleMsg(m, amsg); cmd != nil {
		t.Error("animReadyMsg returned a cmd")
	}
	if got, want := string(written(rec, from)), strings.Join(animationStore(m.previewID(), amsg.set, m.l.previewW, m.l.previewH), ""); got != want {
		t.Errorf("animReadyMsg bytes:\n got %q\nwant %q", got, want)
	}
	if m.anim.attached != amsg.key {
		t.Errorf("attached = %+v, want %+v", m.anim.attached, amsg.key)
	}

	from = rec.Len()
	if cmd := handleMsg(m, msg); cmd != nil {
		t.Error("repeat vectorReadyMsg for the looping framing returned a cmd")
	}
	if got := written(rec, from); len(got) != 0 {
		t.Errorf("repeat vectorReadyMsg re-stored over the animation: %q", got)
	}
}

func TestAnimHiddenSchedulesNothing(t *testing.T) {
	svg, _ := animStub(t)
	m, rec := newAnimModel(t, 1, svg, animPartial)
	m.visible = false
	stubPaneVisible(t, false)

	if cmd := handleMsg(m, vectorReady(t, m)); cmd != nil {
		t.Error("hidden vectorReadyMsg scheduled an animation render")
	}

	boxW, boxH := m.l.previewW*m.cellWpx(), m.l.previewH*m.cellHpx()
	set := renderAnimFrames(context.Background(), svg, m.crop, boxW, boxH, animPartial)
	if set == nil {
		t.Fatal("fixture frame render failed")
	}
	from := rec.Len()
	handleMsg(m, animReadyMsg{gen: m.anim.gen, key: currentAnimKey(m), set: set})
	if got := written(rec, from); len(got) != 0 {
		t.Errorf("hidden animReadyMsg wrote %q", got)
	}
	if m.anim.attached != (animKey{}) {
		t.Errorf("hidden animReadyMsg attached %+v", m.anim.attached)
	}

	gen := m.anim.gen
	from = rec.Len()
	cmd := handleMsg(m, galleryTickMsg{})
	// Running the poller would block until something changes, so name the
	// returned cmd instead: a render batched in makes it a tea.Batch closure.
	// Inlining gives each call site its own copy, hence the substring match.
	if cmd == nil {
		t.Fatal("hidden tick dropped the manifest poller")
	}
	if name := runtime.FuncForPC(reflect.ValueOf(cmd).Pointer()).Name(); !strings.Contains(name, ".galleryTickCmd.func") {
		t.Errorf("hidden tick returned %s, want the manifest poller alone", name)
	}
	if m.visible {
		t.Fatal("tick flipped visibility")
	}
	if m.anim.gen != gen || m.anim.cancel != nil {
		t.Error("hidden tick started animation work")
	}
	if got := written(rec, from); len(got) != 0 {
		t.Errorf("hidden tick wrote %q", got)
	}
}

func TestAnimStaleDropped(t *testing.T) {
	svg, _ := animStub(t)
	m, rec := newAnimModel(t, 1, svg, animPartial)
	cmd := handleMsg(m, vectorReady(t, m))
	if cmd == nil {
		t.Fatal("animated vectorReadyMsg returned no cmd")
	}
	amsg, ok := cmd().(animReadyMsg)
	if !ok {
		t.Fatal("animation render produced no animReadyMsg")
	}

	oldGen := amsg
	oldGen.gen--
	otherCrop := amsg
	otherCrop.key.crop = cropFrac{0, 0, 0.5, 0.5}
	noSet := amsg
	noSet.set = nil
	for _, tc := range []struct {
		name string
		msg  animReadyMsg
	}{{"old gen", oldGen}, {"other crop", otherCrop}, {"nil set", noSet}} {
		from := rec.Len()
		handleMsg(m, tc.msg)
		if got := written(rec, from); len(got) != 0 {
			t.Errorf("%s: wrote %q", tc.name, got)
		}
		if m.anim.attached != (animKey{}) {
			t.Errorf("%s: attached %+v", tc.name, m.anim.attached)
		}
	}
}

func TestAnimFallbackStatic(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    animMode
		bridged bool
	}{
		{"ghostty", animModeFor("xterm-ghostty", ""), false},
		{"AEYE_ANIMATE=0", animModeFor("xterm-kitty", "0"), false},
		{"bridged", animPartial, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svg, _ := animStub(t)
			m, rec := newAnimModel(t, 1, svg, tc.mode)
			m.bridged = tc.bridged

			from := rec.Len()
			if cmd := handleMsg(m, vectorReady(t, m)); cmd != nil {
				t.Error("ineligible viewer scheduled an animation render")
			}
			if got, want := string(written(rec, from)), staticStore(m); got != want {
				t.Errorf("static store bytes:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestPreviewStoreClearsAttached(t *testing.T) {
	svg, _ := animStub(t)
	m, _ := newAnimModel(t, 1, svg, animPartial)

	startLoop(t, m)
	m.transmitPreviewOnly()
	if m.anim.attached != (animKey{}) {
		t.Errorf("transmitPreviewOnly left attached = %+v", m.anim.attached)
	}

	startLoop(t, m)
	m.dropStore()
	if m.anim.attached != (animKey{}) {
		t.Errorf("dropStore left attached = %+v", m.anim.attached)
	}
}

// TestSelectionStopsOldAnimation: the selection change stores the new preview
// under the idle id and leaves the looping id for the next transmit to reap, so
// its loop must be stopped first.
func TestSelectionStopsOldAnimation(t *testing.T) {
	svg, _ := animStub(t)
	m, rec := newAnimModel(t, 2, svg, animPartial)
	startLoop(t, m)
	oldID := m.previewID()

	from := rec.Len()
	m.selectIndex(m.cursor + 1)
	out := string(written(rec, from))
	newID := m.previewID()
	if newID == oldID {
		t.Fatalf("selection change kept preview id %d; expected a double-buffer flip", oldID)
	}
	stop := strings.Index(out, animStop(oldID))
	store := strings.Index(out, fmt.Sprintf("\x1b_Gi=%d,a=T", newID))
	if stop < 0 || store < 0 || stop > store {
		t.Errorf("want animStop(%d) before the store of %d; stop at %d, store at %d in %q", oldID, newID, stop, store, out)
	}
	if m.anim.attached != (animKey{}) {
		t.Errorf("selection change left attached = %+v", m.anim.attached)
	}
}

// TestAnimRootStraightAlpha: kitty's f=32 is straight RGBA, so a translucent
// pixel must reach the root file un-premultiplied.
func TestAnimRootStraightAlpha(t *testing.T) {
	svg, _ := animStub(t)
	dir := filepath.Dir(svg)
	for _, name := range []string{"a.png", "b.png"} {
		img := image.NewNRGBA(image.Rect(0, 0, 40, 30))
		for i := 0; i < len(img.Pix); i += 4 {
			copy(img.Pix[i:i+4], []byte{200, 100, 50, 128})
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
			t.Fatal(err)
		}
	}
	set := renderAnimFrames(context.Background(), svg, fullCrop(), 40, 30, animPartial)
	if set == nil {
		t.Fatal("renderAnimFrames = nil")
	}
	root, err := os.ReadFile(set.Root)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []byte{200, 100, 50, 128} {
		if d := int(root[i]) - int(want); d < -2 || d > 2 {
			t.Fatalf("root pixel = %v, want straight %v", root[:4], []byte{200, 100, 50, 128})
		}
	}
}

// TestAnimRepublishEvicts: a republished diagram (same path, new mtime) must
// evict the older render's files, not leave them behind under another stem.
func TestAnimRepublishEvicts(t *testing.T) {
	svg, _ := animStub(t)
	ctx := context.Background()
	old := renderAnimFrames(ctx, svg, fullCrop(), 40, 30, animPartial)
	if old == nil {
		t.Fatal("renderAnimFrames = nil")
	}
	fi, err := os.Stat(svg)
	if err != nil {
		t.Fatal(err)
	}
	later := fi.ModTime().Add(time.Second)
	if err := os.Chtimes(svg, later, later); err != nil {
		t.Fatal(err)
	}
	cur := renderAnimFrames(ctx, svg, fullCrop(), 40, 30, animPartial)
	if cur == nil {
		t.Fatal("republished render = nil")
	}
	if exists(old.Root) || exists(old.Blob) {
		t.Error("republish left the older mtime's root/blob on disk")
	}
	if !exists(cur.Root) || !exists(cur.Blob) {
		t.Error("republished root/blob missing")
	}
}

// TestAnimUncroppableZoomStaysStatic: zoomed into an SVG with no viewBox to
// crop, the frames would show the whole diagram in the crop's slot.
func TestAnimUncroppableZoomStaysStatic(t *testing.T) {
	svg, _ := animStub(t)
	noVB := strings.Replace(animFixtureSVG, `viewBox="0 0 40 30"`, `width="40" height="30"`, 1)
	if err := os.WriteFile(svg, []byte(noVB), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	ctx := context.Background()
	if got := renderAnimFrames(ctx, svg, cropFrac{0, 0, 0.5, 0.5}, 40, 30, animPartial); got != nil {
		t.Error("zoomed render of an un-croppable svg animated the whole diagram")
	}
	if got := renderAnimFrames(ctx, svg, fullCrop(), 40, 30, animPartial); got == nil {
		t.Error("full-crop render of an svg without a viewBox = nil")
	}
}

// TestAnimCropRealResvg: the crop window reaches resvg, and the frames land at
// the size the static sharp frame is fitted to.
func TestAnimCropRealResvg(t *testing.T) {
	if _, err := exec.LookPath(resvgBin()); err != nil {
		t.Skip("resvg not on PATH")
	}
	t.Setenv("AEYE_D2_FONT_DIR", "")
	t.Setenv("TMPDIR", t.TempDir())
	resetAnimCaches(t)
	svg := filepath.Join(t.TempDir(), "d.svg")
	stroked := strings.Replace(animFixtureSVG, "style=", `stroke="#000" style=`, 1)
	if err := os.WriteFile(svg, []byte(stroked), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	const boxW, boxH = 300, 150
	ctx := context.Background()
	root := func(crop cropFrac) (image.Point, []byte) {
		t.Helper()
		set := renderAnimFrames(ctx, svg, crop, boxW, boxH, animPartial)
		if set == nil {
			t.Fatalf("renderAnimFrames(%v) = nil", crop)
		}
		pix, err := os.ReadFile(set.Root)
		if err != nil {
			t.Fatal(err)
		}
		return image.Pt(set.Plan.W, set.Plan.H), pix
	}
	crop := cropFrac{0, 0, 0.6, 0.6}
	size, cropped := root(crop)

	f, err := os.Open(renderVector(svg, crop, boxW))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck // read-only test file
	ref, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if want := fitToBox(ref, boxW, boxH).Bounds().Size(); size != want {
		t.Errorf("cropped frame 0 is %v, static sharp frame is %v", size, want)
	}
	if _, full := root(fullCrop()); bytes.Equal(cropped, full) {
		t.Error("cropped frame 0 equals the uncropped one: crop not applied")
	}
}

// TestStaticSelectionNoAnimStop: moving between static entries never touches
// kitty's animation state.
func TestStaticSelectionNoAnimStop(t *testing.T) {
	animStub(t)
	svg := filepath.Join(t.TempDir(), "s.svg")
	if err := os.WriteFile(svg, []byte(staticFixtureSVG), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	m, rec := newAnimModel(t, 2, svg, animPartial)
	handleMsg(m, vectorReady(t, m))
	from := rec.Len()
	m.selectIndex(m.cursor + 1)
	out := written(rec, from)
	if len(out) == 0 {
		t.Fatal("selection change stored nothing")
	}
	if bytes.Contains(out, []byte("a=a,")) {
		t.Errorf("static selection change wrote animation control: %q", out)
	}
}

// kickPending drives a vectorReadyMsg for the current framing and returns the
// animation render it kicks, unrun.
func kickPending(t *testing.T, m *galleryModel) tea.Cmd {
	t.Helper()
	cmd := handleMsg(m, vectorReady(t, m))
	if cmd == nil {
		t.Fatal("animated vectorReadyMsg kicked no animation render")
	}
	return cmd
}

// TestAnimUnwantedRenderCancelled: once the preview moves on, the pending render
// is cancelled rather than left to finish frames nobody will attach.
func TestAnimUnwantedRenderCancelled(t *testing.T) {
	for _, tc := range []struct {
		name string
		move func(t *testing.T, m *galleryModel)
	}{
		{"selection change", func(_ *testing.T, m *galleryModel) { m.selectIndex(m.cursor + 1) }},
		{"preview re-store", func(_ *testing.T, m *galleryModel) { m.transmitPreviewOnly() }},
		{"pane hidden", func(t *testing.T, m *galleryModel) {
			stubPaneVisible(t, false)
			handleMsg(m, galleryTickMsg{})
			if m.visible {
				t.Fatal("tick did not hide the pane")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svg, _ := animStub(t)
			m, _ := newAnimModel(t, 2, svg, animPartial)
			cmd := kickPending(t, m)
			tc.move(t, m)
			if msg := cmd(); msg != nil {
				t.Errorf("render still ran after the preview moved on: %#v", msg)
			}
		})
	}
}

// TestAnimSameFramingNotRestarted: a repeat sharp render of the framing whose
// animation is already rendering keeps that render rather than restarting it.
func TestAnimSameFramingNotRestarted(t *testing.T) {
	svg, _ := animStub(t)
	m, _ := newAnimModel(t, 1, svg, animPartial)
	cmd := kickPending(t, m)
	gen := m.anim.gen

	m.kickVector()
	if again := handleMsg(m, vectorReady(t, m)); again != nil {
		t.Error("repeat vectorReadyMsg restarted the render")
	}
	if m.anim.gen != gen {
		t.Errorf("gen = %d, want %d", m.anim.gen, gen)
	}
	msg, ok := cmd().(animReadyMsg)
	if !ok {
		t.Fatal("the pending render was cancelled")
	}
	handleMsg(m, msg)
	if m.anim.attached != msg.key {
		t.Errorf("attached = %+v, want %+v", m.anim.attached, msg.key)
	}
}

// yieldsVectorReady runs cmd's leaves concurrently and reports whether one
// yields a vectorReadyMsg within timeout; a long tick in the batch (the undo
// window) can't hold it up.
func yieldsVectorReady(cmd tea.Cmd, timeout time.Duration) bool {
	found := make(chan struct{}, 1)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			switch msg := c().(type) {
			case tea.BatchMsg:
				for _, sub := range msg {
					run(sub)
				}
			case vectorReadyMsg:
				select {
				case found <- struct{}{}:
				default:
				}
			}
		}()
	}
	run(cmd)
	select {
	case <-found:
		return true
	case <-time.After(timeout):
		return false
	}
}

// TestDeleteCommitKeepsSharpD2: committing a deletion reloads, and the reload's
// re-store can cancel the selection's in-flight animation render, so both
// routes that commit must re-kick the sharp vector render.
func TestDeleteCommitKeepsSharpD2(t *testing.T) {
	t.Run("deleteCommitMsg", func(t *testing.T) {
		svg, _ := animStub(t)
		m, _ := newAnimModel(t, 2, svg, animPartial)
		m.markPending()
		cmd := handleMsg(m, deleteCommitMsg{gen: m.delGen})
		if m.pending != nil || len(m.images) != 1 {
			t.Fatalf("deletion not committed: pending=%v images=%d", m.pending, len(m.images))
		}
		if !yieldsVectorReady(cmd, 3*time.Second) {
			t.Fatal("committing the deletion issued no vector render; the preview stays a bitmap")
		}
	})
	t.Run("x commits the earlier pending deletion", func(t *testing.T) {
		svg, _ := animStub(t)
		m, _ := newAnimModel(t, 3, svg, animPartial)
		m.markPending()
		m.selectIndex(1)
		cmd := handleMsg(m, tea.KeyPressMsg{Text: "x", Code: 'x'})
		if len(m.images) != 2 || m.pending == nil {
			t.Fatalf("x did not commit the earlier deletion and mark the next: images=%d pending=%v", len(m.images), m.pending)
		}
		if !yieldsVectorReady(cmd, 3*time.Second) {
			t.Fatal("x committing an earlier deletion issued no vector render; the preview stays a bitmap")
		}
	})
}
