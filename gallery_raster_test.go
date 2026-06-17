package main

import "testing"

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
