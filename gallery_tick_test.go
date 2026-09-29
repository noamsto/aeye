package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// stale must fire on exactly the conditions the galleryTickMsg handler acts
// on: a quiet poll that misses one leaves the carousel stuck until restart.
func TestTickSnapshotStale(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AEYE_DIR", dir)
	t.Setenv("XDG_STATE_HOME", dir) // no theme-state.json: Detect() == "dark"
	t.Setenv("AEYE_BRIDGED", "")
	manifest := manifestPath("%1")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	visible, relayed := true, false
	prev := tickProbe
	tickProbe = func() (bool, bool) { return visible, relayed }
	t.Cleanup(func() { tickProbe = prev })

	s := &tickSnapshot{}
	s.publish(galleryModel{visible: true, theme: "dark", mtime: manifestMtime("%1"), bridged: bridgedPolicy(false)})
	if s.stale("%1") {
		t.Fatal("stale with nothing changed — every poll would re-render")
	}

	visible = false
	if !s.stale("%1") {
		t.Error("visibility flip not detected")
	}
	visible = true

	relayed = true
	if !s.stale("%1") {
		t.Error("relay attach not detected")
	}
	relayed = false

	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(manifest, later, later); err != nil {
		t.Fatal(err)
	}
	if !s.stale("%1") {
		t.Error("manifest change not detected")
	}
}

// A message handled outside the tick (FocusMsg sets visible) must reach the
// poller's snapshot, or it compares against a value the model no longer holds.
func TestUpdatePublishesTickSnapshot(t *testing.T) {
	m, _ := newVisibilityModel(t)
	m.visible = false
	m.tick = &tickSnapshot{}

	m.Update(tea.FocusMsg{})

	if !m.tick.visible {
		t.Fatal("FocusMsg's visible=true not published to the tick snapshot")
	}
}
