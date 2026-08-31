package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/noamsto/themestate"
)

// TestParsePaneVisible pins the gate tmux actually applies to DCS passthrough:
// tty_write skips clients whose current window isn't the pane's, and
// window_pane_visible hides every non-active pane of a zoomed window. Pane
// focus is not part of it — #133 first blamed focus, scoping the bug too narrowly.
func TestParsePaneVisible(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want bool
	}{
		{"active pane, attached current window", "1 1 0 1", true},
		{"inactive pane, attached current window", "1 1 0 0", true},
		{"pane in a non-current window", "0 1 0 1", false},
		{"current window of a detached session", "1 0 0 1", false},
		{"zoomed window, another pane holds the zoom", "1 1 1 0", false},
		{"zoomed window, we hold the zoom", "1 1 1 1", true},
		{"unparseable output falls back to visible", "no tmux here", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePaneVisible(tt.out); got != tt.want {
				t.Fatalf("parsePaneVisible(%q) = %v, want %v", tt.out, got, tt.want)
			}
		})
	}
}

// TestTickRestoresStoreWhenWindowBecomesVisible is the #133 regression: every
// store issued while the pane's window was hidden was dropped by tmux, and a
// window switch replays the pane from tmux's screen buffer without waking
// bubbletea. Unless the tick notices the edge, the placeholder cells reference
// image ids that never landed and the carousel stays blank.
func TestTickRestoresStoreWhenWindowBecomesVisible(t *testing.T) {
	m, collect := newVisibilityModel(t)
	m.visible = false
	stubPaneVisible(t, true)

	m.Update(galleryTickMsg{})
	out := collect()

	// Both layers must come back: the preview alone is what a keypress already
	// recovers (vectorReadyMsg re-stores only previewID), and that partial
	// recovery is the exact symptom — preview fills, filmstrip stays blank.
	for _, want := range [][]byte{
		[]byte(fmt.Sprintf("\x1b_Gi=%d,a=T", m.previewID())),
		[]byte(fmt.Sprintf("\x1b_Gi=%d,a=T", m.stripID(0))),
	} {
		if !bytes.Contains(out, want) {
			t.Fatalf("no re-store of %q on the hidden->visible edge; got %q", want, out)
		}
	}
}

// TestTickDoesNotRestoreWhileVisible guards the polling cost: the edge fires
// once, not on every 1.5s tick.
func TestTickDoesNotRestoreWhileVisible(t *testing.T) {
	m, collect := newVisibilityModel(t)
	m.visible = true
	stubPaneVisible(t, true)

	m.Update(galleryTickMsg{})

	if out := collect(); bytes.Contains(out, []byte("a=T")) {
		t.Fatalf("steady-state tick re-stored images; got %q", out)
	}
}

// TestTickArmsEdgeWhenWindowGoesHidden: without recording the way out, there is
// no edge to detect on the way back in.
func TestTickArmsEdgeWhenWindowGoesHidden(t *testing.T) {
	m, collect := newVisibilityModel(t)
	m.visible = true
	stubPaneVisible(t, false)

	next, _ := m.Update(galleryTickMsg{})

	if next.(galleryModel).visible {
		t.Fatal("tick did not record the window going hidden; the way back can't re-store")
	}
	if out := collect(); bytes.Contains(out, []byte("a=T")) {
		t.Fatalf("stored into a hidden window; got %q", out)
	}
}

// TestFocusMsgRestoresStore covers the fast path: waiting up to a full tick to
// repaint a window the user just switched to is visible lag.
func TestFocusMsgRestoresStore(t *testing.T) {
	m, collect := newVisibilityModel(t)
	m.visible = false

	m.Update(tea.FocusMsg{})

	want := []byte(fmt.Sprintf("\x1b_Gi=%d,a=T", m.previewID()))
	if out := collect(); !bytes.Contains(out, want) {
		t.Fatalf("FocusMsg did not re-store the image store; got %q", out)
	}
}

// TestViewEnablesFocusReporting: bubbletea v2 carries this on the View, not as
// a NewProgram option. Off, tmux never sends FocusMsg and the fast path is dead.
func TestViewEnablesFocusReporting(t *testing.T) {
	var m galleryModel
	if !m.View().ReportFocus {
		t.Fatal("View.ReportFocus is false; FocusMsg will never arrive")
	}
}

// newVisibilityModel returns a kitty-backed model wired to a pipe tty and a
// hermetic one-image manifest, plus a collector that closes the write end and
// returns every byte the model emitted. theme/mtime are pre-seeded so the
// tick's own theme and manifest checks stay no-ops and can't transmit.
func newVisibilityModel(t *testing.T) (galleryModel, func() []byte) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AEYE_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	const pane = "7"
	entry := fmt.Sprintf("{\"type\":\"image\",\"path\":%q}\n", writeTestPNG(t))
	if err := os.WriteFile(manifestPath(pane), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	got := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(r); got <- b }()

	theme := themestate.Detect()
	m := galleryModel{
		pane:    pane,
		backend: backendKitty,
		tty:     w,
		images:  loadManifest(pane, theme),
		theme:   theme,
		mtime:   manifestMtime(pane),
		ready:   true,
		width:   100,
		height:  40,
	}
	m.l = computeLayout(m.width, m.height)
	if len(m.images) != 1 {
		t.Fatalf("fixture manifest did not load: %d images", len(m.images))
	}
	return m, func() []byte { w.Close(); return <-got }
}

func stubPaneVisible(t *testing.T, visible bool) {
	t.Helper()
	prev := paneVisible
	paneVisible = func() bool { return visible }
	t.Cleanup(func() { paneVisible = prev })
}
