package main

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// graphvizSVG is shaped like Graphviz output: the root carries width and height
// in pt, and a newline separates them from viewBox.
const graphvizSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="532pt" height="548pt"
 viewBox="0.00 0.00 532.00 548.00">
<rect x="0" y="0" width="532" height="548" fill="#ffffff"/>
<rect x="0" y="0" width="532" height="60" fill="#ff0000"/>
<rect x="0" y="488" width="532" height="60" fill="#0000ff"/>
</svg>
`

// TestCropViewBoxDropsRootSize: a cropped root must not keep its fixed size, or
// resvg sizes the canvas from it and fits the crop inside (letterboxing).
func TestCropViewBoxDropsRootSize(t *testing.T) {
	out, ok := cropViewBox([]byte(graphvizSVG), cropFrac{0.25, 0.40, 0.75, 0.60})
	if !ok {
		t.Fatal("expected crop applied")
	}
	want := "<svg xmlns=\"http://www.w3.org/2000/svg\"\n viewBox=\"133.0000 219.2000 266.0000 109.6000\">"
	if !bytes.Contains(out, []byte(want)) {
		t.Errorf("root size not dropped / viewBox not rewritten:\n%s", out)
	}
}

// TestCropViewBoxKeepsNestedSize: only the root tag loses its size; a nested
// <svg> (D2 nests one) keeps its width and height.
func TestCropViewBoxKeepsNestedSize(t *testing.T) {
	svg := []byte(`<svg width="10" viewBox="0 0 100 100"><svg width="40" height="50" viewBox="0 0 1 1"></svg></svg>`)
	out, ok := cropViewBox(svg, cropFrac{0, 0, 0.5, 0.5})
	if !ok {
		t.Fatal("expected crop applied")
	}
	want := `<svg viewBox="0.0000 0.0000 50.0000 50.0000"><svg width="40" height="50" viewBox="0 0 1 1"></svg></svg>`
	if string(out) != want {
		t.Errorf("got %s\nwant %s", out, want)
	}
}

// TestCropViewBoxKeepsSimilarAttrs: whole-name matching only; stroke-width and
// data-width on the root are not size attributes and must survive.
func TestCropViewBoxKeepsSimilarAttrs(t *testing.T) {
	svg := []byte(`<svg width="10" stroke-width="2" data-width="3" height="20" viewBox="0 0 100 100"></svg>`)
	out, ok := cropViewBox(svg, cropFrac{0, 0, 0.5, 0.5})
	if !ok {
		t.Fatal("expected crop applied")
	}
	want := `<svg stroke-width="2" data-width="3" viewBox="0.0000 0.0000 50.0000 50.0000"></svg>`
	if string(out) != want {
		t.Errorf("got %s\nwant %s", out, want)
	}
}

// TestCropViewBoxNestedViewBoxRefused: a root without a viewBox whose first
// viewBox sits on a nested element (symbol, marker, nested svg) must not be
// cropped: rewriting the nested one would not crop the render, so the caller
// has to fall back. The input comes back byte-identical.
func TestCropViewBoxNestedViewBoxRefused(t *testing.T) {
	cases := map[string]string{
		"symbol":     `<svg width="10"><rect width="5" height="5"/><symbol viewBox="0 0 100 100"></symbol></svg>`,
		"marker":     `<svg width="100" height="100"><rect width="50" height="50"/><marker id="m" viewBox="0 0 10 10" markerWidth="4"></marker></svg>`,
		"nested-svg": `<svg width="100" height="100"><svg viewBox="0 0 10 10" width="5" height="5"></svg></svg>`,
	}
	for name, in := range cases {
		out, ok := cropViewBox([]byte(in), cropFrac{0, 0, 0.5, 0.5})
		if ok {
			t.Errorf("%s: crop applied to a nested viewBox: %s", name, out)
		}
		if string(out) != in {
			t.Errorf("%s: input changed: %s", name, out)
		}
	}
}

// TestCropViewBoxSkipsProlog: an XML declaration, DOCTYPE and comments before
// the root (one of which mentions <svg) do not confuse root detection.
func TestCropViewBoxSkipsProlog(t *testing.T) {
	in := `<?xml version="1.0"?><!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "x.dtd"><!-- <svg viewBox="1 1 1 1"> --><svg width="100" viewBox="0 0 100 100"><marker viewBox="0 0 1 1"/></svg>`
	want := `<?xml version="1.0"?><!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "x.dtd"><!-- <svg viewBox="1 1 1 1"> --><svg viewBox="0.0000 0.0000 50.0000 50.0000"><marker viewBox="0 0 1 1"/></svg>`
	out, ok := cropViewBox([]byte(in), cropFrac{0, 0, 0.5, 0.5})
	if !ok {
		t.Fatal("expected crop applied")
	}
	if string(out) != want {
		t.Errorf("got %s\nwant %s", out, want)
	}
}

// TestCropViewBoxQuotedValues: a '>' or a width="..." inside a quoted attribute
// value is not a tag boundary or a size attribute.
func TestCropViewBoxQuotedValues(t *testing.T) {
	cases := map[string][2]string{
		"gt-in-value": {
			`<svg viewBox="0 0 100 100" aria-label="a>b" width="100" height="100"></svg>`,
			`<svg viewBox="0.0000 0.0000 50.0000 50.0000" aria-label="a>b"></svg>`,
		},
		"size-text-in-value": {
			`<svg width="10" data-x='a width="3"' viewBox="0 0 100 100"></svg>`,
			`<svg data-x='a width="3"' viewBox="0.0000 0.0000 50.0000 50.0000"></svg>`,
		},
		"prefixed-root": {
			`<svg:svg xmlns:svg="http://www.w3.org/2000/svg" width="100" viewBox="0 0 100 100"></svg:svg>`,
			`<svg:svg xmlns:svg="http://www.w3.org/2000/svg" viewBox="0.0000 0.0000 50.0000 50.0000"></svg:svg>`,
		},
		"comment-before-root": {
			`<!-- <svg> --><svg width="100" viewBox="0 0 100 100"></svg>`,
			`<!-- <svg> --><svg viewBox="0.0000 0.0000 50.0000 50.0000"></svg>`,
		},
	}
	for name, c := range cases {
		out, ok := cropViewBox([]byte(c[0]), cropFrac{0, 0, 0.5, 0.5})
		if !ok {
			t.Fatalf("%s: expected crop applied", name)
		}
		if string(out) != c[1] {
			t.Errorf("%s: got %s\nwant %s", name, out, c[1])
		}
	}
}

// TestCropViewBoxD2Unchanged:a root without size attributes (D2) is rewritten
// exactly as before: only the viewBox changes.
func TestCropViewBoxD2Unchanged(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50"><g/></svg>`)
	out, ok := cropViewBox(svg, cropFrac{0.2, 0.2, 0.6, 0.7})
	if !ok {
		t.Fatal("expected crop applied")
	}
	want := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="20.0000 10.0000 40.0000 25.0000"><g/></svg>`
	if string(out) != want {
		t.Errorf("got %s\nwant %s", out, want)
	}
}

// TestRenderVectorCropAspectRealResvg: a crop of a sized (Graphviz-shaped) svg
// must render at the crop's aspect ratio, not the full diagram's.
func TestRenderVectorCropAspectRealResvg(t *testing.T) {
	if _, err := exec.LookPath("resvg"); err != nil {
		t.Skip("resvg not on PATH")
	}
	t.Setenv("AEYE_RESVG", "")
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	svg := filepath.Join(dir, "graph.svg")
	if err := os.WriteFile(svg, []byte(graphvizSVG), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	crop := cropFrac{0.25, 0.40, 0.75, 0.60}
	out := renderVector(svg, crop, 1200)
	if out == "" {
		t.Fatal("renderVector failed")
	}
	size, ok := pngSize(out)
	if !ok {
		t.Fatalf("cannot read png size of %s", out)
	}
	want := (crop.h() * 548) / (crop.w() * 532)
	got := float64(size.Y) / float64(size.X)
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("rendered %v (aspect %.3f), want crop aspect %.3f", size, got, want)
	}
}

func TestCropViewBox(t *testing.T) {
	svg := []byte(`<svg viewBox="0 0 100 50"><g/></svg>`)
	out, ok := cropViewBox(svg, cropFrac{0.2, 0.2, 0.6, 0.7})
	if !ok {
		t.Fatal("expected crop applied")
	}
	// x: 0.2*100=20  y: 0.2*50=10  w: 0.4*100=40  h: 0.5*50=25
	if !bytes.Contains(out, []byte(`viewBox="20.0000 10.0000 40.0000 25.0000"`)) {
		t.Errorf("viewBox not rewritten to crop: %s", out)
	}
}

func TestCropViewBoxHonorsOffset(t *testing.T) {
	svg := []byte(`<svg viewBox="-10 -20 200 100"></svg>`)
	out, ok := cropViewBox(svg, cropFrac{0, 0, 0.5, 0.5})
	if !ok {
		t.Fatal("expected crop applied")
	}
	// origin offset carries through: x:-10  y:-20  w:0.5*200=100  h:0.5*100=50
	if !bytes.Contains(out, []byte(`viewBox="-10.0000 -20.0000 100.0000 50.0000"`)) {
		t.Errorf("offset viewBox not honored: %s", out)
	}
}

func TestCropViewBoxOnlyOuter(t *testing.T) {
	// Outer + inner d2-svg both carry a viewBox; only the first (outer) is the
	// crop window — the inner must be left intact.
	svg := []byte(`<svg viewBox="0 0 100 100"><svg viewBox="0 0 100 100"></svg></svg>`)
	out, _ := cropViewBox(svg, cropFrac{0, 0, 0.5, 0.5})
	if n := bytes.Count(out, []byte(`viewBox="0 0 100 100"`)); n != 1 {
		t.Errorf("inner viewBox should be untouched, found %d originals: %s", n, out)
	}
}

func TestCropViewBoxNoViewBox(t *testing.T) {
	if _, ok := cropViewBox([]byte(`<svg></svg>`), cropFrac{0, 0, 1, 1}); ok {
		t.Error("no viewBox → ok must be false")
	}
}

func TestRenderVectorMissingResvg(t *testing.T) {
	// A real svg file so we exercise the resvg-lookup path, not the stat bail.
	svg := filepath.Join(t.TempDir(), "x.svg")
	if err := os.WriteFile(svg, []byte(`<svg viewBox="0 0 10 10"></svg>`), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	t.Setenv("AEYE_RESVG", "/definitely/not/resvg")
	if got := renderVector(svg, fullCrop(), 1000); got != "" {
		t.Errorf("absent resvg must yield empty string, got %q", got)
	}
}

// TestRenderVectorBoundsHeight: the sharp render is sized by width, so a tall
// svg would come out arbitrarily tall without a height bound too.
func TestRenderVectorBoundsHeight(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	runLog := filepath.Join(dir, "resvg.log")
	stub := filepath.Join(dir, "resvg")
	script := "#!/bin/sh\necho \"$*\" >> " + runLog + "\nfor a; do :; done\necho png > \"$a\"\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil { //nolint:gosec // test stub must be executable
		t.Fatal(err)
	}
	t.Setenv("AEYE_RESVG", stub)
	svg := filepath.Join(dir, "x.svg")
	if err := os.WriteFile(svg, []byte(`<svg viewBox="0 0 100 8000"></svg>`), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	if renderVector(svg, fullCrop(), 1600) == "" {
		t.Fatal("renderVector failed")
	}
	logged, err := os.ReadFile(runLog) //nolint:gosec // temp-dir path
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(logged, []byte("--width 1600 --height 16384 ")) {
		t.Fatalf("resvg args %q, want --width 1600 --height 16384", logged)
	}
}

func TestRenderVectorTallSVGRealResvg(t *testing.T) {
	if _, err := exec.LookPath("resvg"); err != nil {
		t.Skip("resvg not on PATH")
	}
	t.Setenv("AEYE_RESVG", "")
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	svg := filepath.Join(dir, "tall.svg")
	if err := os.WriteFile(svg, []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="8000"/>`), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	out := renderVector(svg, fullCrop(), 1600)
	if out == "" {
		t.Fatal("renderVector failed")
	}
	if size, ok := pngSize(out); !ok || max(size.X, size.Y) > maxVectorHeight {
		t.Fatalf("rendered at %v (ok=%v), want long side <= %d", size, ok, maxVectorHeight)
	}
}

// blockingStub is an AEYE_RESVG stand-in that logs "<pid> <args>" per call and
// then blocks, polling in short sleeps (an orphaned long sleep would hold the
// output pipe open past the kill), until its block file is removed; it then
// copies a real PNG to its output so the caller can decode it.
type blockingStub struct {
	svg, log, block string
}

func newBlockingStub(t *testing.T) blockingStub {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("AEYE_D2_FONT_DIR", "")
	st := blockingStub{
		svg:   filepath.Join(dir, "d.svg"),
		log:   filepath.Join(dir, "calls.log"),
		block: filepath.Join(dir, "block"),
	}
	body := fmt.Sprintf("#!/bin/sh\nfor a; do out=$a; done\necho \"$$ $*\" >> %q\n"+
		"while [ -e %q ]; do sleep 0.05; done\ncp %q \"$out\"\n", st.log, st.block, writeTestPNG(t))
	script := filepath.Join(dir, "resvg")
	for path, content := range map[string]string{
		script:   body,
		st.svg:   staticFixtureSVG,
		st.block: "",
	} {
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil { //nolint:gosec // test stub must be executable
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { os.Remove(st.block) }) //nolint:errcheck,gosec // a red run must not leave a stub holding a resvg slot
	t.Setenv("AEYE_RESVG", script)
	return st
}

func (st blockingStub) unblock(t *testing.T) {
	t.Helper()
	if err := os.Remove(st.block); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// waitStarts waits until the stub has been launched n times and returns the pid
// of the latest launch.
func (st blockingStub) waitStarts(t *testing.T, n int) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(st.log); err == nil { //nolint:gosec // temp-dir path
			if lines := strings.Split(strings.TrimSpace(string(data)), "\n"); len(lines) >= n {
				pid, err := strconv.Atoi(strings.Fields(lines[n-1])[0])
				if err != nil {
					t.Fatal(err)
				}
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("stub started fewer than %d times", n)
	return 0
}

// recv returns what ch delivers, failing if nothing arrives in time.
func recv[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("render did not return after cancel")
		panic("unreachable")
	}
}

func processDead(pid int) bool {
	p, err := os.FindProcess(pid)
	return err != nil || p.Signal(syscall.Signal(0)) != nil
}

func TestRenderVectorCtxCancelKillsInFlight(t *testing.T) {
	st := newBlockingStub(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan string, 1)
	go func() { done <- renderVectorCtx(ctx, st.svg, fullCrop(), 800) }()
	pid := st.waitStarts(t, 1)

	cancel()
	if got := recv(t, done); got != "" {
		t.Fatalf("cancelled render returned %q, want empty", got)
	}
	left, _ := filepath.Glob(filepath.Join(os.TempDir(), "aeye-vec-*.png"))
	if len(left) != 0 {
		t.Errorf("cancelled render left output behind: %v", left)
	}
	for deadline := time.Now().Add(2 * time.Second); !processDead(pid) && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if !processDead(pid) {
		t.Errorf("resvg pid %d still running after cancel", pid)
	}
}

func TestRenderVectorCtxCancelledBeforeStartKeepsCache(t *testing.T) {
	st := newBlockingStub(t)
	st.unblock(t)
	cropA, cropB := cropFrac{0, 0, 0.5, 0.5}, cropFrac{0.5, 0.5, 1, 1}
	cached := renderVectorCtx(context.Background(), st.svg, cropA, 800)
	if cached == "" {
		t.Fatal("seed render failed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := renderVectorCtx(ctx, st.svg, cropB, 800); got != "" {
		t.Fatalf("cancelled render returned %q, want empty", got)
	}
	if n := logLines(t, st.log); n != 1 {
		t.Errorf("resvg ran %d times, want only the seed render", n)
	}
	if !exists(cached) {
		t.Error("a cancelled render evicted the cached framing")
	}
}

// vectorKick runs the model's render cmd off the test goroutine.
func vectorKick(cmd tea.Cmd) <-chan tea.Msg {
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	return ch
}

func TestKickVectorSupersededFramingCancelled(t *testing.T) {
	// Each move leaves the latest framing's render to the test (nil: kick it).
	moves := map[string]func(m *galleryModel) tea.Cmd{
		"scheduleVector": func(m *galleryModel) tea.Cmd { m.scheduleVector(); return nil },
		"kickVector":     func(m *galleryModel) tea.Cmd { return m.kickVector() },
	}
	for name, supersede := range moves {
		t.Run(name, func(t *testing.T) {
			st := newBlockingStub(t)
			m, _ := newAnimModel(t, 1, st.svg, animOff)
			first := vectorKick(m.kickVector())
			st.waitStarts(t, 1)

			m.crop = cropFrac{0, 0, 0.5, 0.5}
			cmd := supersede(m)
			if msg := recv(t, first); msg != nil {
				t.Fatalf("superseded render delivered %#v", msg)
			}

			st.unblock(t)
			if cmd == nil {
				cmd = m.kickVector()
			}
			if cmd == nil {
				t.Fatal("latest framing was not kicked")
			}
			msg, ok := cmd().(vectorReadyMsg)
			if !ok || msg.crop != m.crop {
				t.Fatalf("latest framing result = %#v, want a vectorReadyMsg for %v", msg, m.crop)
			}
		})
	}
}

// TestKickVectorSameFramingKept: a repeat kick or schedule for the framing
// already rendering keeps it, so settle, focus and cache-fill kicks don't
// restart a slow render.
func TestKickVectorSameFramingKept(t *testing.T) {
	st := newBlockingStub(t)
	m, _ := newAnimModel(t, 1, st.svg, animOff)
	first := vectorKick(m.kickVector())
	st.waitStarts(t, 1)

	if again := m.kickVector(); again != nil {
		t.Fatal("same-framing kick restarted the render")
	}
	m.scheduleVector()
	st.unblock(t)
	if _, ok := recv(t, first).(vectorReadyMsg); !ok {
		t.Fatal("the in-flight render was cancelled")
	}
	if n := logLines(t, st.log); n != 1 {
		t.Errorf("resvg ran %d times, want 1", n)
	}
	if m.kickVector() == nil {
		t.Error("a finished render still counts as in flight")
	}
}

// TestKickVectorNothingToSharpenCancelsInFlight: a reload that lands on a
// selection with nothing to sharpen must not leave the previous render running.
func TestKickVectorNothingToSharpenCancelsInFlight(t *testing.T) {
	st := newBlockingStub(t)
	m, _ := newAnimModel(t, 1, st.svg, animOff)
	first := vectorKick(m.kickVector())
	st.waitStarts(t, 1)

	m.backend = backendSymbols
	if cmd := m.kickVector(); cmd != nil {
		t.Fatal("kick with nothing to sharpen returned a render")
	}
	if msg := recv(t, first); msg != nil {
		t.Fatalf("in-flight render delivered %#v after cancel", msg)
	}
}
