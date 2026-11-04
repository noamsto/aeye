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
func renderAnimFrames(ctx context.Context, vector string, crop cropFrac, targetW, boxW, boxH int, mode animMode) *animSet {
	if mode == animOff || boxW <= 0 || boxH <= 0 {
		return nil
	}
	n := animFrameCount(boxW, boxH)
	if n == 0 {
		return nil
	}
	fi, err := os.Stat(vector)
	if err != nil {
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
		return nil
	}
	data, err := os.ReadFile(vector) //nolint:gosec // path is the user's own image/manifest file
	if err != nil {
		return nil
	}
	// Own prefix so eviction never touches renderVector's aeye-vec- scratch.
	stem := fmt.Sprintf("aeye-anim-%x", sha1.Sum([]byte(fmt.Sprintf("%s|%d", vector, key.mtime)))) //nolint:gosec // sha1 only names cache files; not a security use
	olds, _ := filepath.Glob(filepath.Join(os.TempDir(), stem+"-*"))
	for _, o := range olds {
		os.Remove(o) //nolint:errcheck,gosec // best-effort temp cleanup
	}
	frames := rasterizeAnimFrames(ctx, data, crop, targetW, boxW, boxH, n)
	// fitToBox rounds a sliver-shaped crop down to zero rows; nothing to encode.
	if frames == nil || frames[0].Bounds().Empty() {
		return nil
	}
	blob, plan := encodeAnim(frames, mode == animFull)
	base := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%.4f_%.4f_%.4f_%.4f-%dx%d-%d-%d",
		stem, crop.x0, crop.y0, crop.x1, crop.y1, boxW, boxH, n, mode))
	root := writeRaw(base+"-root.rgba", frames[0])
	if root == "" {
		return nil
	}
	blobPath := base + "-blob.rgba"
	tmp := blobPath + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		os.Remove(tmp) //nolint:errcheck,gosec // best-effort temp cleanup
		return nil
	}
	if err := os.Rename(tmp, blobPath); err != nil {
		os.Remove(tmp) //nolint:errcheck,gosec // best-effort temp cleanup
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
func rasterizeAnimFrames(ctx context.Context, svg []byte, crop cropFrac, targetW, boxW, boxH, n int) []*image.RGBA {
	work, err := os.MkdirTemp("", "aeye-animwork-")
	if err != nil {
		return nil
	}
	defer os.Remove(work) //nolint:errcheck // best-effort temp cleanup
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	frames := make([]*image.RGBA, n)
	sem := make(chan struct{}, min(4, runtime.NumCPU()))
	var wg sync.WaitGroup
	for k := range n {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			frames[k] = rasterizeAnimFrame(ctx, work, svg, k, n, crop, targetW, boxW, boxH)
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

func rasterizeAnimFrame(ctx context.Context, work string, svg []byte, k, n int, crop cropFrac, targetW, boxW, boxH int) *image.RGBA {
	frame := animFrameSVG(svg, k, n)
	if !crop.isFull() {
		frame, _ = cropViewBox(frame, crop)
	}
	in := filepath.Join(work, strconv.Itoa(k)+".svg")
	out := filepath.Join(work, strconv.Itoa(k)+".png")
	defer os.Remove(in)  //nolint:errcheck // best-effort temp cleanup
	defer os.Remove(out) //nolint:errcheck // best-effort temp cleanup

	if err := os.WriteFile(in, frame, 0o600); err != nil { //nolint:gosec // scratch SVG in our own temp dir for the local rasterizer
		return nil
	}
	args := append(resvgFontArgs(), "--width", strconv.Itoa(targetW), in, out)
	if err := exec.CommandContext(ctx, resvgBin(), args...).Run(); err != nil { //nolint:gosec // args built by the tool, not shell-interpreted
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
	return fitToBox(img, boxW, boxH).(*image.RGBA)
}

// renderAnimCmd renders the frames for key off the event loop; a nil set
// yields a nil msg, leaving the static preview in place.
func renderAnimCmd(ctx context.Context, gen uint64, key animKey, targetW int, mode animMode) tea.Cmd {
	return func() tea.Msg {
		set := renderAnimFrames(ctx, key.vector, key.crop, targetW, key.boxW, key.boxH, mode)
		if set == nil {
			return nil
		}
		return animReadyMsg{gen: gen, key: key, set: set}
	}
}
