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

func TestParseCellPxEnv(t *testing.T) {
	for _, tc := range []struct {
		in   string
		w, h int
		ok   bool
	}{
		{"15x28", 15, 28, true},
		{" 9x19 ", 9, 19, true},
		{"", 0, 0, false},
		{"15", 0, 0, false},
		{"15x", 0, 0, false},
		{"0x28", 0, 0, false},
		{"axb", 0, 0, false},
		{"15x28x3", 0, 0, false},
		{"100000x100000", 0, 0, false},
		{"1500x2800", 0, 0, false},
		{"9223372036854775807x1", 0, 0, false},
	} {
		w, h, ok := parseCellPxEnv(tc.in)
		if ok != tc.ok || w != tc.w || h != tc.h {
			t.Errorf("parseCellPxEnv(%q) = %d,%d,%v; want %d,%d,%v", tc.in, w, h, ok, tc.w, tc.h, tc.ok)
		}
	}
}

func TestParseClientCellPx(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		w, h int
		ok   bool
	}{
		{"one client", "15 28\n", 15, 28, true},
		{"skips clients that report no pixels", "0 0\n15 28\n", 15, 28, true},
		{"unsupported format expands empty", " \n", 0, 0, false},
		{"all unknown", "0 0\n0 0\n", 0, 0, false},
		{"absurd size is skipped", "100000 100000\n15 28\n", 15, 28, true},
		{"garbage", "x y\n", 0, 0, false},
		{"empty", "", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, h, ok := parseClientCellPx(tc.in)
			if ok != tc.ok || w != tc.w || h != tc.h {
				t.Errorf("parseClientCellPx(%q) = %d,%d,%v; want %d,%d,%v", tc.in, w, h, ok, tc.w, tc.h, tc.ok)
			}
		})
	}
}

func TestMeasureCellPxPrecedence(t *testing.T) {
	query := func() (int, int) { return 16, 32 }
	relay := func() (int, int, bool) { return 15, 28, true }
	noRelay := func() (int, int, bool) { return 0, 0, false }
	for _, tc := range []struct {
		name    string
		relayed bool
		env     string
		client  func() (int, int, bool)
		w, h    int
	}{
		{"env wins over everything", true, "9x19", relay, 9, 19},
		{"relay asks the mirroring client, not the pane's tty", true, "", relay, 15, 28},
		{"relay without client pixels falls back to the query", true, "", noRelay, 16, 32},
		{"bad env is ignored", true, "junk", relay, 15, 28},
		{"local terminal queries its own tty", false, "", relay, 16, 32},
		{"env also applies locally", false, "9x19", relay, 9, 19},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, h := measureCellPx(tc.relayed, tc.env, tc.client, query)
			if w != tc.w || h != tc.h {
				t.Errorf("measureCellPx = %dx%d, want %dx%d", w, h, tc.w, tc.h)
			}
		})
	}
}
