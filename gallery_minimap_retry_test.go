package main

import (
	"os"
	"testing"
	"time"
)

// zoomedMinimapModel is a kitty model zoomed in, its overlay stored once.
func zoomedMinimapModel(t *testing.T) (*galleryModel, *transmitRecorder) {
	t.Helper()
	rec := newTransmitRecorder(t)
	m := newMinimapModel(t, rec)
	m.transmitView()
	zoomIn(m)
	if m.crop.isFull() {
		t.Fatal("zoomIn left the crop full")
	}
	return m, rec
}

// thumbOf is the cache file the selected slot's minimap is built from.
func thumbOf(t *testing.T, m *galleryModel) string {
	t.Helper()
	if m.cursor >= len(m.images) {
		t.Fatal("cursor past the images")
	}
	out, ok := pngCacheName(m.images[m.cursor].Path, m.l.stripW, m.l.stripH)
	if !ok {
		t.Fatal("selected image has no cache name")
	}
	return out
}

// breakThumb overwrites the selected thumb with undecodable bytes and clears the
// model's memory of its overlay so the next sync has to rebuild it.
func breakThumb(t *testing.T, m *galleryModel, rec *transmitRecorder) (good []byte, mtime time.Time) {
	t.Helper()
	p := thumbOf(t, m)
	good, err := os.ReadFile(p) //nolint:gosec // test cache file
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.mini.stored, m.mini.baseKey = minimapStamp{}, minimapBaseKey{}
	before := rec.Len()
	m.syncMinimap()
	assertPlain(t, onlyStore(t, added(rec, before), selectedSlotID(m)), m)
	return good, fi.ModTime()
}

// A thumb that failed to decode and is later rewritten (same base key) gets its
// overlay back on the next sync.
func TestMinimapRecoversWhenThumbRewritten(t *testing.T) {
	m, rec := zoomedMinimapModel(t)
	good, mtime := breakThumb(t, m, rec)

	p := thumbOf(t, m)
	if err := os.WriteFile(p, good, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime.Add(time.Hour), mtime.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	before := rec.Len()
	m.syncMinimap()
	assertOverlay(t, m, onlyStore(t, added(rec, before), selectedSlotID(m)))
}

// A permanently undecodable thumb is decoded once, however many frames follow,
// through both the kitty path and the raster path.
func TestMinimapPermanentFailureNotRedecoded(t *testing.T) {
	m, rec := zoomedMinimapModel(t)
	breakThumb(t, m, rec)

	decodes := 0
	minimapDecodeHook = func() { decodes++ }
	t.Cleanup(func() { minimapDecodeHook = nil })

	for range 20 {
		m.panBy(0.01, 0)
		m.syncMinimap()
		w := m.minimapWant()
		w.overlay, w.fit = true, minimapFit(m.curSize, m.minimapBox())
		w.rect = minimapRect(w.fit, m.crop)
		if m.minimapFrame(w) != nil {
			t.Fatal("frame built from an undecodable thumb")
		}
	}
	if decodes != 0 {
		t.Errorf("undecodable thumb re-decoded %d times after the first failure", decodes)
	}
}

// An access failure that passes without the file changing recovers after the
// backoff, and not before it.
func TestMinimapRecoversFromTransientOpenFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	m, rec := zoomedMinimapModel(t)
	minimapRetryDelay = func(int) time.Duration { return 40 * time.Millisecond }
	t.Cleanup(func() { minimapRetryDelay = cacheWriteRetryDelay })

	p := thumbOf(t, m)
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	m.mini.stored, m.mini.baseKey = minimapStamp{}, minimapBaseKey{}
	before := rec.Len()
	m.syncMinimap()
	assertPlain(t, onlyStore(t, added(rec, before), selectedSlotID(m)), m)

	if err := os.Chmod(p, 0o600); err != nil { // readable again; size and mtime untouched
		t.Fatal(err)
	}
	decodes := 0
	minimapDecodeHook = func() { decodes++ }
	t.Cleanup(func() { minimapDecodeHook = nil })

	m.mini.stored = minimapStamp{}
	for range 5 {
		m.syncMinimap()
	}
	if decodes != 0 {
		t.Errorf("retried %d times inside the backoff, want 0", decodes)
	}

	time.Sleep(60 * time.Millisecond)
	before = rec.Len()
	m.mini.stored = minimapStamp{}
	m.syncMinimap()
	assertOverlay(t, m, onlyStore(t, added(rec, before), selectedSlotID(m)))
	if decodes != 1 {
		t.Errorf("decoded %d times after the backoff, want 1", decodes)
	}
}
