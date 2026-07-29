package main

import "testing"

func TestParseCellPxReply(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		w, h int
		ok   bool
	}{
		{"kitty reply", "\x1b[6;20;10t", 10, 20, true},
		{"leading noise", "junk\x1b[6;34;17t", 17, 34, true},
		{"wrong final byte", "\x1b[6;20;10x", 0, 0, false},
		{"missing field", "\x1b[6;20t", 0, 0, false},
		{"zero size", "\x1b[6;0;10t", 0, 0, false},
		{"empty", "", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, h, ok := parseCellPxReply(tc.in)
			if ok != tc.ok || w != tc.w || h != tc.h {
				t.Errorf("parseCellPxReply(%q) = %d,%d,%v; want %d,%d,%v",
					tc.in, w, h, ok, tc.w, tc.h, tc.ok)
			}
		})
	}
}
