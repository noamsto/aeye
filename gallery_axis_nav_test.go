package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestAxisFallbackNavigation pins the per-axis contract for the zoomed pan keys:
// a key pans on an axis whose crop has slack, and switches images on an axis
// whose crop already spans the whole image.
func TestAxisFallbackNavigation(t *testing.T) {
	type keyCase struct {
		msg      tea.KeyPressMsg
		vertical bool
	}
	keys := []keyCase{
		{tea.KeyPressMsg{Text: "h", Code: 'h'}, false},
		{tea.KeyPressMsg{Text: "l", Code: 'l'}, false},
		{tea.KeyPressMsg{Code: tea.KeyLeft}, false},
		{tea.KeyPressMsg{Code: tea.KeyRight}, false},
		{tea.KeyPressMsg{Text: "k", Code: 'k'}, true},
		{tea.KeyPressMsg{Text: "j", Code: 'j'}, true},
		{tea.KeyPressMsg{Code: tea.KeyUp}, true},
		{tea.KeyPressMsg{Code: tea.KeyDown}, true},
	}
	shapes := []struct {
		name               string
		crop               cropFrac
		wantPanX, wantPanY bool
	}{
		// w==1: the crop spans the full width, so only the vertical keys pan.
		{"thin", cropFrac{0, 0.3, 1, 0.7}, false, true},
		// h==1: the crop spans the full height, so only the horizontal keys pan.
		{"short", cropFrac{0.3, 0, 0.7, 1}, true, false},
		// Slack on both axes: every pan key pans.
		{"both", cropFrac{0.2, 0.2, 0.8, 0.8}, true, true},
	}
	for _, shape := range shapes {
		for _, k := range keys {
			t.Run(shape.name+"/"+k.msg.String(), func(t *testing.T) {
				// A fresh model per key: navigation moves the cursor, so reusing
				// one model across keys would change the starting state.
				m := land(t, newDecodeModel(t, sizeA, sizeB, sizeC))
				m = sendAll(t, m, key2) // cursor 1 of 3, so either direction moves
				m.crop = shape.crop
				before := m.crop
				m = sendAll(t, m, k.msg)

				wantPan := (!k.vertical && shape.wantPanX) || (k.vertical && shape.wantPanY)
				if wantPan {
					if m.cursor != 1 {
						t.Fatalf("cursor = %d, want 1 (pan on a slack axis, not a switch)", m.cursor)
					}
					if m.crop == before {
						t.Fatal("crop did not move on a pannable axis")
					}
					return
				}
				// No slack on this axis: the key switches images like n/p. No
				// neighbour has a remembered crop, so the target opens at fit.
				if m.cursor == 1 {
					t.Fatal("cursor stayed at 1, want a switch to another image")
				}
				if !m.crop.isFull() {
					t.Fatalf("crop after switch = %+v, want fit", m.crop)
				}
			})
		}
	}
}
