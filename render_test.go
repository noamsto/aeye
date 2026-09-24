package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderMermaidToSVG(t *testing.T) {
	out := filepath.Join(t.TempDir(), "flow.svg")
	src := strings.NewReader("flowchart LR\n  A[Start] --> B[Done]\n")
	if err := runRender("mermaid", "-", out, src); err != nil {
		t.Fatalf("runRender: %v", err)
	}
	svg, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(svg, []byte("<svg")) || !bytes.Contains(svg, []byte("Start")) {
		t.Fatalf("not an SVG of the flowchart: %.200s", svg)
	}
	// An SVG is for a browser, which reads d2's embedded @font-face fonts —
	// the resvg font rewrite would only swap them for a system family.
	if !bytes.Contains(svg, []byte("@font-face")) {
		t.Fatal("SVG output lost its embedded fonts")
	}
}

// A Mermaid chart with no D2 analog must fail without writing out, so a caller
// such as a markdown previewer can fall back to a browser renderer.
func TestRenderUnsupportedMermaidFails(t *testing.T) {
	out := filepath.Join(t.TempDir(), "pie.png")
	src := strings.NewReader("pie title Pets\n  \"Dogs\" : 386\n")
	err := runRender("mermaid", "-", out, src)
	if err == nil || !strings.Contains(err.Error(), "pie") {
		t.Fatalf("got %v, want an unsupported-type error naming pie", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("out was written despite the failure: %v", statErr)
	}
}

func TestRenderD2FileToPNG(t *testing.T) {
	if _, err := exec.LookPath(resvgBin()); err != nil {
		t.Skip("resvg not on PATH")
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "d.d2")
	if err := os.WriteFile(in, []byte("a -> b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "d.png")
	if err := runRender("d2", in, out, nil); err != nil {
		t.Fatalf("runRender: %v", err)
	}
	png, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(png, []byte("\x89PNG")) {
		t.Fatalf("out is not a PNG: % x", png[:min(8, len(png))])
	}
}
