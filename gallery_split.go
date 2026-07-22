package main

import (
	"os"
	"os/exec"
	"strings"
)

// flipAxis returns the opposite split axis and the tmux split-window flag that
// produces it via move-pane. Anything other than "bottom" is treated as a side
// layout, so the first press always lands on "bottom".
func flipAxis(cur string) (next, tmuxFlag string) {
	if cur == "bottom" {
		return "side", "-h"
	}
	return "bottom", "-v"
}

// tmuxPaneAxis reads the @claude_img_axis pane option the launcher recorded, or
// "side" when unset or off-tmux (the historical default).
func tmuxPaneAxis() string {
	out, err := exec.Command("tmux", "show-options", "-p", "-qv", "@claude_img_axis").Output()
	if err != nil {
		return "side"
	}
	if strings.TrimSpace(string(out)) == "bottom" {
		return "bottom"
	}
	return "side"
}

// toggleSplitAxis flips the carousel between a side (left|right) and bottom
// (top/bottom) split of its tmux host. tmux-only: move-pane re-splits the host
// in place, so the viewer process survives and repaints on the resize it
// receives. A no-op off-tmux — there m.pane is a session id, and the other
// backends have no in-place axis flip in v1.
func (m *galleryModel) toggleSplitAxis() {
	if os.Getenv("TMUX") == "" {
		return
	}
	next, flag := flipAxis(m.splitAxis)
	out, err := exec.Command("tmux", "display-message", "-p", "#{pane_id}").Output()
	if err != nil {
		return
	}
	self := strings.TrimSpace(string(out))
	if err := exec.Command("tmux", "move-pane", flag, "-s", self, "-t", m.pane).Run(); err != nil {
		return
	}
	m.splitAxis = next
	_ = exec.Command("tmux", "set-option", "-p", "-t", self, "@claude_img_axis", next).Run()
}
