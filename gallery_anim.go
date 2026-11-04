package main

import (
	"context"
	"crypto/sha1" //nolint:gosec // sha1 only names cache files; not a security use
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
)

type animMode int

const (
	animOff animMode = iota
	animPartial
	animFull
)

// animModeFor picks the playback mode from TERM and AEYE_ANIMATE. Only kitty
// plays kitty animation frames: Ghostty speaks the graphics protocol but drops
// frames, so it stays static.
func animModeFor(termname, env string) animMode {
	if !strings.HasPrefix(termname, "xterm-kitty") {
		return animOff
	}
	switch env {
	case "0":
		return animOff
	case "full":
		return animFull
	}
	return animPartial
}

// animatesCache memoizes vectorAnimates by (path, mtime, size) so the reload
// poll doesn't re-scan an unchanged SVG.
var animatesCache = struct {
	sync.Mutex
	m map[string]animatesResult
}{m: map[string]animatesResult{}}

type animatesResult struct {
	mtime, size int64
	animates    bool
}

// vectorAnimates reports whether the SVG at path has anything to animate; false
// when it can't be read.
func vectorAnimates(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	mtime, size := fi.ModTime().UnixNano(), fi.Size()
	animatesCache.Lock()
	if r, ok := animatesCache.m[path]; ok && r.mtime == mtime && r.size == size {
		animatesCache.Unlock()
		return r.animates
	}
	animatesCache.Unlock()
	data, err := os.ReadFile(path) //nolint:gosec // path is the user's own image/manifest file
	if err != nil {
		return false
	}
	animates := svgAnimates(data)
	animatesCache.Lock()
	animatesCache.m[path] = animatesResult{mtime, size, animates}
	animatesCache.Unlock()
	return animates
}

// animKey identifies the framing an animation was rendered for, so Update can
// drop a set that arrives after the selection, crop or box moved on.
type animKey struct {
	id     int
	vector string
	crop   cropFrac
	boxW   int
	boxH   int
}

// animReadyMsg carries a rendered frame set back to Update.
type animReadyMsg struct {
	gen uint64
	key animKey
	set *animSet
}

// animState tracks the preview's animation: gen supersedes earlier renders,
// cancel aborts the running one and inflight is the framing it renders, and
// attached is the framing kitty is looping (zero when the preview holds a
// static image).
type animState struct {
	gen      uint64
	cancel   context.CancelFunc
	inflight animKey
	attached animKey
}

type animSetKey struct {
	mtime      int64
	crop       cropFrac
	boxW, boxH int
	n          int
	mode       animMode
}

type animSetEntry struct {
	key animSetKey
	set *animSet
}

// animSetCache holds one rendered framing per vector path; a new framing
// replaces it, matching the one-framing-per-diagram eviction on disk.
var animSetCache = struct {
	sync.Mutex
	m map[string]animSetEntry
}{m: map[string]animSetEntry{}}

// animRenderMu serializes frame renders: renders of one diagram share file
// names and evict each other's, and concurrent requests for the same framing
// then render it once.
var animRenderMu sync.Mutex

func cachedAnimSet(vector string, key animSetKey) *animSet {
	animSetCache.Lock()
	e, ok := animSetCache.m[vector]
	animSetCache.Unlock()
	if !ok || e.key != key {
		return nil
	}
	if _, err := os.Stat(e.set.Root); err != nil {
		return nil
	}
	if _, err := os.Stat(e.set.Blob); err != nil {
		return nil
	}
	return e.set
}

// renderAnimFrames rasterizes every loop frame of the vector's crop window,
// fits each to the box, and encodes them into a root + rect blob pair under
// os.TempDir(). Returns nil when the box is too large to animate, on any
// failure, or when ctx is cancelled.
func renderAnimFrames(ctx context.Context, vector string, crop cropFrac, boxW, boxH int, mode animMode) *animSet {
	if mode == animOff || boxW <= 0 || boxH <= 0 {
		return nil
	}
	n := animFrameCount(boxW, boxH)
	if n == 0 {
		tracef("anim render %s: %dx%d box is over the frame budget", vector, boxW, boxH)
		return nil
	}
	fi, err := os.Stat(vector)
	if err != nil {
		tracef("anim render %s: %v", vector, err)
		return nil
	}
	key := animSetKey{fi.ModTime().UnixNano(), crop, boxW, boxH, n, mode}
	if s := cachedAnimSet(vector, key); s != nil {
		return s
	}
	animRenderMu.Lock()
	defer animRenderMu.Unlock()
	if s := cachedAnimSet(vector, key); s != nil {
		return s
	}
	if ctx.Err() != nil {
		tracef("anim render %s: cancelled", vector)
		return nil
	}
	data, err := os.ReadFile(vector) //nolint:gosec // path is the user's own image/manifest file
	if err != nil {
		tracef("anim render %s: %v", vector, err)
		return nil
	}
	// Own prefix so eviction never touches renderVector's aeye-vec- scratch. The
	// mtime stays out of the stem so a republished diagram evicts its older sets.
	stem := fmt.Sprintf("aeye-anim-%x", sha1.Sum([]byte(vector))) //nolint:gosec // sha1 only names cache files; not a security use
	olds, _ := filepath.Glob(filepath.Join(os.TempDir(), stem+"-*"))
	for _, o := range olds {
		os.Remove(o) //nolint:errcheck,gosec // best-effort temp cleanup
	}
	frames := rasterizeAnimFrames(ctx, data, crop, boxW, boxH, n)
	if frames == nil {
		if ctx.Err() != nil {
			tracef("anim render %s: cancelled", vector)
		} else {
			tracef("anim render %s: rasterizing a frame failed", vector)
		}
		return nil
	}
	// fitToBox rounds a sliver-shaped crop down to zero rows; nothing to encode.
	if frames[0].Bounds().Empty() {
		tracef("anim render %s: crop fits the box at zero size", vector)
		return nil
	}
	blob, plan := encodeAnim(frames, mode == animFull)
	base := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d-%.4f_%.4f_%.4f_%.4f-%dx%d-%d-%d",
		stem, key.mtime, crop.x0, crop.y0, crop.x1, crop.y1, boxW, boxH, n, mode))
	root := writePix(base+"-root.rgba", frames[0].Pix)
	if root == "" {
		tracef("anim render %s: writing the root frame failed", vector)
		return nil
	}
	blobPath := writePix(base+"-blob.rgba", blob)
	if blobPath == "" {
		tracef("anim render %s: writing the frame blob failed", vector)
		return nil
	}
	set := &animSet{Root: root, Blob: blobPath, Plan: plan}
	animSetCache.Lock()
	animSetCache.m[vector] = animSetEntry{key, set}
	animSetCache.Unlock()
	return set
}

// rasterizeAnimFrames runs resvg for all n frames, at most min(4, NumCPU) at
// once, and returns them fitted to the box; nil if any frame fails.
func rasterizeAnimFrames(ctx context.Context, svg []byte, crop cropFrac, boxW, boxH, n int) []*image.NRGBA {
	work, err := os.MkdirTemp("", "aeye-animwork-")
	if err != nil {
		return nil
	}
	defer os.Remove(work) //nolint:errcheck // best-effort temp cleanup
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	frames := make([]*image.NRGBA, n)
	sem := make(chan struct{}, min(4, runtime.NumCPU()))
	var wg sync.WaitGroup
	for k := range n {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			frames[k] = rasterizeAnimFrame(ctx, work, svg, k, n, crop, boxW, boxH)
			if frames[k] == nil {
				cancel()
			}
		})
	}
	wg.Wait()
	for _, f := range frames {
		if f == nil {
			return nil
		}
	}
	return frames
}

func rasterizeAnimFrame(ctx context.Context, work string, svg []byte, k, n int, crop cropFrac, boxW, boxH int) *image.NRGBA {
	frame := animFrameSVG(svg, k, n)
	if !crop.isFull() {
		var ok bool
		// With no viewBox to crop, the frames would play the whole diagram in
		// the crop window's place.
		if frame, ok = cropViewBox(frame, crop); !ok {
			if ctx.Err() == nil {
				tracef("anim frame %d: no viewBox to crop", k)
			}
			return nil
		}
	}
	in := filepath.Join(work, strconv.Itoa(k)+".svg")
	out := filepath.Join(work, strconv.Itoa(k)+".png")
	defer os.Remove(in)  //nolint:errcheck // best-effort temp cleanup
	defer os.Remove(out) //nolint:errcheck // best-effort temp cleanup

	if err := os.WriteFile(in, frame, 0o600); err != nil { //nolint:gosec // scratch SVG in our own temp dir for the local rasterizer
		return nil
	}
	// resvg fits within both bounds keeping aspect, so a tall diagram is never
	// rasterized at full box width only to be shrunk.
	args := append(resvgFontArgs(), "--width", strconv.Itoa(boxW), "--height", strconv.Itoa(boxH), in, out)
	if err := exec.CommandContext(ctx, resvgBin(), args...).Run(); err != nil { //nolint:gosec // args built by the tool, not shell-interpreted
		if ctx.Err() == nil {
			tracef("anim frame %d: resvg: %v", k, err)
		}
		return nil
	}
	f, err := os.Open(out) //nolint:gosec // scratch PNG this function just rendered
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck // read-only file; close error is not actionable
	img, err := png.Decode(f)
	if err != nil {
		return nil
	}
	return straightAlpha(fitToBox(img, boxW, boxH).(*image.RGBA))
}

// straightAlpha un-premultiplies img, which must be tightly packed: kitty
// reads f=32 pixels as straight RGBA.
func straightAlpha(img *image.RGBA) *image.NRGBA {
	dst := image.NewNRGBA(img.Bounds())
	for i := 0; i < len(img.Pix); i += 4 {
		switch a := img.Pix[i+3]; a {
		case 0:
		case 0xff:
			copy(dst.Pix[i:i+4], img.Pix[i:i+4])
		default:
			for c := range 3 {
				// Premultiplied c <= a keeps v <= 0xff; min only proves it to gosec.
				v := (uint32(img.Pix[i+c])*0xff + uint32(a)/2) / uint32(a)
				dst.Pix[i+c] = uint8(min(v, 0xff))
			}
			dst.Pix[i+3] = a
		}
	}
	return dst
}

// renderAnimCmd renders the frames for key off the event loop; a nil set
// yields a nil msg, leaving the static preview in place.
func renderAnimCmd(ctx context.Context, gen uint64, key animKey, mode animMode) tea.Cmd {
	return func() tea.Msg {
		set := renderAnimFrames(ctx, key.vector, key.crop, key.boxW, key.boxH, mode)
		if set == nil {
			return nil
		}
		return animReadyMsg{gen: gen, key: key, set: set}
	}
}

// previewAnimKey is the framing the preview shows for vector at crop.
func (m *galleryModel) previewAnimKey(vector string, crop cropFrac) animKey {
	return animKey{id: m.previewID(), vector: vector, crop: crop,
		boxW: m.l.previewW * m.cellWpx(), boxH: m.l.previewH * m.cellHpx()}
}

// kickAnim supersedes any running frame render with one for key, or returns nil
// when the preview stays static: nothing animates, the terminal can't play
// frames, a relay can't read the t=f frame files, or the pane is hidden. It
// also returns nil while key is already rendering; a render that failed stays
// in flight, so the same framing isn't retried until something cancels it.
func (m *galleryModel) kickAnim(key animKey, animates bool) tea.Cmd {
	if !animates || m.animMode == animOff || m.bridged || !m.visible || key == m.anim.inflight {
		return nil
	}
	m.cancelAnim()
	m.anim.gen++
	ctx, cancel := context.WithCancel(context.Background())
	m.anim.cancel = cancel
	m.anim.inflight = key
	return renderAnimCmd(ctx, m.anim.gen, key, m.animMode)
}

func (m *galleryModel) cancelAnim() {
	if m.anim.cancel != nil {
		m.anim.cancel()
		m.anim.cancel = nil
	}
	m.anim.inflight = animKey{}
}

// attachAnim plays msg's frames on the preview if they still match what it
// shows; the static image stays up otherwise.
func (m *galleryModel) attachAnim(msg animReadyMsg) {
	if msg.gen != m.anim.gen {
		return
	}
	m.cancelAnim()
	if msg.set == nil || !m.visible || m.bridged || m.backend != backendKitty || m.tty == nil ||
		msg.key != m.previewAnimKey(m.curVector(), m.crop) {
		return
	}
	// One write per escape: a large write can go out in pieces, and bubbletea's
	// frame output landing between two pieces would split an escape.
	for _, apc := range animationStore(m.previewID(), msg.set, m.l.previewW, m.l.previewH) {
		fmt.Fprint(m.tty, apc) //nolint:errcheck // best-effort tty escape-sequence write
	}
	m.anim.attached = msg.key
}

// stopAnim forgets the looping framing ahead of a preview re-store. The store
// may land under the idle id, leaving the looping one up until the next
// transmit reaps it, so its loop is stopped first.
func (m *galleryModel) stopAnim() {
	if m.anim.attached != (animKey{}) {
		fmt.Fprint(m.tty, animStop(m.anim.attached.id)) //nolint:errcheck // best-effort tty escape-sequence write
	}
	m.anim.attached = animKey{}
}
