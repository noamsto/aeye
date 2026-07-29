package main

import (
	"image"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestFilesToDeletePlain(t *testing.T) {
	e := imageEntry{Path: "/shots/login.png", Source: "Read"}
	got := e.filesToDelete()
	if len(got) != 1 || got[0] != "/shots/login.png" {
		t.Fatalf("filesToDelete() = %v, want [/shots/login.png]", got)
	}
}

func TestFilesToDeleteD2Cluster(t *testing.T) {
	e := imageEntry{
		Path:   "/d/hash-dark.png",
		Vector: "/d/hash-dark.svg",
		Source: "d2",
	}
	got := e.filesToDelete()
	sort.Strings(got)
	want := []string{
		"/d/hash-dark.png", "/d/hash-dark.svg",
		"/d/hash-light.png", "/d/hash-light.svg",
	}
	if len(got) != len(want) {
		t.Fatalf("filesToDelete() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("filesToDelete() = %v, want %v", got, want)
		}
	}
}

func TestFilesToDeleteD2NoVector(t *testing.T) {
	e := imageEntry{Path: "/d/hash-dark.png", Source: "d2"}
	got := e.filesToDelete()
	if len(got) != 2 {
		t.Fatalf("filesToDelete() = %v, want the two png variants only", got)
	}
}

func TestIsD2RenderArtifact(t *testing.T) {
	dir := "/state/images/diagrams"
	for _, path := range []string{
		dir + "/0123456789abcdef-light.png",
		dir + "/ABCDEF0123456789-dark.svg",
	} {
		if !isD2RenderArtifact(path, dir) {
			t.Errorf("isD2RenderArtifact(%q) = false, want true", path)
		}
	}
	for _, path := range []string{
		dir + "/preview-light.png",
		dir + "/0123456789abcdef.png",
		dir + "/nested/0123456789abcdef-light.png",
		dir + "-backup/0123456789abcdef-light.png",
	} {
		if isD2RenderArtifact(path, dir) {
			t.Errorf("isD2RenderArtifact(%q) = true, want false", path)
		}
	}
}

func TestTransmitVirtualRawDeclaresPixelSize(t *testing.T) {
	t.Setenv("TMUX", "") // assert raw escapes, not the DCS wrapper
	// kitty cannot infer dimensions from raw pixels, so s/v are mandatory; f=32 is
	// RGBA. base64 of "/x.raw" is "L3gucmF3".
	got := transmitVirtualRaw(7, "/x.raw", 40, 20, 20, 10)
	want := "\x1b_Gi=7,a=T,U=1,q=2,f=32,s=40,v=20,c=20,r=10,t=f;L3gucmF3\x1b\\"
	if got != want {
		t.Errorf("transmitVirtualRaw = %q, want %q", got, want)
	}
}

func TestWriteRawDumpsTightlyPackedPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 5, 3))
	out := filepath.Join(t.TempDir(), "f.raw")
	if got := writeRaw(out, img); got != out {
		t.Fatalf("writeRaw = %q, want %q", got, out)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 5*3*4 {
		t.Errorf("wrote %d bytes, want %d (w*h*4)", len(b), 5*3*4)
	}
}

func TestWriteRawRefusesASubImage(t *testing.T) {
	// A sub-image's Pix is not tightly packed, so dumping it would shear the frame.
	full := image.NewRGBA(image.Rect(0, 0, 8, 8))
	sub := full.SubImage(image.Rect(0, 0, 4, 4)).(*image.RGBA)
	if got := writeRaw(filepath.Join(t.TempDir(), "f.raw"), sub); got != "" {
		t.Errorf("writeRaw accepted a sub-image (%q); it must refuse", got)
	}
}
