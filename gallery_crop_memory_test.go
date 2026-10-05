package main

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

var (
	key1    = tea.KeyPressMsg{Text: "1", Code: '1'}
	key2    = tea.KeyPressMsg{Text: "2", Code: '2'}
	keyJ    = tea.KeyPressMsg{Text: "j", Code: 'j'}
	keyOut  = tea.KeyPressMsg{Text: "Z", Code: 'Z'}
	keyEsc  = tea.KeyPressMsg{Code: tea.KeyEscape}
	keyFill = tea.KeyPressMsg{Text: "f", Code: 'f'}
)

func sendAll(t *testing.T, m galleryModel, msgs ...tea.Msg) galleryModel {
	t.Helper()
	for _, msg := range msgs {
		m, _ = send(t, m, msg)
	}
	return m
}

func TestCropRememberedPerImage(t *testing.T) {
	m := land(t, newDecodeModel(t, sizeA, sizeB))
	// Pan vertically only: after this zoom the crop still spans the full width,
	// so l/h switch images instead of panning (TestAxisFallbackNavigation). j
	// still leaves a non-centered crop for the memory assertion.
	m = sendAll(t, m, keyZoom, keyZoom, keyJ)
	cropA := m.crop
	if cropA.isFull() {
		t.Fatal("setup: A is not zoomed")
	}
	m = sendAll(t, m, key2)
	if !m.crop.isFull() {
		t.Fatalf("B opened at %+v, want fit", m.crop)
	}
	m = sendAll(t, m, keyZoom)
	cropB := m.crop
	m = sendAll(t, m, key1)
	if m.crop != cropA {
		t.Errorf("A restored as %+v, want %+v", m.crop, cropA)
	}
	m = sendAll(t, m, key2)
	if m.crop != cropB {
		t.Errorf("B restored as %+v, want %+v", m.crop, cropB)
	}
}

func TestFillCropRemembered(t *testing.T) {
	m := land(t, newDecodeModel(t, sizeA, sizeB))
	m = sendAll(t, m, keyFill)
	if m.crop.isFull() {
		t.Skip("fill is full on this geometry")
	}
	fill := m.crop
	m = sendAll(t, m, key2, key1)
	if m.crop != fill {
		t.Errorf("fill restored as %+v, want %+v", m.crop, fill)
	}
}

func TestExplicitResetForgetsCrop(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reset []tea.Msg
	}{
		{"0", []tea.Msg{keyUnzoom}},
		{"esc", []tea.Msg{keyEsc}},
		{"zoom out to fit", []tea.Msg{keyOut, keyOut}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := land(t, newDecodeModel(t, sizeA, sizeB))
			m = sendAll(t, m, keyZoom, keyZoom)
			m = sendAll(t, m, tc.reset...)
			if !m.crop.isFull() {
				t.Fatalf("setup: reset left crop %+v", m.crop)
			}
			m = sendAll(t, m, key2, key1)
			if !m.crop.isFull() {
				t.Errorf("A reopened at %+v, want fit", m.crop)
			}
		})
	}
	for _, tc := range []struct {
		name  string
		reset tea.Msg
	}{
		{"reset after a restore with 0", keyUnzoom},
		{"reset after a restore with esc", keyEsc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := land(t, newDecodeModel(t, sizeA, sizeB))
			m = sendAll(t, m, keyZoom, keyZoom, key2, key1)
			if m.crop.isFull() {
				t.Fatal("setup: crop was not restored")
			}
			m = sendAll(t, m, tc.reset, key2, key1)
			if !m.crop.isFull() {
				t.Errorf("A reopened at %+v, want fit", m.crop)
			}
		})
	}
}

func TestRestoreDuringDecodeWindowAppliesOnLand(t *testing.T) {
	m := land(t, newDecodeModel(t, sizeA, sizeB))
	m = sendAll(t, m, keyZoom)
	cropA := m.crop
	m = sendAll(t, m, key2)
	m = land(t, m)
	m = sendAll(t, m, key1)
	if m.curImg != nil || m.crop != cropA {
		t.Fatalf("pending restore: curImg=%v crop=%+v, want nil and %+v", m.curImg, m.crop, cropA)
	}
	before := ttyLen(t, m)
	os.Remove(m.zoomRawPath()) //nolint:errcheck,gosec // forcing a fresh zoom render
	m = land(t, m)
	if ttyLen(t, m) <= before {
		t.Error("landing the decode transmitted nothing")
	}
	if _, err := os.Stat(m.zoomRawPath()); err != nil {
		t.Errorf("zoom render not produced: %v", err)
	}
	if m.crop != cropA {
		t.Errorf("crop after land = %+v, want %+v", m.crop, cropA)
	}
}

func TestRememberedCropDroppedWhenImageChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, path string)
	}{
		{"different size", func(t *testing.T, path string) {
			if err := os.WriteFile(path, encode16BitPNG(t, sizeC.X, sizeC.Y, 0x7000), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"same content, newer mtime", func(t *testing.T, path string) {
			later := time.Now().Add(time.Hour)
			if err := os.Chtimes(path, later, later); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := land(t, newDecodeModel(t, sizeA, sizeB))
			m = sendAll(t, m, keyZoom, key2)
			tc.change(t, m.images[0].Path)
			m = sendAll(t, m, key1)
			if !m.crop.isFull() {
				t.Errorf("changed image reopened at %+v, want fit", m.crop)
			}
		})
	}
}

func TestPruneCropsEvictsDroppedImages(t *testing.T) {
	c := savedCrop{crop: cropFrac{0, 0, 0.5, 0.5}}
	m := &galleryModel{
		images: []imageEntry{{Path: "a"}},
		crops:  map[string]savedCrop{"a": c, "gone": c},
	}
	m.pruneCrops()
	if _, ok := m.crops["a"]; !ok || len(m.crops) != 1 {
		t.Errorf("crops = %v, want only a", m.crops)
	}
}

func TestReloadEvictsCropOfDroppedImage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AEYE_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0o755); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	a, b := filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")
	writeTestImage(t, a, 4, 4)
	writeTestImage(t, b, 4, 4)
	manifest := manifestPath("p1")
	line := func(p string) string { return `{"type":"image","path":"` + p + `","mtime":1}` + "\n" }
	if err := os.WriteFile(manifest, []byte(line(a)+line(b)), 0o600); err != nil {
		t.Fatal(err)
	}

	m := &galleryModel{pane: "p1"}
	m.reload()
	if len(m.images) != 2 {
		t.Fatalf("setup: reload = %+v, want a and b", m.images)
	}
	c := savedCrop{crop: cropFrac{0, 0, 0.5, 0.5}}
	m.crops = map[string]savedCrop{a: c, b: c}

	if err := os.WriteFile(manifest, []byte(line(b)), 0o600); err != nil {
		t.Fatal(err)
	}
	m.reload()
	if _, ok := m.crops[a]; ok {
		t.Errorf("crops = %v, want a evicted", m.crops)
	}
	if _, ok := m.crops[b]; !ok {
		t.Errorf("crops = %v, want b kept", m.crops)
	}
}

// cropDir is a temp AEYE_DIR that holds both the manifest and the image fixtures.
func cropDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AEYE_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0o755); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	return dir
}

// newManifestCropModel is a ready model over a manifest in the cropDir, so
// tests drive theme flips through the real reload path. pane is the one
// newDecodeModel derives, so its scratch cleanup stays valid.
func newManifestCropModel(t *testing.T, theme string, lines ...string) galleryModel {
	t.Helper()
	m := newDecodeModel(t, sizeA)
	m.theme = theme
	writeCropManifest(t, m.pane, lines...)
	return land(t, reloaded(m))
}

func writeCropManifest(t *testing.T, pane string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(manifestPath(pane), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// diagramPair writes <hash>-dark.png and <hash>-light.png at size and returns
// the manifest line of the d2 entry (recorded at its dark variant).
func diagramPair(t *testing.T, dir, hash string, size image.Point) string {
	t.Helper()
	for i, mode := range []string{"dark", "light"} {
		p := filepath.Join(dir, hash+"-"+mode+".png")
		if err := os.WriteFile(p, encode16BitPNG(t, size.X, size.Y, uint16(i+1)*0x3000), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return `{"type":"image","path":"` + filepath.Join(dir, hash+"-dark.png") + `","source":"d2","name":"d","mtime":1}`
}

const (
	hashA = "0123456789abcdef"
	hashB = "fedcba9876543210"
)

// reloaded runs reload the way the tick handler does, minus Update: it arms the
// decode request reload made, which Update would otherwise schedule on return.
func reloaded(m galleryModel) galleryModel {
	m.reload()
	m.armDecode()
	return m
}

func flipTheme(m galleryModel, theme string) galleryModel {
	m.theme = theme
	return reloaded(m)
}

func TestDiagramCropSurvivesThemeSwitch(t *testing.T) {
	dir := cropDir(t)
	m := newManifestCropModel(t, "dark", diagramPair(t, dir, hashA, sizeA))
	m = sendAll(t, m, keyZoom, keyZoom, keyJ)
	zoomed := m.crop
	if zoomed.isFull() {
		t.Fatal("setup: diagram is not zoomed")
	}

	m = land(t, flipTheme(m, "light"))
	if m.crop != zoomed {
		t.Errorf("light variant opened at %+v, want %+v", m.crop, zoomed)
	}
	m = land(t, flipTheme(m, "dark"))
	if m.crop != zoomed {
		t.Errorf("dark variant reopened at %+v, want %+v", m.crop, zoomed)
	}
}

func TestReloadKeepsDiagramCropAcrossThemeSwitch(t *testing.T) {
	dir := cropDir(t)
	m := newManifestCropModel(t, "dark", diagramPair(t, dir, hashA, sizeA), diagramPair(t, dir, hashB, sizeB))
	m = sendAll(t, m, keyZoom, keyZoom, keyJ)
	zoomed := m.crop
	m = sendAll(t, m, key2)

	m = flipTheme(m, "light")
	if _, ok := m.crops[cropKey(m.images[0])]; !ok {
		t.Fatalf("crops = %v, want the unselected diagram's crop kept across the theme flip", m.crops)
	}
	m = sendAll(t, m, key1)
	if m.crop != zoomed {
		t.Errorf("diagram reopened at %+v, want %+v", m.crop, zoomed)
	}
}

func TestDiagramCropDroppedWhenSourceChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, dir string, m galleryModel) galleryModel
	}{
		{"canonical render rewritten at a new size", func(t *testing.T, dir string, m galleryModel) galleryModel {
			p := withTheme(m.images[0].Path, "dark")
			if err := os.WriteFile(p, encode16BitPNG(t, sizeC.X, sizeC.Y, 0x7000), 0o600); err != nil {
				t.Fatal(err)
			}
			return flipTheme(m, "light")
		}},
		{"canonical render with a newer mtime", func(t *testing.T, dir string, m galleryModel) galleryModel {
			later := time.Now().Add(time.Hour)
			if err := os.Chtimes(withTheme(m.images[0].Path, "dark"), later, later); err != nil {
				t.Fatal(err)
			}
			return flipTheme(m, "light")
		}},
		{"different hash", func(t *testing.T, dir string, m galleryModel) galleryModel {
			writeCropManifest(t, m.pane, diagramPair(t, dir, hashB, sizeA))
			m = reloaded(m)
			if len(m.crops) != 0 {
				t.Errorf("crops = %v, want the replaced diagram's crop pruned", m.crops)
			}
			return m
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := cropDir(t)
			m := newManifestCropModel(t, "dark", diagramPair(t, dir, hashA, sizeA))
			m = sendAll(t, m, keyZoom, keyZoom, keyJ)
			if m.crop.isFull() {
				t.Fatal("setup: diagram is not zoomed")
			}
			m = tc.change(t, dir, m)
			if !m.crop.isFull() {
				t.Errorf("changed diagram opened at %+v, want fit", m.crop)
			}
		})
	}
}

// A raster is keyed by its exact path: x-dark.png and x-light.png are two
// images, not theme variants of one.
func TestRasterCropNotConflatedAcrossThemeSuffix(t *testing.T) {
	dir := cropDir(t)
	var lines []string
	for _, name := range []string{"x-dark.png", "x-light.png"} {
		p := filepath.Join(dir, name)
		writeTestImage(t, p, sizeA.X, sizeA.Y)
		lines = append(lines, `{"type":"image","path":"`+p+`","mtime":1}`)
	}
	m := newDecodeModel(t, sizeA)
	writeCropManifest(t, m.pane, lines...)
	m = land(t, reloaded(m))
	if len(m.images) != 2 {
		t.Fatalf("setup: images = %+v, want both rasters", m.images)
	}

	m = sendAll(t, m, keyZoom, keyZoom, keyJ)
	zoomed := m.crop
	m = sendAll(t, m, key2)
	if !m.crop.isFull() {
		t.Errorf("x-light.png opened at %+v, want fit", m.crop)
	}
	m = sendAll(t, m, key1)
	if m.crop != zoomed {
		t.Errorf("x-dark.png reopened at %+v, want %+v", m.crop, zoomed)
	}
}
