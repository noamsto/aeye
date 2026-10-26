package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/noamsto/themestate"

	tea "charm.land/bubbletea/v2"
)

type kittyOp struct {
	op string // "T" store, "d" delete
	id int
}

func kittyOps(buf []byte) []kittyOp {
	var out []kittyOp
	for _, body := range apcBodies(buf) {
		keys, _ := apcKeys(body)
		id, _ := strconv.Atoi(keys["i"])
		switch keys["a"] {
		case "T", "d":
			out = append(out, kittyOp{keys["a"], id})
		}
	}
	return out
}

// refreshFixture is a ready kitty carousel over a real manifest of n images,
// already transmitted once, with the recorder positioned after that first store.
type refreshFixture struct {
	t      *testing.T
	m      *galleryModel
	rec    *transmitRecorder
	paths  []string
	before int
}

func newRefreshFixture(t *testing.T, n int) *refreshFixture {
	t.Helper()
	t.Setenv("TMUX", "")
	dir := t.TempDir()
	t.Setenv("AEYE_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0o755); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	f := &refreshFixture{t: t, rec: newTransmitRecorder(t)}
	for i := range n {
		p := filepath.Join(dir, "img"+strconv.Itoa(i)+".png")
		writeTestImage(t, p, 4+i, 4)
		f.paths = append(f.paths, p)
	}
	f.writeManifest(nil)
	f.m = &galleryModel{pane: "p1", backend: backendKitty, tty: f.rec.f, ready: true, width: 100, height: 40}
	f.m.l = computeLayout(f.m.width, f.m.height)
	f.m.reload()
	warmThumbs(f.m)
	f.m.lastTransmitSig = nil
	f.m.transmitView()
	f.before = f.rec.Len()
	return f
}

func (f *refreshFixture) writeManifest(mtimes map[int]int64) {
	f.t.Helper()
	var s string
	for i, p := range f.paths {
		mt := int64(1)
		if v, ok := mtimes[i]; ok {
			mt = v
		}
		s += `{"type":"image","path":"` + p + `","mtime":` + strconv.FormatInt(mt, 10) + "}\n"
	}
	if err := os.WriteFile(manifestPath("p1"), []byte(s), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// rewrite changes entry i's pixels and capture mtime, as a re-rendered diagram would.
func (f *refreshFixture) rewrite(i int) {
	f.t.Helper()
	writeTestImage(f.t, f.paths[i], 30+i, 9)
	f.writeManifest(map[int]int64{i: 99})
}

func (f *refreshFixture) reload() []kittyOp {
	f.t.Helper()
	warmThumbs(f.m) // the cache fill has landed for the rewritten file
	f.m.reload()
	ops := kittyOps(added(f.rec, f.before))
	f.before = f.rec.Len()
	return ops
}

// AC2: rewriting one non-selected visible entry re-sends only that entry, and
// nothing live is deleted before its replacement is stored.
func TestRefreshRewritesOnlyChangedEntry(t *testing.T) {
	f := newRefreshFixture(t, 5)
	liveBefore := map[int]bool{}
	for _, id := range f.m.storedIDs {
		liveBefore[id] = true
	}
	f.rewrite(3)
	ops := f.reload()

	var stores []int
	for _, o := range ops {
		if o.op == "T" {
			stores = append(stores, o.id)
		}
	}
	if len(stores) != 1 {
		t.Fatalf("stores = %v (ops %v), want exactly the rewritten entry's thumb", stores, ops)
	}
	if liveBefore[stores[0]] {
		t.Errorf("replacement stored under live id %d; want the alternate id so the old image stays up", stores[0])
	}
	for _, o := range ops {
		if o.op == "d" && liveBefore[o.id] {
			t.Errorf("live id %d deleted during refresh (ops %v)", o.id, ops)
		}
	}
	if f.m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", f.m.cursor)
	}
}

// Stale ids are reaped by the next transmit, never while still displayed.
func TestRefreshReapsReplacedIDLater(t *testing.T) {
	f := newRefreshFixture(t, 5)
	old := f.m.stripID(3)
	f.rewrite(3)
	f.reload()
	if f.m.stripID(3) == old {
		t.Fatal("slot kept its id; no double buffering")
	}
	f.m.cursor = 1 // slot 3's content is unchanged, so nothing re-stores over old
	ops := f.reload()
	for _, o := range ops {
		if o.op == "d" && o.id == old {
			return
		}
	}
	t.Errorf("replaced id %d was never reaped (ops %v)", old, ops)
}

// AC3: a manifest touch that changes nothing visible writes nothing and clears nothing.
func TestRefreshNoVisibleChangeWritesNothing(t *testing.T) {
	f := newRefreshFixture(t, 5)
	f.writeManifest(nil) // new manifest mtime, same content
	if ops := f.reload(); len(ops) != 0 {
		t.Fatalf("unchanged reload wrote %v", ops)
	}
	f.writeManifest(nil)
	f.m.tick = &tickSnapshot{}
	f.m.theme, f.m.visible = themeNow(), paneVisible()
	_, cmd := f.m.Update(galleryTickMsg{})
	for _, msg := range runCmdBounded(cmd) {
		if fmt.Sprintf("%T", msg) == fmt.Sprintf("%T", tea.ClearScreen()) {
			t.Error("refresh issued a screen clear")
		}
	}
	if ops := kittyOps(added(f.rec, f.before)); len(ops) != 0 {
		t.Errorf("tick reload wrote %v", ops)
	}
}

func themeNow() string { return themestate.Detect() }

// runCmdBounded runs cmd's batch members, abandoning any that blocks (the
// manifest poller sleeps until something changes).
func runCmdBounded(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msgs := make(chan tea.Msg, 16)
	run := func(c tea.Cmd) {
		if c != nil {
			msgs <- c()
		}
	}
	top := cmd()
	if b, ok := top.(tea.BatchMsg); ok {
		for _, c := range b {
			go run(c)
		}
	} else {
		msgs <- top
	}
	var out []tea.Msg
	for {
		select {
		case m := <-msgs:
			out = append(out, m)
		case <-time.After(300 * time.Millisecond):
			return out
		}
	}
}

// AC4: a background rewrite never moves the selection; pinned still follows a new image.
func TestRefreshKeepsCursorOnBackgroundRewrite(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		f := newRefreshFixture(t, 5)
		f.m.cursor, f.m.pinned = 4, pinned
		if !pinned {
			f.m.cursor = 1
		}
		want := f.m.cursor
		f.rewrite(3)
		f.reload()
		if f.m.cursor != want {
			t.Errorf("pinned=%v: cursor = %d, want %d", pinned, f.m.cursor, want)
		}
	}
}

func TestPinnedStillFollowsNewImage(t *testing.T) {
	f := newRefreshFixture(t, 3)
	f.m.pinned, f.m.cursor = true, 2
	p := filepath.Join(filepath.Dir(f.paths[0]), "new.png")
	writeTestImage(t, p, 8, 8)
	f.paths = append(f.paths, p)
	f.writeManifest(nil)
	f.reload()
	if f.m.cursor != 3 {
		t.Errorf("cursor = %d, want 3 (newest)", f.m.cursor)
	}
}

func TestAlternateIDsStayInPaneBlock(t *testing.T) {
	m := &galleryModel{pane: "%7"}
	base := paneImageIDBase("%7")
	m.l = computeLayout(5000, 40)
	for slot := range 2 + m.l.stripCols {
		for bit := range 2 {
			if id := m.slotIDFor(slot, bit); id < base || id > base+maxCellDim {
				t.Errorf("slot %d bit %d id %d outside block %d..%d", slot, bit, id, base, base+maxCellDim)
			}
		}
	}
}

// The hook re-appends a re-rendered diagram, so a background rewrite of an old
// one becomes the newest entry. A pinned viewer must not treat that as a new
// image and jump to it.
func TestPinnedIgnoresReappendedDiagram(t *testing.T) {
	dir := cropDir(t)
	const hashC, hashA2 = "1111111111111111", "2222222222222222"
	a := diagramPairNamed(t, dir, "a", hashA, sizeA)
	b := diagramPairNamed(t, dir, "b", hashB, sizeB)
	c := diagramPairNamed(t, dir, "c", hashC, sizeA)
	m := newDecodeModel(t, sizeA)
	m.theme, m.pinned = "dark", true
	writeCropManifest(t, m.pane, a, b, c)
	m.reload()
	if len(m.images) != 3 || m.images[m.cursor].Name != "c" {
		t.Fatalf("setup: pinned cursor on %q, want c", m.images[m.cursor].Name)
	}

	writeCropManifest(t, m.pane, b, c, diagramPairNamed(t, dir, "a", hashA2, sizeA))
	m.reload()
	if len(m.images) != 3 {
		t.Fatalf("reload kept %d images, want 3", len(m.images))
	}
	if got := m.images[m.cursor].Name; got != "c" {
		t.Errorf("pinned cursor jumped to %q after a background rewrite, want c", got)
	}
}
