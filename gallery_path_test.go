package main

import (
	"math"
	"testing"
)

func bboxEq(t *testing.T, d string, x0, y0, x1, y1 float64) {
	t.Helper()
	gx0, gy0, gx1, gy1, ok := pathBBox(d)
	if !ok {
		t.Fatalf("pathBBox(%q) ok=false", d)
	}
	for _, p := range []struct {
		name     string
		got, exp float64
	}{{"x0", gx0, x0}, {"y0", gy0, y0}, {"x1", gx1, x1}, {"y1", gy1, y1}} {
		if math.Abs(p.got-p.exp) > 0.5 {
			t.Errorf("%s = %v, want %v (d=%q)", p.name, p.got, p.exp, d)
		}
	}
}

func TestPathBBoxRectLines(t *testing.T) {
	bboxEq(t, "M0 0 L100 0 L100 50 L0 50 Z", 0, 0, 100, 50)
}

func TestPathBBoxCylinderCurvesAndV(t *testing.T) {
	// the real d2 cylinder path (sketch): C cubics + V vertical lines.
	d := "M 0 24 C 0 0 61 0 68 0 C 75 0 136 0 136 24 V 142 C 136 166 75 166 68 166 C 61 166 0 166 0 142 V 24 Z"
	bboxEq(t, d, 0, 0, 136, 166)
}

func TestPathBBoxRelative(t *testing.T) {
	bboxEq(t, "M10 10 l20 0 l0 20 l-20 0 z", 10, 10, 30, 30)
}

func TestPathBBoxEmpty(t *testing.T) {
	if _, _, _, _, ok := pathBBox("Z"); ok {
		t.Error("no coordinates → ok must be false")
	}
}
