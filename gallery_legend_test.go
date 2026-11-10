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

// A sidecar resvg cannot render is reported once and never retried per frame.
func TestLegendRenderFailureReportedAndNotRetried(t *testing.T) {
	svg, _ := animStub(t)
	m, _ := newAnimModel(t, 1, svg, animOff)
	writeLegend(t, m.images[0].Path, legendSuffix)
	log := filepath.Join(t.TempDir(), "calls.log")
	script := filepath.Join(t.TempDir(), "resvg")
	body := fmt.Sprintf("#!/bin/sh\necho x >> %q\nexit 1\n", log)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil { //nolint:gosec // test fixture in a temp dir
		t.Fatal(err)
	}
	t.Setenv("AEYE_RESVG", script)

	pressLegend(m)
	if m.status != "Legend failed to render" || m.legend.on {
		t.Fatalf("status=%q on=%v, want the failure hint and the toggle off", m.status, m.legend.on)
	}
	if n := logLines(t, log); n != 1 {
		t.Fatalf("resvg ran %d times for one failed toggle", n)
	}

	// Left on (as if an earlier render had worked), a pan frame must not retry.
	m.legend.on = true
	m.transmitPreviewOnly()
	m.transmitPreviewOnly()
	if n := logLines(t, log); n != 1 {
		t.Errorf("failed legend re-rendered on pan frames: resvg ran %d times", n)
	}
}

func TestPlaceholderBlockOver(t *testing.T) {
	const id, ovID = 0x010203, 0x040506
	if got, want := placeholderBlockOver(id, 6, 4, ovID, 0, 0), placeholderBlock(id, 6, 4); got != want {
		t.Errorf("no overlay must equal the plain block:\n got %q\nwant %q", got, want)
	}
	// The overlay replaces the last two cells of the last row, indexed from its own origin.
	cell := func(id, r, c int) string {
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (id>>16)&0xff, (id>>8)&0xff, id&0xff) +
			placeholderRune + string(rowColDiacritics[r]) + string(rowColDiacritics[c])
	}
	rows := strings.Split(placeholderBlockOver(id, 3, 2, ovID, 2, 1), "\n")
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	plain := strings.Split(placeholderBlock(id, 3, 2), "\n")
	if rows[0] != plain[0] {
		t.Errorf("row above the overlay changed:\n got %q\nwant %q", rows[0], plain[0])
	}
	want := cell(id, 1, 0) + "\x1b[39m" + cell(ovID, 0, 0) + placeholderRune + string(rowColDiacritics[0]) + string(rowColDiacritics[1]) + "\x1b[39m"
	if rows[1] != want {
		t.Errorf("overlay row:\n got %q\nwant %q", rows[1], want)
	}
	// A stale overlay larger than the preview is clamped to it: no panic, the
	// preview id gone only because the overlay now covers all of it.
	if got := placeholderBlockOver(id, 2, 2, ovID, 9, 9); strings.Count(got, placeholderRune) != 4 {
		t.Errorf("oversized overlay changed the cell count: %q", got)
	}
}

// legendID must never collide with a filmstrip slot id, however wide the pane.
func TestLegendIDReserved(t *testing.T) {
	m := &galleryModel{pane: "%9"}
	l := computeLayout(1<<20, 50)
	for slot := 0; slot <= l.stripCols; slot++ {
		for bit := range 2 {
			if m.slotIDFor(slot, bit) == m.legendID() {
				t.Fatalf("slot %d bit %d uses the legend id %d", slot, bit, m.legendID())
			}
		}
	}
}
