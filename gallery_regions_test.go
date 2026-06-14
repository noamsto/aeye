package main

import (
	"math"
	"os"
	"testing"
)

func keysOf(m map[string]region) []string {
	var k []string
	for s := range m {
		k = append(k, s)
	}
	return k
}

func TestParseRegionsSketch(t *testing.T) {
	data, err := os.ReadFile("tests/fixtures/regions-sketch.svg")
	if err != nil {
		t.Fatal(err)
	}
	rs := parseRegions(data)
	paths := map[string]region{}
	for _, r := range rs {
		paths[r.path] = r
	}
	for _, want := range []string{"ingest", "store", "ingest.read", "ingest.parse", "store.t"} {
		if _, ok := paths[want]; !ok {
			t.Errorf("missing region %q (got %v)", want, keysOf(paths))
		}
	}
	for got := range paths {
		if got == "(ingest -> store)[0]" || got == "(ingest -&gt; store)[0]" {
			t.Errorf("connection group must be skipped: %q", got)
		}
	}
	ing, ok := paths["ingest"]
	if !ok {
		t.Fatal("no ingest region")
	}
	if !(ing.x0 >= 0 && ing.x1 <= 1.0001 && ing.x0 < ing.x1 && ing.y0 < ing.y1) {
		t.Errorf("ingest bbox not a normalized rect: %+v", ing)
	}
	if rd, ok := paths["ingest.read"]; ok {
		if !(ing.x0 <= rd.x0+1e-6 && ing.x1 >= rd.x1-1e-6 && ing.y0 <= rd.y0+1e-6 && ing.y1 >= rd.y1-1e-6) {
			t.Errorf("ingest must contain ingest.read: %+v vs %+v", ing, rd)
		}
	}
}

func pathsOf(rs []region) []string {
	var p []string
	for _, r := range rs {
		p = append(p, r.path)
	}
	return p
}

func TestRegionTreeDrill(t *testing.T) {
	rs := []region{
		{path: "ingest", x0: 0.0, y0: 0, x1: 0.4, y1: 1},
		{path: "store", x0: 0.6, y0: 0, x1: 1, y1: 1},
		{path: "ingest.read", x0: 0.05, y0: 0.1, x1: 0.35, y1: 0.4},
		{path: "ingest.parse", x0: 0.05, y0: 0.6, x1: 0.35, y1: 0.9},
	}
	tr := newRegionTree(rs)

	root := tr.childrenOf(nil)
	if len(root) != 2 || root[0].path != "ingest" || root[1].path != "store" {
		t.Fatalf("root level = %v", pathsOf(root))
	}
	kids := tr.childrenOf([]string{"ingest"})
	if len(kids) != 2 || kids[0].path != "ingest.read" || kids[1].path != "ingest.parse" {
		t.Fatalf("ingest children = %v", pathsOf(kids))
	}
	if len(tr.childrenOf([]string{"store"})) != 0 {
		t.Error("store should be a leaf")
	}
}

func TestFrameRegionContainsAndAspect(t *testing.T) {
	// landscape source 2000x1000, landscape box 800x400 → target frac-aspect = (800*1000)/(400*2000) = 1.0
	r := region{x0: 0.4, y0: 0.45, x1: 0.6, y1: 0.55}
	c := frameRegion(r, 2000, 1000, 800, 400)
	if !(c.x0 <= r.x0 && c.x1 >= r.x1 && c.y0 <= r.y0 && c.y1 >= r.y1) {
		t.Errorf("crop must contain region: %+v vs %+v", c, r)
	}
	if math.Abs(c.cx()-r.cx()) > 1e-9 || math.Abs(c.cy()-r.cy()) > 1e-9 {
		t.Errorf("crop not centered on region: %+v", c)
	}
	if af := c.w() / c.h(); math.Abs(af-1.0) > 1e-6 {
		t.Errorf("crop frac-aspect = %v, want 1.0", af)
	}
	if c.x0 < -1e-9 || c.y0 < -1e-9 || c.x1 > 1+1e-9 || c.y1 > 1+1e-9 {
		t.Errorf("crop escaped [0,1]: %+v", c)
	}
}

func TestFrameRegionClampsAtEdge(t *testing.T) {
	r := region{x0: 0, y0: 0, x1: 0.2, y1: 0.2}
	c := frameRegion(r, 1000, 1000, 400, 400)
	if c.x0 < -1e-9 || c.y0 < -1e-9 {
		t.Errorf("corner crop must clamp to 0: %+v", c)
	}
	if !(c.x1 >= r.x1 && c.y1 >= r.y1) {
		t.Errorf("crop must still contain region after clamp: %+v", c)
	}
}

func TestRegionModeCycleAndDrill(t *testing.T) {
	rs := []region{
		{path: "ingest", x0: 0, y0: 0, x1: 0.4, y1: 1},
		{path: "store", x0: 0.6, y0: 0, x1: 1, y1: 1},
		{path: "ingest.read", x0: 0.05, y0: 0.1, x1: 0.35, y1: 0.4},
		{path: "ingest.parse", x0: 0.05, y0: 0.6, x1: 0.35, y1: 0.9},
	}
	m := &galleryModel{regions: newRegionTree(rs), regionIdx: -1, l: layout{previewW: 80, previewH: 40}}

	m.cycleRegion(+1)
	if r, ok := m.focusedRegion(); !ok || r.path != "ingest" {
		t.Fatalf("first focus = %v,%v", r.path, ok)
	}
	m.cycleRegion(+1)
	if r, _ := m.focusedRegion(); r.path != "store" {
		t.Fatalf("cycle → %v, want store", r.path)
	}
	m.cycleRegion(+1)
	if r, _ := m.focusedRegion(); r.path != "ingest" {
		t.Fatalf("wrap → %v, want ingest", r.path)
	}
	m.drillIn()
	if r, ok := m.focusedRegion(); !ok || r.path != "ingest.read" {
		t.Fatalf("drillIn focus = %v,%v", r.path, ok)
	}
	m.drillOut()
	if r, _ := m.focusedRegion(); r.path != "ingest" {
		t.Fatalf("drillOut focus = %v, want ingest", r.path)
	}
}

// A left-to-right flow can place the entry node lower than its neighbours;
// Tab must still start at the leftmost group, not the topmost. Coordinates
// mirror a real D2 render (api gateway on the left, sitting below the services
// and data columns it fans into).
func TestRegionCycleHorizontalFlow(t *testing.T) {
	rs := []region{
		{path: "api", x0: 0.088, y0: 0.305, x1: 0.309, y1: 0.829},
		{path: "services", x0: 0.431, y0: 0.176, x1: 0.626, y1: 0.717},
		{path: "data", x0: 0.748, y0: 0.194, x1: 0.910, y1: 0.734},
	}
	m := &galleryModel{regions: newRegionTree(rs), regionIdx: -1, l: layout{previewW: 80, previewH: 40}}
	for _, want := range []string{"api", "services", "data"} {
		m.cycleRegion(+1)
		if r, _ := m.focusedRegion(); r.path != want {
			t.Fatalf("cycle → %v, want %v", r.path, want)
		}
	}
}

func TestDrillInLeafNoOp(t *testing.T) {
	rs := []region{{path: "store", x0: 0.6, y0: 0, x1: 1, y1: 1}}
	m := &galleryModel{regions: newRegionTree(rs), regionIdx: -1, l: layout{previewW: 80, previewH: 40}}
	m.cycleRegion(+1)
	m.drillIn()
	if r, _ := m.focusedRegion(); r.path != "store" {
		t.Fatalf("leaf drillIn moved focus to %v", r.path)
	}
}
