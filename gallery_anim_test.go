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
	"path/filepath"
	"strings"
	"sync"
	"testing"
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
	body := fmt.Sprintf("#!/bin/sh\nfor a; do in=$out; out=$a; done\necho call >> %q\n"+
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

	set := renderAnimFrames(ctx, svg, fullCrop(), boxW, boxW, boxH, animPartial)
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

	if again := renderAnimFrames(ctx, svg, fullCrop(), boxW, boxW, boxH, animPartial); again != set {
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
	cropped := renderAnimFrames(ctx, svg, half, boxW, boxW, boxH, animPartial)
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

	full := renderAnimFrames(ctx, svg, half, boxW, boxW, boxH, animFull)
	if full == nil {
		t.Fatal("full-mode render = nil")
	}
	for i, f := range full.Plan.Frames {
		if !f.Full {
			t.Errorf("animFull frame %d is partial", i)
		}
	}

	if got := renderAnimFrames(ctx, svg, fullCrop(), boxW, boxW, boxH, animOff); got != nil {
		t.Error("animOff rendered frames")
	}

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	other := cropFrac{0.25, 0.25, 0.75, 0.75}
	if got := renderAnimFrames(cctx, svg, other, boxW, boxW, boxH, animPartial); got != nil {
		t.Error("cancelled ctx returned a set")
	}
	key := animKey{id: 1, vector: svg, crop: other, boxW: boxW, boxH: boxH}
	if msg := renderAnimCmd(cctx, 1, key, boxW, animPartial)(); msg != nil {
		t.Errorf("cancelled renderAnimCmd msg = %#v, want nil", msg)
	}
	if msg, ok := renderAnimCmd(ctx, 7, key, boxW, animPartial)().(animReadyMsg); !ok || msg.gen != 7 || msg.key != key || msg.set == nil {
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
			sets[i] = renderAnimFrames(context.Background(), svg, fullCrop(), 40, 40, 30, animPartial)
		})
	}
	wg.Wait()
	for i := range 8 {
		if !animates[i] || sets[i] == nil {
			t.Errorf("goroutine %d: animates=%v set=%v", i, animates[i], sets[i])
		}
	}
}
