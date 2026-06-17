package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
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

func TestRenderD2SVG(t *testing.T) {
	src := filepath.Join(t.TempDir(), "d.d2")
	if err := os.WriteFile(src, []byte("a -> b -> c\n"), 0o644); err != nil {
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
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
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
