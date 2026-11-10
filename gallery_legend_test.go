package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

const legendFixtureSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 60">` +
	`<rect width="120" height="60" fill="#222"/></svg>`

func writeLegend(t *testing.T, imagePath, suffix string) string {
	t.Helper()
	p := strings.TrimSuffix(imagePath, filepath.Ext(imagePath)) + suffix
	if err := os.WriteFile(p, []byte(legendFixtureSVG), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	return p
}

// legendRowCount is the number of preview rows carrying a run of the legend's
// placeholder cells: each row sets the id's color once, then its cells.
func legendRowCount(m *galleryModel) int {
	return strings.Count(m.renderView(), legendFG(m.legendID())+placeholderRune)
}

func legendFG(id int) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (id>>16)&0xff, (id>>8)&0xff, id&0xff)
}

func pressLegend(m *galleryModel) tea.Cmd {
	return handleMsg(m, tea.KeyPressMsg{Code: 'L', Text: "L"})
}

func TestLegendSidecarThemeVariants(t *testing.T) {
	dir := t.TempDir()
	dark := filepath.Join(dir, "d-dark.png")
	light := filepath.Join(dir, "d-light.png")
	if got := legendSidecar(dark); got != "" {
		t.Fatalf("no sidecar yet, got %q", got)
	}
	shared := writeLegend(t, dark, ".legend.svg") // d-dark.legend.svg: variant-specific
	if got := legendSidecar(dark); got != shared {
		t.Errorf("variant sidecar = %q, want %q", got, shared)
	}
	if got := legendSidecar(light); got != "" {
		t.Errorf("light image picked up the dark legend: %q", got)
	}
	plain := filepath.Join(dir, "d.legend.svg")
	if err := os.WriteFile(plain, []byte(legendFixtureSVG), 0o644); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	if got := legendSidecar(light); got != plain {
		t.Errorf("light falls back to the shared legend: got %q, want %q", got, plain)
	}
	if got := legendSidecar(dark); got != shared {
		t.Errorf("variant legend must win over the shared one: got %q", got)
	}
}

func TestLegendToggleShowsAndHidesOverlay(t *testing.T) {
	svg, _ := animStub(t)
	m, rec := newAnimModel(t, 2, svg, animOff)
	writeLegend(t, m.images[0].Path, legendSuffix)
	crop := cropAtMagnification(2, .5, .5, boxAspectFrac(40, 30, m.l.previewW*m.cellWpx(), m.l.previewH*m.cellHpx()))
	m.crop = crop

	from := rec.Len()
	pressLegend(m)
	if !m.legend.on || m.legend.stored.cols == 0 {
		t.Fatalf("legend not shown: on=%v stored=%+v status=%q", m.legend.on, m.legend.stored, m.status)
	}
	if got := string(written(rec, from)); !strings.Contains(got, "i="+strconv.Itoa(m.legendID())+",") {
		t.Errorf("legend image %d was not stored: %q", m.legendID(), got)
	}
	lg := m.legend.stored
	if got, want := legendRowCount(m), lg.rows; got != want {
		t.Errorf("overlay draws %d legend rows, want %d", got, want)
	}
	if lg.cols > m.l.previewW*2/5 || lg.rows > m.l.previewH*2/5 {
		t.Errorf("overlay %dx%d exceeds the corner box of %dx%d", lg.cols, lg.rows, m.l.previewW*2/5, m.l.previewH*2/5)
	}
	if m.crop != crop {
		t.Errorf("legend moved the image's crop: %v -> %v", crop, m.crop)
	}

	from = rec.Len()
	pressLegend(m)
	if m.legend.on || m.legend.stored != (legendKey{}) {
		t.Errorf("legend still shown after the second press: on=%v stored=%+v", m.legend.on, m.legend.stored)
	}
	if got := string(written(rec, from)); !strings.Contains(got, deleteImage(m.legendID())) {
		t.Errorf("legend image not deleted: %q", got)
	}
	if got := legendRowCount(m); got != 0 {
		t.Errorf("hidden legend still draws %d rows", got)
	}
	if m.crop != crop {
		t.Errorf("hiding the legend moved the crop: %v -> %v", crop, m.crop)
	}
}

func TestLegendToggleNoSidecarIsAHint(t *testing.T) {
	svg, _ := animStub(t)
	m, rec := newAnimModel(t, 1, svg, animOff)

	from := rec.Len()
	pressLegend(m)
	if m.legend.on || m.legend.stored != (legendKey{}) {
		t.Errorf("legend enabled without a sidecar: on=%v stored=%+v", m.legend.on, m.legend.stored)
	}
	if m.status != "No legend for this image" {
		t.Errorf("status = %q, want the no-legend hint", m.status)
	}
	if got := written(rec, from); len(got) != 0 {
		t.Errorf("no-op toggle wrote to the terminal: %q", got)
	}
	if got := legendRowCount(m); got != 0 {
		t.Errorf("no legend, yet %d overlay rows drawn", got)
	}
}

// A legend that is on follows the selection: it shows for the image that has a
// sidecar, vanishes for one that has none, and returns when the terminal's
// store is dropped and restored.
func TestLegendFollowsSelectionAndRestore(t *testing.T) {
	svg, _ := animStub(t)
	m, rec := newAnimModel(t, 2, svg, animOff)
	writeLegend(t, m.images[0].Path, legendSuffix)
	pressLegend(m)
	if m.legend.stored.cols == 0 {
		t.Fatal("legend not shown on the image with a sidecar")
	}

	m.selectIndex(1)
	m.transmitView()
	if m.legend.stored != (legendKey{}) {
		t.Errorf("legend kept over an image with no sidecar: %+v", m.legend.stored)
	}
	if !m.legend.on {
		t.Error("moving off a legend turned the toggle off")
	}

	m.selectIndex(0)
	m.transmitView()
	if m.legend.stored.cols == 0 {
		t.Fatal("legend did not return with its image")
	}

	m.dropStore()
	from := rec.Len()
	m.transmitView()
	if got := string(written(rec, from)); !strings.Contains(got, "i="+strconv.Itoa(m.legendID())+",") {
		t.Errorf("restore did not re-store the legend: %q", got)
	}
}
