//go:build !windows

package main

import (
	"bytes"
	"context"
	"errors"
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

// runBoundedModes lists the bounding modes this OS can exercise.
func runBoundedModes() []boundMode {
	if runtime.GOOS == "linux" {
		return []boundMode{boundWatch, boundRLimit}
	}
	return []boundMode{boundWatch}
}

func writeResvgStub(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "resvg-stub")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil { //nolint:gosec // test stub must be executable
		t.Fatal(err)
	}
	return path
}

// TestResvgHogHelper is the watch-mode memory hog, re-exec'd by
// TestRunBoundedWatchKillsHog. It writes non-zero bytes so the pages are
// resident (and incompressible) before it sleeps.
func TestResvgHogHelper(t *testing.T) {
	if os.Getenv("AEYE_RESVG_HOG") == "" {
		t.Skip("subprocess helper")
	}
	buf := make([]byte, 1<<30)
	for i := range buf {
		buf[i] = byte(i) | 1
	}
	time.Sleep(time.Minute)
	os.Exit(int(buf[0]))
}

func TestRunBoundedWatchKillsHog(t *testing.T) {
	t.Setenv("AEYE_RESVG_HOG", "1")
	start := time.Now()
	_, err := runBounded(context.Background(), nil, os.Args[0], []string{"-test.run=^TestResvgHogHelper$"}, boundWatch, 256<<20, time.Minute)
	if !errors.Is(err, errDiagramTooLarge) {
		t.Fatalf("err = %v, want errDiagramTooLarge", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("hog took %v to be killed, want < 10s", d)
	}
}

func TestRunBoundedRLimitApplied(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RLIMIT_AS wrapper is linux-only")
	}
	stub := writeResvgStub(t, "ulimit -S -v")
	out, err := runBounded(context.Background(), nil, stub, nil, boundRLimit, 256<<20, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(out)), strconv.Itoa(256<<10); got != want {
		t.Fatalf("ulimit -S -v = %q, want %q", got, want)
	}
}

func TestRunBoundedRLimitAbortClassified(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RLIMIT_AS wrapper is linux-only")
	}
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"abort with allocation message", "echo 'memory allocation of 1 bytes failed' >&2; kill -ABRT $$", true},
		{"plain exit with the same message", "echo 'memory allocation of 1 bytes failed' >&2; exit 1", false},
		{"abort without the message", "echo oops >&2; kill -ABRT $$", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := writeResvgStub(t, tc.body)
			_, err := runBounded(context.Background(), nil, stub, nil, boundRLimit, 256<<20, time.Minute)
			if err == nil {
				t.Fatal("want an error")
			}
			if got := errors.Is(err, errDiagramTooLarge); got != tc.want {
				t.Fatalf("too-large = %v, want %v (err: %v)", got, tc.want, err)
			}
		})
	}
}

func TestRunBoundedTimeout(t *testing.T) {
	stub := writeResvgStub(t, "exec sleep 30")
	for _, mode := range runBoundedModes() {
		t.Run([]string{"rlimit", "watch"}[mode], func(t *testing.T) {
			start := time.Now()
			_, err := runBounded(context.Background(), nil, stub, nil, mode, 1<<30, 300*time.Millisecond)
			if !errors.Is(err, errDiagramTooLarge) {
				t.Fatalf("err = %v, want errDiagramTooLarge", err)
			}
			if !strings.Contains(err.Error(), "timed out") {
				t.Fatalf("err = %q, want it to mention the timeout", err)
			}
			if d := time.Since(start); d > 5*time.Second {
				t.Fatalf("took %v, want < 5s", d)
			}
		})
	}
}

func TestRunBoundedParentCancel(t *testing.T) {
	stub := writeResvgStub(t, "exec sleep 30")
	for _, mode := range runBoundedModes() {
		t.Run([]string{"rlimit", "watch"}[mode], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(100*time.Millisecond, cancel)
			start := time.Now()
			_, err := runBounded(ctx, nil, stub, nil, mode, 1<<30, time.Minute)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
			if errors.Is(err, errDiagramTooLarge) {
				t.Fatalf("parent cancel classified as too large: %v", err)
			}
			if d := time.Since(start); d > 5*time.Second {
				t.Fatalf("took %v, want < 5s", d)
			}
		})
	}
}

func TestRunResvgOtherFailure(t *testing.T) {
	t.Setenv("AEYE_RESVG", writeResvgStub(t, "echo boom >&2; exit 1"))
	_, err := runResvg(context.Background(), nil)
	if err == nil {
		t.Fatal("want an error")
	}
	if errors.Is(err, errDiagramTooLarge) {
		t.Fatalf("plain failure classified as too large: %v", err)
	}
	if want := "resvg: exit status 1: boom"; !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("err = %q, want prefix %q", err, want)
	}
}

func TestRunResvgMissingBinary(t *testing.T) {
	t.Setenv("AEYE_RESVG", "/nope/resvg")
	_, err := runResvg(context.Background(), nil)
	if _, ok := errors.AsType[*exec.Error](err); !ok {
		t.Fatalf("err = %v, want an exec lookup error", err)
	}
	if errors.Is(err, errDiagramTooLarge) {
		t.Fatalf("missing binary classified as too large: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "resvg: ") {
		t.Fatalf("err = %q, want a resvg: prefix", err)
	}
}

func TestRunResvgStdin(t *testing.T) {
	t.Setenv("AEYE_RESVG", writeResvgStub(t, `for a; do out=$a; done; cat > "$out"`))
	out := filepath.Join(t.TempDir(), "out.bin")
	want := []byte("<svg>stdin payload</svg>")
	if _, err := runResvg(context.Background(), bytes.NewReader(want), "-", out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out) //nolint:gosec // test temp path
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("stdin passthrough = %q, want %q", got, want)
	}
}

func TestResvgMemLimit(t *testing.T) {
	tests := []struct {
		name string
		set  bool
		env  string
		want int
	}{
		{"unset", false, "", resvgMemLimitMiB},
		{"empty", true, "", resvgMemLimitMiB},
		{"valid", true, "256", 256},
		{"zero", true, "0", resvgMemLimitMiB},
		{"negative", true, "-5", resvgMemLimitMiB},
		{"not a number", true, "abc", resvgMemLimitMiB},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(resvgMemEnv, tc.env)
			} else {
				t.Setenv(resvgMemEnv, "")
				os.Unsetenv(resvgMemEnv) //nolint:errcheck // restored by t.Setenv cleanup
			}
			if got := resvgMemLimit(); got != tc.want {
				t.Fatalf("resvgMemLimit() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestRunResvgByteIdentical guards that the bounded wrapper changes nothing
// about a normal render: the PNG is byte-for-byte what resvg writes when run
// directly with the same arguments.
func TestRunResvgByteIdentical(t *testing.T) {
	t.Setenv("AEYE_RESVG", "")
	if _, err := exec.LookPath("resvg"); err != nil {
		t.Skip("resvg not on PATH")
	}
	dir := t.TempDir()
	sources := map[string]string{
		"flowchart.d2": "classes: {store: {shape: cylinder; style.fill: \"#cde\"}}\nclient -> api: request\napi -> db: write\ndb: Orders {class: store}\ndb -> cache: replicate\ncache: {class: store}\n",
		"text.d2":      "note: |md\n  # Title\n  some *marked up* text\n|\nnote -> other\n",
	}
	svgs := map[string][]byte{}
	for name, src := range sources {
		in := filepath.Join(dir, name)
		if err := os.WriteFile(in, []byte(src), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
			t.Fatal(err)
		}
		svg, err := renderD2SVG(in)
		if err != nil {
			t.Fatalf("compile %s: %v", name, err)
		}
		svgs[name] = fixFonts(svg)
	}
	fixture, err := os.ReadFile(filepath.Join("tests", "fixtures", "regions-plain.svg"))
	if err != nil {
		t.Fatal(err)
	}
	svgs["regions-plain.svg"] = fixture

	for name, svg := range svgs {
		t.Run(name, func(t *testing.T) {
			in := filepath.Join(dir, name+".svg")
			if err := os.WriteFile(in, svg, 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
				t.Fatal(err)
			}
			sizeArgs, err := rasterSizeArgs(svg)
			if err != nil {
				t.Fatal(err)
			}
			args := append(sizeArgs, in)
			viaHelper, direct := filepath.Join(dir, name+"-helper.png"), filepath.Join(dir, name+"-direct.png")
			if _, err := runResvg(context.Background(), nil, append(slices.Clone(args), viaHelper)...); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("resvg", append(append(resvgFontArgs(), args...), direct)...) //nolint:gosec // args built by the test, not shell-interpreted
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("direct resvg: %v: %s", err, b)
			}
			got, err := os.ReadFile(viaHelper) //nolint:gosec // test temp path
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(direct) //nolint:gosec // test temp path
			if err != nil {
				t.Fatal(err)
			}
			if len(got) == 0 || !bytes.Equal(got, want) {
				t.Fatalf("bounded render differs from direct resvg (%d vs %d bytes)", len(got), len(want))
			}
		})
	}
}
