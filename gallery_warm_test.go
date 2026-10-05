package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// runCmd runs cmd and returns the messages it produces, flattening batches.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, runCmd(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// coldCache points the thumbnail cache at an empty private dir for the test.
func coldCache(t *testing.T) {
	t.Helper()
	old := imgCacheDir
	imgCacheDir = t.TempDir()
	t.Cleanup(func() { imgCacheDir = old })
}

// fastWarmDebounce shortens the resize debounce so tests wait milliseconds.
func fastWarmDebounce(t *testing.T) {
	t.Helper()
	old := warmDebounce
	warmDebounce = 20 * time.Millisecond
	t.Cleanup(func() { warmDebounce = old })
}

// pump delivers msgs to m and runs every Cmd that results, until none are left.
// It keeps only the cache fill messages' follow-ups moving; the rest of the
// program's timers aren't part of what these tests look at.
func pump(m galleryModel, msgs ...tea.Msg) galleryModel {
	for len(msgs) > 0 {
		next, cmd := m.Update(msgs[0])
		m = next.(galleryModel)
		msgs = append(msgs[1:], runCmd(cmd)...)
	}
	return m
}

// msgOf returns the first message of type T among msgs.
func msgOf[T any](msgs []tea.Msg) (T, bool) {
	for _, msg := range msgs {
		if v, ok := msg.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// runFill takes the cacheKickMsg among cmd's results through Update and runs the
// fill it starts, returning its result without delivering it.
func runFill(t *testing.T, m *galleryModel, cmd tea.Cmd) cacheFilledMsg {
	t.Helper()
	kick, ok := msgOf[cacheKickMsg](runCmd(cmd))
	if !ok {
		t.Fatal("no fill was armed")
	}
	next, cmd := m.Update(kick)
	*m = next.(galleryModel)
	filled, ok := msgOf[cacheFilledMsg](runCmd(cmd))
	if !ok {
		t.Fatal("the fill produced no result")
	}
	return filled
}

func cacheFiles(t *testing.T) map[string]bool {
	t.Helper()
	ents, err := os.ReadDir(imgCacheDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, e := range ents {
		out[filepath.Join(imgCacheDir, e.Name())] = true
	}
	return out
}

// newColdModel is a ready kitty model of n distinct images whose thumbnails are
// all cold, as after a resize.
func newColdModel(t *testing.T, rec *transmitRecorder, n int) *galleryModel {
	t.Helper()
	t.Setenv("TMUX", "") // keeps the APCs unwrapped for storesFor
	m := newTransmitModel(t, rec, n)
	for i := range m.images {
		m.images[i].Path = writeSizedPNG(t, 600+i, 400)
	}
	m.crop = fullCrop()
	coldCache(t)
	return m
}

func TestColdSwitchDoesNotTranscodeOnLoop(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newColdModel(t, rec, 2)
	cachedPNG(imagePath(m, 0), m.l.previewW, m.l.previewH)
	cachedPNG(imagePath(m, 0), m.l.stripW, m.l.stripH)
	warm := cacheFiles(t)
	before := rec.Len()

	next, cmd := m.Update(tea.KeyPressMsg{Text: "l", Code: 'l'})
	got := next.(galleryModel)

	if got.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", got.cursor)
	}
	if files := cacheFiles(t); len(files) != len(warm) {
		t.Errorf("switching to a cold image wrote %d cache files on the loop", len(files)-len(warm))
	}
	if n := len(storesFor(t, added(rec, before), got.previewID())); n != 0 {
		t.Errorf("preview stored %d times before its thumbnail existed", n)
	}
	if cmd == nil {
		t.Fatal("no fill was armed for the cold thumbnail")
	}

	before = rec.Len()
	got = pump(got, runCmd(cmd)...)

	stores := storesFor(t, added(rec, before), got.previewID())
	if len(stores) == 0 {
		t.Fatal("preview not stored once the fill landed")
	}
	want, _ := pngCacheName(imagePath(&got, 1), got.l.previewW, got.l.previewH)
	if stores[len(stores)-1].path != want {
		t.Errorf("preview stored %q, want its cached thumbnail %q", stores[len(stores)-1].path, want)
	}
}

func TestResizeStormTranscodesOnlySettledSize(t *testing.T) {
	old := warmDebounce
	warmDebounce = 100 * time.Millisecond
	t.Cleanup(func() { warmDebounce = old })
	rec := newTransmitRecorder(t)
	m := newColdModel(t, rec, 3)
	*m = pump(*m, tea.WindowSizeMsg{Width: m.width, Height: m.height})
	coldCache(t) // the layout before the storm is stale by the time it ends

	// Cmds run in real time, as the runtime would: a kick that isn't debounced
	// arrives while the storm is still going and starts a transcode for its size.
	msgs := make(chan tea.Msg, 1024)
	run := func(cmd tea.Cmd) {
		go func() {
			for _, msg := range runCmd(cmd) {
				msgs <- msg
			}
		}()
	}
	cur := *m
	step := func(msg tea.Msg) {
		next, cmd := cur.Update(msg)
		cur = next.(galleryModel)
		run(cmd)
	}
	deliver := func(wait time.Duration) bool {
		select {
		case msg := <-msgs:
			next, cmd := cur.Update(msg)
			cur = next.(galleryModel)
			run(cmd)
			return true
		case <-time.After(wait):
			return false
		}
	}
	const steps = 12
	for i := range steps {
		step(tea.WindowSizeMsg{Width: 90 + i, Height: 40})
		for deliver(10 * time.Millisecond) {
		}
	}
	want := map[string]bool{}
	for _, im := range cur.images {
		for _, sz := range [][2]int{{cur.l.previewW, cur.l.previewH}, {cur.l.stripW, cur.l.stripH}} {
			out, _ := pngCacheName(im.Path, sz[0], sz[1])
			want[out] = true
		}
	}
	// Wait for the settled size to be warmed, then a little longer for a
	// transcode that shouldn't be there to show up.
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		got := cacheFiles(t)
		if len(got) >= len(want) {
			break
		}
		deliver(50 * time.Millisecond)
	}
	for deliver(150 * time.Millisecond) {
	}

	got := cacheFiles(t)
	for f := range got {
		if !want[f] {
			t.Errorf("transcoded a thumbnail for a size the resize did not settle on: %s", f)
		}
	}
	for f := range want {
		if !got[f] {
			t.Errorf("settled-size thumbnail %s was not warmed", f)
		}
	}
}

func TestLateFillForSupersededSelectionIgnored(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newColdModel(t, rec, 3)
	cachedPNG(imagePath(m, 0), m.l.previewW, m.l.previewH)
	cachedPNG(imagePath(m, 0), m.l.stripW, m.l.stripH)

	next, cmd := m.Update(tea.KeyPressMsg{Text: "l", Code: 'l'})
	cur := next.(galleryModel)
	filled := runFill(t, &cur, cmd)

	// The selection moves back to the warm image before the fill lands.
	viewBefore := rec.Len()
	next, _ = cur.Update(tea.KeyPressMsg{Text: "h", Code: 'h'})
	cur = next.(galleryModel)
	wantA, _ := pngCacheName(imagePath(&cur, 0), cur.l.previewW, cur.l.previewH)
	if st := storesFor(t, added(rec, viewBefore), cur.previewID()); len(st) == 0 || st[len(st)-1].path != wantA {
		t.Errorf("returning to the cached image left its preview unstored: %v", st)
	}
	before := rec.Len()
	next, cmd = cur.Update(filled)
	cur = next.(galleryModel)

	if rec.Len() != before {
		t.Error("a fill for a superseded selection re-stored the view")
	}
	pump(cur, runCmd(cmd)...)
	if rec.Len() != before {
		t.Error("warming after a superseded fill re-stored the view")
	}
}

func TestLateFillForSupersededSizeIgnored(t *testing.T) {
	fastWarmDebounce(t)
	rec := newTransmitRecorder(t)
	m := newColdModel(t, rec, 2)
	*m = pump(*m, tea.WindowSizeMsg{Width: m.width, Height: m.height})

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 90, Height: 40})
	cur := next.(galleryModel)
	filled := runFill(t, &cur, cmd)

	// The pane is resized again while that fill was running.
	next, _ = cur.Update(tea.WindowSizeMsg{Width: 70, Height: 40})
	cur = next.(galleryModel)
	before := rec.Len()
	next, cmd = cur.Update(filled)
	cur = next.(galleryModel)

	if rec.Len() != before {
		t.Error("a fill for a superseded size re-stored the view")
	}
	if cmd != nil {
		t.Error("a fill for a superseded size went on to warm the rest")
	}
}

func TestUndecodableSourceIsNotRefilled(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newColdModel(t, rec, 1)
	bad := filepath.Join(t.TempDir(), "bad.png")
	if err := os.WriteFile(bad, []byte("not a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	setImagePath(m, 0, bad)

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 90, Height: 40})
	cur := pump(next.(galleryModel), runCmd(cmd)...)

	if cur.cacheBusy {
		t.Error("fill still pending after the source failed to transcode")
	}
	cur.lastTransmitSig = nil
	cur.transmitView()
	if cur.cacheMiss {
		t.Error("a source that failed to transcode keeps asking for a fill")
	}
}

// A layout the fill never reached must not leave the view blank when the pane
// returns to a layout whose thumbnails are cached: the cold transmit in between
// cleared the store.
func TestResizeAndBackBeforeFillRestoresView(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newColdModel(t, rec, 2)
	warmThumbs(m)
	m.transmitView()
	cur := *m

	next, _ := cur.Update(tea.WindowSizeMsg{Width: 90, Height: 40})
	cur = next.(galleryModel)
	before := rec.Len()
	next, _ = cur.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	cur = next.(galleryModel)

	if st := storesFor(t, added(rec, before), cur.previewID()); len(st) == 0 {
		t.Error("view left blank after resizing back to a cached layout")
	}
}

// A miss still records the preview id, so the next transmit deletes whatever
// other paths (vector, crop) stored under it meanwhile.
func TestMissedPreviewIDStillCleared(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newColdModel(t, rec, 1)
	m.transmitView()
	for _, id := range m.storedIDs {
		if id == m.previewID() {
			return
		}
	}
	t.Errorf("storedIDs %v lack the preview id %d", m.storedIDs, m.previewID())
}

// On raster a resize raises no miss itself; the repaint 50ms later does. That
// miss must join the pending debounced fill, not start an undebounced one.
func TestRasterPaintMissJoinsPendingFill(t *testing.T) {
	fastWarmDebounce(t)
	rec := newTransmitRecorder(t)
	m := newColdModel(t, rec, 2)
	m.backend = backendRaster
	*m = pump(*m, tea.WindowSizeMsg{Width: m.width, Height: m.height})

	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 40})
	cur := next.(galleryModel)
	gen := cur.cacheGen
	next, _ = cur.Update(rasterPaintMsg{gen: cur.rasterGen})
	cur = next.(galleryModel)

	if cur.cacheGen != gen {
		t.Errorf("a paint miss superseded the debounced fill (gen %d -> %d)", gen, cur.cacheGen)
	}
	if !cur.cacheBusy {
		t.Error("the pending fill won't re-paint for the miss that joined it")
	}
}

// The image list can empty while a fill runs (a foreign or unreadable manifest,
// the last image deleted); the fill landing must not index it.
func TestFillLandingOnEmptyListDoesNotPanic(t *testing.T) {
	rec := newTransmitRecorder(t)
	m := newColdModel(t, rec, 2)
	cachedPNG(imagePath(m, 0), m.l.previewW, m.l.previewH)
	cachedPNG(imagePath(m, 0), m.l.stripW, m.l.stripH)
	next, cmd := m.Update(tea.KeyPressMsg{Text: "l", Code: 'l'})
	cur := next.(galleryModel)
	filled := runFill(t, &cur, cmd)

	cur.images = nil
	cur.Update(filled)
}

// imagePath and setImagePath range rather than index so the nil-flow analysis
// sees an images slice that loadManifest may have left empty.
func imagePath(m *galleryModel, i int) string {
	for j, im := range m.images {
		if j == i {
			return im.Path
		}
	}
	return ""
}

func setImagePath(m *galleryModel, i int, p string) {
	for j := range m.images {
		if j == i {
			m.images[j].Path = p
		}
	}
}
