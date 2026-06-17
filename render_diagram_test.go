package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixFonts(t *testing.T) {
	t.Setenv("AEYE_D2_FONT", "") // default family
	in := []byte(`.s{font-family:"d2-123-font-regular";}` +
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
