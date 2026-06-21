package main

import (
	"strings"
	"testing"
)

func TestParseSixelDA(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"\x1b[?62;4;6;9;22c", true},              // VT, sixel (4) present
		{"\x1b[?64;1;2;4;6;9;15;18;21;22c", true}, // 4 in a long list
		{"\x1b[?14;4c", true},                     // 4 at the end
		{"\x1b[?62;6;9;22c", false},               // no 4
		{"\x1b[?1;2c", false},                     // no 4
		{"\x1b[?44c", false},                      // 44 is not 4
		{"\x1b[?62;4", false},                     // no 'c' terminator
		{"garbage", false},                        // no '?'
		{"", false},                               // empty
	}
	for _, c := range cases {
		if got := parseSixelDA(c.in); got != c.want {
			t.Errorf("parseSixelDA(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestBlankBlock(t *testing.T) {
	if got := blankBlock(3, 2); got != "   \n   " {
		t.Errorf("blankBlock(3,2) = %q, want %q", got, "   \n   ")
	}
	if got := blankBlock(2, 1); got != "  " {
		t.Errorf("blankBlock(2,1) = %q, want %q", got, "  ")
	}
}

func TestPaintRasterAt(t *testing.T) {
	var b strings.Builder
	paintRasterAt(&b, rect{x: 4, y: 2, w: 8, h: 4}, "SIXELDATA")
	// Cursor coords are 1-based: row = y+1 = 3, col = x+1 = 5.
	want := "\x1b7\x1b[3;5HSIXELDATA\x1b8"
	if b.String() != want {
		t.Errorf("paintRasterAt =\n%q\nwant\n%q", b.String(), want)
	}
}

func TestPaintRasterAtEmpty(t *testing.T) {
	var b strings.Builder
	paintRasterAt(&b, rect{x: 1, y: 1, w: 4, h: 4}, "")
	if b.String() != "" {
		t.Errorf("paintRasterAt with empty payload wrote %q, want nothing", b.String())
	}
}
