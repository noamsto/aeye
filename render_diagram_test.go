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

func TestResvgFontArgs(t *testing.T) {
	t.Run("pins the fonts dir when set", func(t *testing.T) {
		t.Setenv("AEYE_D2_FONT_DIR", "/fonts")
		want := []string{"--skip-system-fonts", "--use-fonts-dir", "/fonts"}
		got := resvgFontArgs()
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	})
	t.Run("empty when unset, so resvg uses system fonts", func(t *testing.T) {
		t.Setenv("AEYE_D2_FONT_DIR", "")
		if got := resvgFontArgs(); got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
}

func TestD2ThemeID(t *testing.T) {
	// Point detectTheme at a scratch state dir so the test controls the mode.
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	writeTheme := func(theme string) {
		if err := os.WriteFile(filepath.Join(state, "theme-state.json"),
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
