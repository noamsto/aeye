package main

import "testing"

func TestHostPane(t *testing.T) {
	cases := []struct{ env, key, want string }{
		{"%10", "12448-10", "%10"}, // launcher forwarded the host pane
		{"", "12448-10", ""},       // a server-scoped key is not a tmux target
		{"", "%10", "%10"},         // manual `aeye %10` launch
		{"", "sess-abc", ""},       // off-tmux session key
		{"%3", "%10", "%3"},        // env wins over the key
	}
	for _, c := range cases {
		t.Setenv("AEYE_HOST_PANE", c.env)
		if got := hostPane(c.key); got != c.want {
			t.Errorf("hostPane(%q) with AEYE_HOST_PANE=%q = %q; want %q", c.key, c.env, got, c.want)
		}
	}
}

func TestParseSelfPane(t *testing.T) {
	cases := []struct {
		out          string
		wantID       string
		wantFloating bool
	}{
		{"%5 1\n", "%5", true},
		{"%5 0\n", "%5", false},
		{"%5 \n", "%5", false}, // server without floats expands the format to empty
		{"%5\n", "%5", false},
	}
	for _, c := range cases {
		id, floating := parseSelfPane(c.out)
		if id != c.wantID || floating != c.wantFloating {
			t.Errorf("parseSelfPane(%q) = (%q,%v); want (%q,%v)", c.out, id, floating, c.wantID, c.wantFloating)
		}
	}
}

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
