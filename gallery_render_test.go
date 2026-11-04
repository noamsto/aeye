package main

import (
	"encoding/base64"
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
	b, err := os.ReadFile(out) //nolint:gosec // test fixture path
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

func TestAnimEscapes(t *testing.T) {
	t.Setenv("TMUX", "")
	b64 := base64.StdEncoding.EncodeToString([]byte("/tmp/b"))

	t.Run("partial frame", func(t *testing.T) {
		got := animNewFrame(7, animRect{X: 16, Y: 32, W: 48, H: 16, Off: 0, Size: 3072}, "/tmp/b", false, 100)
		want := "\x1b_Ga=f,i=7,q=2,c=1,X=1,x=16,y=32,s=48,v=16,f=32,t=f,O=0,S=3072,z=100;" + b64 + "\x1b\\"
		if got != want {
			t.Errorf("animNewFrame partial =\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("full frame", func(t *testing.T) {
		got := animNewFrame(7, animRect{W: 64, H: 48, Size: 12288}, "/tmp/b", true, 100)
		want := "\x1b_Ga=f,i=7,q=2,s=64,v=48,f=32,t=f,O=0,S=12288,z=100;" + b64 + "\x1b\\"
		if got != want {
			t.Errorf("animNewFrame full =\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("edit frame", func(t *testing.T) {
		got := animEditFrame(7, 3, animRect{X: 0, Y: 0, W: 16, H: 16, Off: 3072, Size: 1024}, "/tmp/b")
		want := "\x1b_Ga=f,i=7,q=2,r=3,X=1,x=0,y=0,s=16,v=16,f=32,t=f,O=3072,S=1024;" + b64 + "\x1b\\"
		if got != want {
			t.Errorf("animEditFrame =\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("start loop", func(t *testing.T) {
		got := animStartLoop(7, 100)
		want := "\x1b_Ga=a,i=7,q=2,r=1,z=100\x1b\\\x1b_Ga=a,i=7,q=2,s=3,v=1\x1b\\"
		if got != want {
			t.Errorf("animStartLoop =\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("stop", func(t *testing.T) {
		got := animStop(7)
		want := "\x1b_Ga=a,i=7,q=2,s=1\x1b\\"
		if got != want {
			t.Errorf("animStop = %q, want %q", got, want)
		}
	})

	t.Run("store", func(t *testing.T) {
		r1 := animRect{X: 0, Y: 0, W: 16, H: 16, Off: 0, Size: 1024}
		r2 := animRect{X: 16, Y: 16, W: 16, H: 16, Off: 1024, Size: 1024}
		rf := animRect{W: 64, H: 48, Off: 0, Size: 12288}
		s := &animSet{
			Root: "/tmp/r",
			Blob: "/tmp/b",
			Plan: animPlan{W: 64, H: 48, Frames: []animFrame{
				{Rects: []animRect{r1, r2}},
				{Full: true, Rects: []animRect{rf}},
			}},
		}
		got := animationStore(7, s, 10, 5)
		want := transmitVirtualRaw(7, "/tmp/r", 64, 48, 10, 5) +
			animNewFrame(7, r1, "/tmp/b", false, animGapMS) +
			animEditFrame(7, 2, r2, "/tmp/b") +
			animNewFrame(7, rf, "/tmp/b", true, animGapMS) +
			animStartLoop(7, animGapMS)
		if got != want {
			t.Errorf("animationStore =\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("stop under tmux", func(t *testing.T) {
		t.Setenv("TMUX", "x")
		got := animStop(7)
		want := "\x1bPtmux;\x1b\x1b_Ga=a,i=7,q=2,s=1\x1b\x1b\\\x1b\\"
		if got != want {
			t.Errorf("animStop under tmux = %q, want %q", got, want)
		}
	})
}
