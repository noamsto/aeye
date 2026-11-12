package main

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFixFonts(t *testing.T) {
	t.Setenv("AEYE_D2_FONT", "") // default family
	in := []byte(`.r{font-family:"d2-123-font-regular";}` +
		`.m{font-family:"d2-123-font-mono-bold";}` + // compound variant
		`.text-bold {color:red;}` +
		`.text-italic {color:blue;}`)

	got := string(fixFonts(in))
	for _, want := range []string{
		`font-family:"Noto Sans"`,
		`.text-bold {font-weight:bold;color:red;}`,
		`.text-italic {font-style:italic;color:blue;}`,
	} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	// Compound family suffixes must be fully consumed, not left as "Noto Sans-bold".
	if bytes.Contains([]byte(got), []byte("Noto Sans-")) {
		t.Fatalf("compound font suffix leaked:\n%s", got)
	}

	// Idempotent: a second pass must not double the injected declarations.
	if again := string(fixFonts([]byte(got))); again != got {
		t.Fatalf("not idempotent:\n first: %s\nsecond: %s", got, again)
	}
}

func TestResvgFontArgs(t *testing.T) {
	check := func(t *testing.T, want []string) {
		t.Helper()
		if got := resvgFontArgs(); !slices.Equal(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	family := func(f string) []string {
		return []string{"--font-family", f, "--serif-family", f, "--sans-serif-family", f}
	}
	t.Run("defaults every generic to Noto Sans", func(t *testing.T) {
		t.Setenv("AEYE_D2_FONT", "")
		t.Setenv("AEYE_D2_FONT_DIR", "")
		check(t, family("Noto Sans"))
	})
	t.Run("follows AEYE_D2_FONT", func(t *testing.T) {
		t.Setenv("AEYE_D2_FONT", "Source Sans 3")
		t.Setenv("AEYE_D2_FONT_DIR", "")
		check(t, family("Source Sans 3"))
	})
	t.Run("pins the fonts dir when set", func(t *testing.T) {
		t.Setenv("AEYE_D2_FONT", "")
		t.Setenv("AEYE_D2_FONT_DIR", "/fonts")
		check(t, append(family("Noto Sans"), "--skip-system-fonts", "--use-fonts-dir", "/fonts"))
	})
}

func TestD2ThemeID(t *testing.T) {
	// Point themestate.Detect at a scratch state dir so the test controls the mode.
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	writeTheme := func(theme string) {
		if err := os.WriteFile(filepath.Join(state, "theme-state.json"), //nolint:gosec // test fixture in a temp dir
			[]byte(`{"theme":"`+theme+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("env wins", func(t *testing.T) {
		t.Setenv("AEYE_D2_THEME", "300")
		got, err := d2ThemeID()
		if err != nil || got != 300 {
			t.Fatalf("env override: got %d, %v; want 300", got, err)
		}
	})
	t.Run("invalid env errors", func(t *testing.T) {
		t.Setenv("AEYE_D2_THEME", "nope")
		if _, err := d2ThemeID(); err == nil {
			t.Fatal("expected an error for a non-numeric AEYE_D2_THEME")
		}
	})
	t.Run("dark mode -> 200", func(t *testing.T) {
		t.Setenv("AEYE_D2_THEME", "")
		writeTheme("dark")
		if got, _ := d2ThemeID(); got != 200 {
			t.Fatalf("dark mode: got %d, want 200", got)
		}
	})
	t.Run("light mode -> 105", func(t *testing.T) {
		t.Setenv("AEYE_D2_THEME", "")
		writeTheme("light")
		if got, _ := d2ThemeID(); got != 105 {
			t.Fatalf("light mode: got %d, want 105", got)
		}
	})
}

func TestRenderD2SVG(t *testing.T) {
	src := filepath.Join(t.TempDir(), "d.d2")
	if err := os.WriteFile(src, []byte("a -> b -> c\n"), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	svg, err := renderD2SVG(src)
	if err != nil {
		t.Fatalf("renderD2SVG: %v", err)
	}
	if !bytes.Contains(svg, []byte("<svg")) {
		t.Fatalf("output is not an SVG: %.80s", svg)
	}
}

func TestRunRenderDiagramSVGInput(t *testing.T) {
	dir := t.TempDir()
	runLog := filepath.Join(dir, "resvg.log")
	stub := filepath.Join(dir, "resvg")
	script := "#!/bin/sh\nfor a; do :; done\necho png > \"$a\"\necho run >> " + runLog + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil { //nolint:gosec // test stub must be executable
		t.Fatal(err)
	}
	t.Setenv("AEYE_RESVG", stub)

	in := filepath.Join(dir, "x.svg")
	want := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><!-- verbatim-marker-7731 --></svg>`)
	if err := os.WriteFile(in, want, 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	out := filepath.Join(dir, "x-dark.png")
	if err := runRenderDiagram(in, out); err != nil {
		t.Fatalf("runRenderDiagram: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "x-dark.svg")) //nolint:gosec // temp-dir path
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("sibling svg = %q, want input verbatim", got)
	}
	runs, err := os.ReadFile(runLog) //nolint:gosec // temp-dir path
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(runs, []byte("run")); n != 1 {
		t.Fatalf("resvg ran %d times, want 1", n)
	}
}

// TestRunRenderDiagramSVGRasterCap: a .svg input is rasterized at its declared
// size, so a tiny file declaring a huge canvas must be scaled down to the cap
// instead of driving resvg to gigabytes. Both bounds always go out — a normal
// svg gets its own (ceiled) size — so a misread size can only shrink the output.
func TestRunRenderDiagramSVGRasterCap(t *testing.T) {
	const ns = `xmlns="http://www.w3.org/2000/svg"`
	for _, tt := range []struct {
		name, attrs string
		w, h        string // --width/--height resvg must get
	}{
		{"huge square", `width="20000" height="20000"`, "8192", "8192"},
		{"tall", `width="100" height="30000"`, "28", "8192"},
		{"normal", `width="400" height="160"`, "400", "160"},
		{"fractional", `width="10.2" height="10.7"`, "11", "11"},
		{"pt units", `width="20000pt" height="100pt"`, "8192", "41"},
		{"percent of a huge viewBox", `width="100%" viewBox="0 0 20000 100"`, "8192", "41"},
		{"svg-namespaced size", `xmlns:s="http://www.w3.org/2000/svg" s:width="8500" s:height="8500" viewBox="0 0 10 10"`, "8192", "8192"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			runLog := filepath.Join(dir, "resvg.log")
			stub := filepath.Join(dir, "resvg")
			script := "#!/bin/sh\necho \"$*\" >> " + runLog + "\nfor a; do :; done\necho png > \"$a\"\n"
			if err := os.WriteFile(stub, []byte(script), 0o755); err != nil { //nolint:gosec // test stub must be executable
				t.Fatal(err)
			}
			t.Setenv("AEYE_RESVG", stub)
			in := filepath.Join(dir, "x.svg")
			if err := os.WriteFile(in, []byte(`<svg `+ns+` `+tt.attrs+`/>`), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
				t.Fatal(err)
			}
			if err := runRenderDiagram(in, filepath.Join(dir, "x-dark.png")); err != nil {
				t.Fatalf("runRenderDiagram: %v", err)
			}
			logged, err := os.ReadFile(runLog) //nolint:gosec // temp-dir path
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Fields(string(logged))
			flag := func(name string) string {
				if i := slices.Index(args, name); i >= 0 && i+1 < len(args) {
					return args[i+1]
				}
				return ""
			}
			if flag("--width") != tt.w || flag("--height") != tt.h {
				t.Fatalf("resvg args %v, want --width %s --height %s", args, tt.w, tt.h)
			}
		})
	}
}

// TestSVGSize pins svgSize to the canvas resvg 0.49 actually rasterizes each
// root tag at.
func TestSVGSize(t *testing.T) {
	for _, tt := range []struct {
		attrs string
		w, h  float64
	}{
		{`width="400" height="160"`, 400, 160},
		{`width="1in" height="1cm"`, 96, 96 / 2.54},
		{`width="3pc" height="3pt" `, 48, 4},
		{`width="25.4mm" height="10px"`, 96, 10},
		{`width="1e4" height="10"`, 10000, 10},
		{`viewBox="0 0 300,200"`, 300, 200},
		{`width="300%" height="100%" viewBox="0 0 100 50"`, 300, 50},
		{`width="50" viewBox="0 0 100 200"`, 50, 100},
		{`height="50" viewBox="0 0 100 200"`, 25, 50},
		{`width="200%" viewBox="0 0 100 200"`, 200, 400},
		{`width="8000" viewBox="0 0 1 1000"`, 8000, 8000000},
		{`width=" 50." height="+.5e2px"`, 50, 50},
		{`viewBox=" 0, 0 ,5E1	20 "`, 50, 20},
		{`xmlns:s="http://www.w3.org/2000/svg" s:width="30" s:height="20"`, 30, 20},
		{`xmlns:x="urn:x" x:width="9000" width="30" height="20"`, 30, 20},
	} {
		w, h, err := svgSize([]byte(`<?xml version="1.0"?><!-- c --><svg xmlns="http://www.w3.org/2000/svg" ` + tt.attrs + `/>`))
		if err != nil || math.Abs(w-tt.w) > 1e-9 || math.Abs(h-tt.h) > 1e-9 {
			t.Errorf("%s: got %vx%v, %v; want %vx%v", tt.attrs, w, h, err, tt.w, tt.h)
		}
	}
	for _, src := range []string{
		`<svg/>`,
		`<svg width="20000"/>`,
		`<svg width="100%" height="100%"/>`,
		`<svg width="1000em" height="1em" viewBox="0 0 10 10"/>`,
		`<svg width="px" height="10"/>`,
		`<svg width="Inf" height="10"/>`,
		`<html><svg width="10" height="10"/></html>`,
		`<!DOCTYPE svg [<!ENTITY w "20000">]><svg width="&w;" height="10"/>`,
		``,
		// resvg rejects each of these numbers, so it would not rasterize at them.
		`<svg width="0x1p3" height="0x1p3" viewBox="0 0 8500 8500"/>`,
		`<svg width="1_0" height="10"/>`,
		`<svg width="nan" height="10"/>`,
		`<svg width="50 " height="10" viewBox="0 0 8500 10"/>`,
		`<svg width="100%" height="100%" viewBox="0,,0,,10,,10"/>`,
		`<svg viewBox="0 0 0x1p3 0x1p3"/>`,
		`<svg viewBox=",0 0 10 10"/>`,
		`<svg width="1e400" height="10"/>`,
		`<svg width="1e308in" height="10"/>`,
		`<svg width="0" height="10"/>`,
		`<svg width="-5" height="10"/>`,
		`<svg xmlns:s="http://www.w3.org/2000/svg" width="10" s:width="8500" height="10"/>`,
	} {
		if w, h, err := svgSize([]byte(src)); err == nil {
			t.Errorf("%s: got %vx%v, want an error", src, w, h)
		}
	}
}

func TestRunRenderDiagramSVGUnknownSize(t *testing.T) {
	dir := t.TempDir()
	runLog := filepath.Join(dir, "resvg.log")
	stub := filepath.Join(dir, "resvg")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho run >> "+runLog+"\n"), 0o755); err != nil { //nolint:gosec // test stub must be executable
		t.Fatal(err)
	}
	t.Setenv("AEYE_RESVG", stub)
	in := filepath.Join(dir, "x.svg")
	if err := os.WriteFile(in, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="5" height="5"/></svg>`), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	err := runRenderDiagram(in, filepath.Join(dir, "x-dark.png"))
	want := "svg " + in + ": cannot determine its size (no width/height or viewBox)"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if exists(runLog) {
		t.Fatal("resvg ran on an svg of unknown size")
	}
}

// zeroPNG returns a w x h 8-bit grayscale PNG of zero pixels. It streams one
// reused zero row through zlib, so a canvas far larger than memory costs little.
func zeroPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var idat bytes.Buffer
	zw, err := zlib.NewWriterLevel(&idat, zlib.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	row := make([]byte, 1+w) // filter byte 0 (none) + w zero pixels
	for range h {
		if _, err := zw.Write(row); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	var png bytes.Buffer
	png.WriteString("\x89PNG\r\n\x1a\n")
	chunk := func(typ string, data []byte) {
		_ = binary.Write(&png, binary.BigEndian, uint32(len(data))) //nolint:gosec // test chunk sizes are small
		body := append([]byte(typ), data...)
		png.Write(body)
		_ = binary.Write(&png, binary.BigEndian, crc32.ChecksumIEEE(body))
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], uint32(w)) //nolint:gosec // test image sides are positive and small
	binary.BigEndian.PutUint32(ihdr[4:], uint32(h)) //nolint:gosec // test image sides are positive and small
	ihdr[8] = 8                                     // bit depth; color type 0 (grayscale), the rest zero
	chunk("IHDR", ihdr)
	chunk("IDAT", idat.Bytes())
	chunk("IEND", nil)
	return png.Bytes()
}

// pngProbeSVG is a 10x10 svg embedding a side x side PNG as a data: URI.
func pngProbeSVG(t *testing.T, side int) string {
	t.Helper()
	return `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 10 10">` +
		`<image width="10" height="10" href="data:image/png;base64,` +
		base64.StdEncoding.EncodeToString(zeroPNG(t, side, side)) + `"/></svg>`
}

// patternProbeSVG is a 10x10 svg whose pattern fill is scaled by scale, so resvg
// allocates a (10*scale)^2 pixmap for a 300-byte file.
func patternProbeSVG(scale int) string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 10 10"><defs>` +
		`<pattern id="p" width="10" height="10" patternUnits="userSpaceOnUse" patternTransform="scale(` + strconv.Itoa(scale) + `)">` +
		`<rect width="10" height="10" fill="red"/></pattern></defs><rect width="10" height="10" fill="url(#p)"/></svg>`
}

// TestRunRenderDiagramSVGRasterCapRealResvg runs the security probes through
// the real resvg: each PNG must come out capped (or the file be refused), never
// at the oversized canvas resvg reads. All but the first are written so a naive
// size estimate disagrees with resvg's own parse. The memory probes stay under
// the size cap, so only the memory bound stops them: they must fail fast with
// errDiagramTooLarge.
func TestRunRenderDiagramSVGRasterCapRealResvg(t *testing.T) {
	if _, err := exec.LookPath("resvg"); err != nil {
		t.Skip("resvg not on PATH")
	}
	t.Setenv("AEYE_RESVG", "")
	const ns = `xmlns="http://www.w3.org/2000/svg"`
	const big = `<rect width="8500" height="8500"/>`
	for _, tt := range []struct {
		name         string
		svg          string
		build        func(t *testing.T) string // svg generated at run time, only when the row runs
		memMB        string                    // AEYE_RESVG_MEM_MB override; empty keeps the default
		wantTooLarge bool
		linuxOnly    bool // needs the hard RLIMIT_AS bound
	}{
		{name: "declared huge", svg: `<svg ` + ns + ` width="20000" height="20000"/>`},
		{name: "svg-namespaced size", svg: `<svg ` + ns + ` xmlns:s="http://www.w3.org/2000/svg" s:width="8500" s:height="8500" viewBox="0 0 10 10"/>`},
		{name: "hex-float size", svg: `<svg ` + ns + ` width="0x1p3" height="0x1p3" viewBox="0 0 8500 8500"/>`},
		{name: "empty viewBox fields", svg: `<svg ` + ns + ` width="100%" height="100%" viewBox="0,,0,,10,,10">` + big + `</svg>`},
		{name: "hex-float viewBox", svg: `<svg ` + ns + ` viewBox="0 0 0x1p3 0x1p3">` + big + `</svg>`},
		{name: "scaled_pattern", svg: patternProbeSVG(1500), memMB: "256", wantTooLarge: true},
		{name: "scaled_png", build: func(t *testing.T) string { return pngProbeSVG(t, 16000) }, memMB: "256", wantTooLarge: true},
		{name: "pattern_scale_5000", svg: patternProbeSVG(5000), wantTooLarge: true, linuxOnly: true},
		{name: "embedded_png_40000", build: func(t *testing.T) string { return pngProbeSVG(t, 40000) }, wantTooLarge: true, linuxOnly: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.linuxOnly && runtime.GOOS != "linux" {
				t.Skip("needs the RLIMIT_AS bound")
			}
			if tt.memMB != "" {
				t.Setenv(resvgMemEnv, tt.memMB)
			}
			svg := tt.svg
			if tt.build != nil {
				svg = tt.build(t)
			}
			dir := t.TempDir()
			in := filepath.Join(dir, "probe.svg")
			if err := os.WriteFile(in, []byte(svg), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
				t.Fatal(err)
			}
			out := filepath.Join(dir, "probe-dark.png")
			start := time.Now()
			err := runRenderDiagram(in, out)
			if tt.wantTooLarge {
				if !errors.Is(err, errDiagramTooLarge) {
					t.Fatalf("runRenderDiagram err = %v, want errDiagramTooLarge", err)
				}
				if d := time.Since(start); d > 20*time.Second {
					t.Fatalf("refusal took %v, want well under the %v timeout", d, resvgTimeout)
				}
				return
			}
			if err != nil {
				if !strings.Contains(err.Error(), "cannot determine its size") {
					t.Fatalf("runRenderDiagram: %v", err)
				}
				return
			}
			size, ok := pngSize(out)
			if !ok || max(size.X, size.Y) > maxRasterSide {
				t.Fatalf("probe rasterized at %v (ok=%v), want long side <= %d", size, ok, maxRasterSide)
			}
		})
	}
	// The guard itself: both bounds always reach resvg, so an estimate far below
	// resvg's own canvas shrinks the output instead of letting it through.
	t.Run("underestimate only shrinks", func(t *testing.T) {
		dir := t.TempDir()
		in, out := filepath.Join(dir, "probe.svg"), filepath.Join(dir, "probe.png")
		if err := os.WriteFile(in, []byte(`<svg `+ns+` width="8500" height="8500"/>`), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
			t.Fatal(err)
		}
		args := append(rasterCapArgs(10, 10), in, out)
		if b, err := exec.Command("resvg", args...).CombinedOutput(); err != nil { //nolint:gosec // args built by the test, not shell-interpreted
			t.Fatalf("resvg: %v: %s", err, b)
		}
		if size, ok := pngSize(out); !ok || max(size.X, size.Y) > 10 {
			t.Fatalf("rasterized at %v (ok=%v), want long side <= 10", size, ok)
		}
	})

	// A coarse, process-wide bound: the peak resident size of any child this test
	// binary has waited for, not just the probes above.
	if maxKiB, ok := childMaxRSSKiB(); ok && maxKiB > resvgMemLimitMiB*1024 {
		t.Fatalf("largest child peaked at %d KiB, want <= %d KiB (the default bound)", maxKiB, resvgMemLimitMiB*1024)
	}
}

// d2HugeSrc declares a canvas far past maxRasterSide: d2 compiles it to a
// viewBox of about 30200 units, which resvg would rasterize at full size.
const d2HugeSrc = "big: {width: 30000; height: 30000}\n"

// stubResvgArgs installs a resvg stub that logs its arguments and writes a
// placeholder PNG, returning the log path. Nothing near the canvas is allocated.
func stubResvgArgs(t *testing.T, dir string) string {
	t.Helper()
	runLog := filepath.Join(dir, "resvg.log")
	stub := filepath.Join(dir, "resvg")
	script := "#!/bin/sh\necho \"$*\" >> " + runLog + "\nfor a; do :; done\necho png > \"$a\"\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil { //nolint:gosec // test stub must be executable
		t.Fatal(err)
	}
	t.Setenv("AEYE_RESVG", stub)
	return runLog
}

// requireCap fails unless resvg was given a --width and --height of 8192.
func requireCap(t *testing.T, runLog string) {
	t.Helper()
	logged, err := os.ReadFile(runLog) //nolint:gosec // temp-dir path
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Fields(string(logged))
	flag := func(name string) string {
		if i := slices.Index(args, name); i >= 0 && i+1 < len(args) {
			return args[i+1]
		}
		return ""
	}
	if flag("--width") != "8192" || flag("--height") != "8192" {
		t.Fatalf("resvg args %v, want --width 8192 --height 8192", args)
	}
}

// TestRunRenderDiagramD2RasterCap: a D2 diagram declaring a huge canvas must be
// handed to resvg capped, like a .svg input, not at its declared size.
func TestRunRenderDiagramD2RasterCap(t *testing.T) {
	dir := t.TempDir()
	runLog := stubResvgArgs(t, dir)
	in := filepath.Join(dir, "big.d2")
	if err := os.WriteFile(in, []byte(d2HugeSrc), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	if err := runRenderDiagram(in, filepath.Join(dir, "big-dark.png")); err != nil {
		t.Fatalf("runRenderDiagram: %v", err)
	}
	requireCap(t, runLog)
}

// TestRunRenderD2RasterCap: the same cap on the aeye render entry point, which
// pipes the compiled SVG to resvg on stdin rather than through a sibling file.
func TestRunRenderD2RasterCap(t *testing.T) {
	dir := t.TempDir()
	runLog := stubResvgArgs(t, dir)
	in := filepath.Join(dir, "big.d2")
	if err := os.WriteFile(in, []byte(d2HugeSrc), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	if err := runRender("d2", in, filepath.Join(dir, "big.png"), nil); err != nil {
		t.Fatalf("runRender: %v", err)
	}
	requireCap(t, runLog)
}

// TestD2RasterCapRealResvg runs the oversized diagram through the real resvg on
// both D2 paths: the PNG must come out at most maxRasterSide on its long side.
// A normal diagram must keep exactly the pixel size resvg gives it uncapped.
func TestD2RasterCapRealResvg(t *testing.T) {
	if _, err := exec.LookPath("resvg"); err != nil {
		t.Skip("resvg not on PATH")
	}
	t.Setenv("AEYE_RESVG", "")
	dir := t.TempDir()
	// 8500 is just past the cap, keeping the uncapped render cheap.
	big := filepath.Join(dir, "big.d2")
	if err := os.WriteFile(big, []byte("big: {width: 8500; height: 8500}\n"), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	paths := []struct {
		name string
		run  func(in, out string) error
	}{
		{"runRenderDiagram", runRenderDiagram},
		{"runRender", func(in, out string) error { return runRender("d2", in, out, nil) }},
	}
	huge := filepath.Join(dir, "huge.d2")
	if err := os.WriteFile(huge, []byte(d2HugeSrc), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	for _, p := range paths {
		for _, in := range []struct{ name, path string }{{"oversized", big}, {"30000", huge}} {
			t.Run(p.name+" "+in.name, func(t *testing.T) {
				out := filepath.Join(dir, p.name+"-"+in.name+".png")
				if err := p.run(in.path, out); err != nil {
					t.Fatalf("render: %v", err)
				}
				size, ok := pngSize(out)
				if !ok || max(size.X, size.Y) > maxRasterSide {
					t.Fatalf("rasterized at %v (ok=%v), want long side <= %d", size, ok, maxRasterSide)
				}
			})
		}
	}

	// Normal size: the cap must not change the pixels. The reference is resvg
	// run on the same compiled SVG with no size arguments.
	small := filepath.Join(dir, "small.d2")
	if err := os.WriteFile(small, []byte("a -> b -> c\n"), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	svg, err := renderD2SVG(small)
	if err != nil {
		t.Fatal(err)
	}
	svgPath := filepath.Join(dir, "ref.svg")
	if err := os.WriteFile(svgPath, fixFonts(svg), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	ref := filepath.Join(dir, "ref.png")
	if b, err := exec.Command("resvg", append(resvgFontArgs(), svgPath, ref)...).CombinedOutput(); err != nil { //nolint:gosec // args built by the test
		t.Fatalf("reference resvg: %v: %s", err, b)
	}
	want, ok := pngSize(ref)
	if !ok {
		t.Fatal("reference PNG unreadable")
	}
	for _, p := range paths {
		t.Run(p.name+" normal", func(t *testing.T) {
			out := filepath.Join(dir, p.name+"-small.png")
			if err := p.run(small, out); err != nil {
				t.Fatalf("render: %v", err)
			}
			if got, ok := pngSize(out); !ok || got != want {
				t.Fatalf("rasterized at %v (ok=%v), want %v as uncapped", got, ok, want)
			}
		})
	}
}

// TestRenderD2SVGHonorsInFileLayout guards the regression where presetting
// CompileOptions.Layout silently defeats in-file `layout-engine: elk` (d2 only
// reads d2-config's engine when Layout is nil). Same graph, elk vs dagre: if
// the override is honored the two layouts — and thus the SVGs — differ.
func TestRenderD2SVGHonorsInFileLayout(t *testing.T) {
	t.Setenv("AEYE_D2_SKETCH", "0") // deterministic output
	dir := t.TempDir()
	graph := "a -> b\na -> c\nb -> d\nc -> d\na -> d\n"
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
			t.Fatal(err)
		}
		return p
	}
	elk, err := renderD2SVG(write("elk.d2", "vars: { d2-config: { layout-engine: elk } }\n"+graph))
	if err != nil {
		t.Fatal(err)
	}
	dagre, err := renderD2SVG(write("dagre.d2", graph))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(elk, dagre) {
		t.Fatal("in-file layout-engine: elk was ignored — output identical to dagre")
	}
}
