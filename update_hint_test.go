package main

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubAeye writes an executable named `name` that prints ver for --version and
// appends a line to a call log, so a test can tell whether it was exec'd.
func stubAeye(t *testing.T, dir, name, ver, callLog string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	script := "#!/bin/sh\necho ran >> " + callLog + "\necho " + ver + "\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDriftHint(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		running, candidate, want string
	}{
		{"same build", "1.0.2-b421285", "1.0.2-b421285", ""},
		{"older running", "1.0.0-9de760d", "1.0.2-b421285", "1.0.2"},
		{"unknown candidate", "1.0.2-b421285", "", ""},
		{"suffix dropped", "0.19.0", "1.0.2-b421285", "1.0.2"},
		{"same release, new build", "1.0.2-aaaaaaa", "1.0.2-bbbbbbb", "1.0.2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := driftHint(tc.running, tc.candidate); got != tc.want {
				t.Errorf("driftHint(%q, %q) = %q, want %q", tc.running, tc.candidate, got, tc.want)
			}
		})
	}
}

func TestLauncherBinPrefersAEYEBIN(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	onPath := stubAeye(t, dir, "aeye", "1.0.2-onpath", log)
	pinned := stubAeye(t, dir, "aeye-pinned", "0.19.0-pinned", log)

	t.Setenv("PATH", dir)
	t.Setenv("AEYE_BIN", "")
	if got := launcherBin(); got != onPath {
		t.Errorf("without AEYE_BIN: got %q, want %q", got, onPath)
	}
	// The launcher honors AEYE_BIN over PATH, so the hint must too — otherwise
	// it advertises a build that reopening the pane would not actually exec.
	t.Setenv("AEYE_BIN", pinned)
	if got := launcherBin(); got != pinned {
		t.Errorf("with AEYE_BIN: got %q, want %q", got, pinned)
	}
}

// Regression guard for the case that motivated this: /etc/profiles/…/aeye is a
// symlink, so an unresolved path looks identical across a nix switch.
func TestLauncherBinResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := stubAeye(t, dir, "aeye-real", "1.0.2", filepath.Join(dir, "calls"))
	linkDir := t.TempDir()
	if err := os.Symlink(real, filepath.Join(linkDir, "aeye")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", linkDir)
	t.Setenv("AEYE_BIN", "")
	if got := launcherBin(); got != real {
		t.Errorf("launcherBin() = %q, want the resolved target %q", got, real)
	}
}

func TestLauncherBinMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("AEYE_BIN", "")
	if got := launcherBin(); got != "" {
		t.Errorf("no aeye on PATH: got %q, want \"\"", got)
	}
}

func TestBinVersion(t *testing.T) {
	dir := t.TempDir()
	p := stubAeye(t, dir, "aeye", "1.0.2-b421285", filepath.Join(dir, "calls"))
	if got := binVersion(p); got != "1.0.2-b421285" {
		t.Errorf("binVersion() = %q, want %q", got, "1.0.2-b421285")
	}
}

func TestProbeUpdateCmdExecsOnNewPath(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	p := stubAeye(t, dir, "aeye", "1.0.2-b421285", log)
	t.Setenv("PATH", dir)
	t.Setenv("AEYE_BIN", "")

	msg, ok := probeUpdateCmd(updateWatch{})().(updateProbeMsg)
	if !ok {
		t.Fatal("probe returned no updateProbeMsg")
	}
	if msg.path != p || msg.ver != "1.0.2-b421285" {
		t.Errorf("probe = %+v, want path %q ver %q", msg, p, "1.0.2-b421285")
	}
}

// The probe runs every 30s for the life of the viewer; only a changed path may
// cost a fork.
func TestProbeUpdateCmdSkipsExecOnUnchangedPath(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	p := stubAeye(t, dir, "aeye", "1.0.2-b421285", log)
	t.Setenv("PATH", dir)
	t.Setenv("AEYE_BIN", "")

	known := updateWatch{path: p, ver: "1.0.2-b421285"}
	msg := probeUpdateCmd(known)().(updateProbeMsg)
	if msg.ver != known.ver {
		t.Errorf("cached ver = %q, want %q", msg.ver, known.ver)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Error("probe exec'd the binary although the path was unchanged")
	}
}

func TestProbeUpdateCmdClearsWhenBinaryGone(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("AEYE_BIN", "")
	msg := probeUpdateCmd(updateWatch{path: "/gone/aeye", ver: "1.0.2"})().(updateProbeMsg)
	if msg.path != "" || msg.ver != "" {
		t.Errorf("probe = %+v, want zero — a stale version must not keep advertising", msg)
	}
}

// The hint is only useful if it reaches the screen: the title is the one place
// the running build is named, so that is where drift has to surface.
func TestRenderViewShowsDriftHint(t *testing.T) {
	m := galleryModel{
		pane:    "%0",
		backend: backendKitty,
		images:  []imageEntry{{Path: "/tmp/shot.png"}},
		curImg:  image.NewRGBA(image.Rect(0, 0, 100, 100)),
		crop:    fullCrop(),
		width:   100,
		height:  40,
		cellW:   10,
		cellH:   20,
		l:       layout{previewW: 40, previewH: 16, stripW: 12, stripH: 6, stripCols: 1},
	}
	if got := m.renderView(); strings.Contains(got, "restart pane") {
		t.Error("current build must not nag about restarting")
	}

	m.update.hint = "1.0.2"
	got := m.renderView()
	if !strings.Contains(got, "→ 1.0.2 · restart pane") {
		t.Error("drifted build must name the release a restart lands on")
	}
	if !strings.Contains(got, version()) {
		t.Error("title must still carry the running build's own version")
	}
}
