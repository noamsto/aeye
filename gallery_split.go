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

// hostPane is the tmux pane the carousel was split off — a tmux *target*, which
// the manifest key is not: inside tmux the key carries the server pid
// ("<server pid>-<pane>") so two tmux servers can't share a manifest. The
// launcher forwards the pane as AEYE_HOST_PANE; key is the fallback for a manual
// `aeye %N` launch, where the key is itself a pane id. Empty when neither is.
func hostPane(key string) string {
	if p := os.Getenv("AEYE_HOST_PANE"); p != "" {
		return p
	}
	if strings.HasPrefix(key, "%") {
		return key
	}
	return ""
}

// toggleSplitAxis flips the carousel between a side (left|right) and bottom
// (top/bottom) split of its tmux host. tmux-only: move-pane re-splits the host
// in place, so the viewer process survives and repaints on the resize it
// receives. A no-op off-tmux — there is no host pane there, and the other
// backends have no in-place axis flip in v1.
func (m *galleryModel) toggleSplitAxis() {
	if os.Getenv("TMUX") == "" {
		return
	}
	host := hostPane(m.pane)
	if host == "" {
		return
	}
	next, flag := flipAxis(m.splitAxis)
	out, err := exec.Command("tmux", "display-message", "-p", "#{pane_id}").Output()
	if err != nil {
		return
	}
	self := strings.TrimSpace(string(out))
	if err := exec.Command("tmux", "move-pane", flag, "-s", self, "-t", host).Run(); err != nil {
		return
	}
	m.splitAxis = next
	if err := exec.Command("tmux", "set-option", "-p", "-t", self, "@claude_img_axis", next).Run(); err != nil {
		tracef("set-option @claude_img_axis=%s failed: %v", next, err)
	}
}
