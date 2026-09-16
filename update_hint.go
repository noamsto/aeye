package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// updateInterval paces the drift probe. A rebuild lands a few times a day at
// most, so a tighter cadence buys nothing.
const updateInterval = 30 * time.Second

// updateWatch is what the last probe found, so a probe that resolves the same
// binary as the previous one can reuse its version instead of exec'ing again.
type updateWatch struct {
	path string // resolved launcher binary at the last probe
	ver  string // that binary's --version output
	hint string // release to advertise in the title, "" when this build is current
}

// launcherBin resolves the viewer binary the way tmux-claude-images does —
// $AEYE_BIN, else `aeye` on PATH — so the hint names what reopening the pane
// would actually exec rather than whatever PATH happens to offer here.
func launcherBin() string {
	bin := os.Getenv("AEYE_BIN")
	if bin == "" {
		bin = "aeye"
	}
	p, err := exec.LookPath(bin)
	if err != nil {
		return ""
	}
	// Resolved, so a profile symlink that re-points (nix switch) reads as a new
	// binary rather than the same /etc/profiles path it has always been.
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return ""
	}
	return resolved
}

// binVersion asks a binary what build it is. The comparison has to be on
// version strings rather than paths: under a nix wrapper our own
// os.Executable() is .aeye-wrapped while the launcher resolves to the sibling
// `aeye`, so identical builds have different paths.
func binVersion(path string) string {
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// driftHint is the release to advertise when candidate is a different build
// from running, else "". The build suffix is dropped because the title already
// carries this build's full string; the hint only names where a restart lands.
func driftHint(running, candidate string) string {
	if candidate == "" || candidate == running {
		return ""
	}
	return strings.SplitN(candidate, "-", 2)[0]
}

type updateTickMsg struct{}

// updateTickCmd re-arms the drift probe.
func updateTickCmd() tea.Cmd {
	return tea.Tick(updateInterval, func(time.Time) tea.Msg { return updateTickMsg{} })
}

// updateProbeMsg carries a finished probe back to Update.
type updateProbeMsg struct {
	path string
	ver  string
}

// probeUpdateCmd resolves the launcher's binary off the event loop, exec'ing it
// for its version only when the path changed since the last probe — a fork of a
// ~40MB binary is not something to do between frames.
func probeUpdateCmd(known updateWatch) tea.Cmd {
	return func() tea.Msg {
		p := launcherBin()
		if p == "" {
			return updateProbeMsg{}
		}
		if p == known.path {
			return updateProbeMsg{path: p, ver: known.ver}
		}
		return updateProbeMsg{path: p, ver: binVersion(p)}
	}
}
