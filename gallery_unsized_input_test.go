package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// newUnsizedModel builds a model in the state a freshly-spawned pane is in
// before its first (real) WindowSizeMsg: curImg eagerly decoded, but layout
// still zero (previewW == previewH == 0) and ready == false.
func newUnsizedModel(t *testing.T) galleryModel {
	t.Helper()
	dir := t.TempDir()
	img := filepath.Join(dir, "fixture.png")
	f, err := os.Create(img)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 40, 30))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	tty, err := os.CreateTemp(dir, "tty")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tty.Close() })
	m := galleryModel{
		pane:    "test",
		images:  []imageEntry{{Path: img}},
		backend: backendKitty,
		tty:     tty,
		crop:    fullCrop(),
	}
	m.ensureDecoded() // mirrors runGallery: curImg is non-nil before first size
	if m.curImg == nil {
		t.Fatal("curImg not decoded")
	}
	return m
}

// Zooming before the pane is sized must be a no-op, not a panic. Under a 0x0
// first WindowSizeMsg (tmux 3.8) the layout is zero; acting on the zoom key
// would feed 0/0 into boxAspectFrac (NaN crop → image.NewRGBA panic).
func TestZoomKeyIgnoredWhileUnsized(t *testing.T) {
	m := newUnsizedModel(t)
	out, _ := m.Update(tea.KeyPressMsg{Text: "z", Code: 'z'})
	got := out.(galleryModel)
	if !got.crop.isFull() {
		t.Fatalf("unsized zoom mutated crop: %+v", got.crop)
	}
	if got.ready {
		t.Fatal("model became ready without a size")
	}
}

// The gate must not over-block: once a real size arrives, zoom works again.
func TestZoomKeyWorksAfterSizing(t *testing.T) {
	m := newUnsizedModel(t)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m = sized.(galleryModel)
	if !m.ready {
		t.Fatal("model not ready after a real WindowSizeMsg")
	}
	out, _ := m.Update(tea.KeyPressMsg{Text: "z", Code: 'z'})
	if out.(galleryModel).crop.isFull() {
		t.Fatal("zoom after sizing left the crop full (did not zoom)")
	}
}
