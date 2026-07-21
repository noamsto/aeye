package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A capture still being flushed can have a valid PNG header but truncated pixel
// data. decodeErr must reject it (full decode, not just the header) so the
// carousel drops it instead of painting a blank cell.
func TestDecodeErrTruncatedFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.png")
	writeTestImage(t, good, 16, 16)
	if err := decodeErr(good); err != nil {
		t.Fatalf("complete PNG should decode, got %v", err)
	}

	// Keep the signature + IHDR (DecodeConfig would still accept this) but drop
	// the IDAT/IEND so a full decode fails.
	full, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	trunc := filepath.Join(dir, "trunc.png")
	if err := os.WriteFile(trunc, full[:40], 0o644); err != nil {
		t.Fatal(err)
	}
	if decodeErr(trunc) == nil {
		t.Error("truncated PNG should fail the full decode, but decodeErr returned nil")
	}
}

// A manifest whose .owner sidecar names a different session than the viewer's
// identity is a prior session's images left under a reused pane id — loadManifest
// must return nothing rather than bleed them in.
func TestLoadManifestRefusesForeignOwner(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AEYE_DIR", dir)
	t.Setenv("AEYE_OWNER", "session-A")
	imagesDir := filepath.Join(dir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	img := filepath.Join(dir, "shot.png")
	writeTestImage(t, img, 8, 8)
	manifest := filepath.Join(imagesDir, "p9.jsonl")
	if err := os.WriteFile(manifest, []byte(`{"type":"image","path":"`+img+`","source":"Read","mtime":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	owner := filepath.Join(imagesDir, "p9.owner")

	if err := os.WriteFile(owner, []byte("session-B"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadManifest("p9", "dark"); got != nil {
		t.Errorf("foreign-owned manifest should yield no images, got %d", len(got))
	}

	if err := os.WriteFile(owner, []byte("session-A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadManifest("p9", "dark"); len(got) != 1 {
		t.Errorf("own manifest should load, got %d images", len(got))
	}

	// No sidecar at all: the check is inert, so the manifest still loads.
	if err := os.Remove(owner); err != nil {
		t.Fatal(err)
	}
	if got := loadManifest("p9", "dark"); len(got) != 1 {
		t.Errorf("ownerless manifest should load, got %d images", len(got))
	}
}

// An intermittent bleed can only be attributed after the fact if the viewer
// records why a load was accepted or rejected. With AEYE_DEBUG on, loadManifest
// must trace the manifest key, this viewer's identity, the manifest's stamped
// owner, and whether they disagreed.
func TestLoadManifestTracesOwnerDecision(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AEYE_DIR", dir)
	t.Setenv("AEYE_OWNER", "session-A")
	imagesDir := filepath.Join(dir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	img := filepath.Join(dir, "shot.png")
	writeTestImage(t, img, 8, 8)
	if err := os.WriteFile(filepath.Join(imagesDir, "p9.jsonl"),
		[]byte(`{"type":"image","path":"`+img+`","source":"Read","mtime":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imagesDir, "p9.owner"), []byte("session-B"), 0o644); err != nil {
		t.Fatal(err)
	}

	tracePath := filepath.Join(dir, "trace.log")
	t.Setenv("AEYE_DEBUG", tracePath)
	resetTrace()
	defer resetTrace()
	traceInit("p9")

	loadManifest("p9", "dark")
	traceFile.Sync()

	b, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{"key=p9", `self="session-A"`, `owner="session-B"`, "foreign=true"} {
		if !strings.Contains(got, want) {
			t.Errorf("trace missing %q; got:\n%s", want, got)
		}
	}
}
