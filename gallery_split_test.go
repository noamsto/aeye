package main

import "testing"

func TestFlipAxis(t *testing.T) {
	cases := []struct{ cur, wantNext, wantFlag string }{
		{"side", "bottom", "-v"},
		{"bottom", "side", "-h"},
		{"", "bottom", "-v"}, // unset defaults to a side layout, so it flips to bottom
		{"garbage", "bottom", "-v"},
	}
	for _, c := range cases {
		next, flag := flipAxis(c.cur)
		if next != c.wantNext || flag != c.wantFlag {
			t.Errorf("flipAxis(%q) = (%q,%q); want (%q,%q)", c.cur, next, flag, c.wantNext, c.wantFlag)
		}
	}
}
