package main

import "testing"

func TestVectorTargetWBounded(t *testing.T) {
	// full crop → roughly one box width (rest-state fill, no source-size blowup).
	boxW, boxH := 800, 400
	if got := vectorTargetW(boxW, boxH, fullCrop()); got < boxW || got > boxW*2 {
		t.Errorf("full-crop targetW = %d, want ~%d", got, boxW)
	}
	// 8x zoom (min side 1/8) → ~8 box widths, NOT a function of source size.
	c := cropFrac{x0: 0.4, y0: 0.4, x1: 0.525, y1: 0.525} // side 0.125 = 1/8
	if got := vectorTargetW(boxW, boxH, c); got < boxW*7 || got > boxW*9 {
		t.Errorf("8x targetW = %d, want ~%d", got, boxW*8)
	}
}

func TestRenderVectorMissingResvg(t *testing.T) {
	t.Setenv("AGENT_CAROUSEL_RESVG", "/definitely/not/resvg")
	if got := renderVector("/whatever.svg", 1000); got != "" {
		t.Errorf("absent resvg must yield empty string, got %q", got)
	}
}
