package main

import (
	"os/exec"
	"strconv"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestWatchOwnerFiresOnExit is the core behavior: the watcher must stay quiet
// while the owner runs and fire once it is killed.
func TestWatchOwnerFiresOnExit(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	gone := watchOwner(pid)
	select {
	case <-gone:
		t.Fatal("watcher fired before the owner was killed")
	case <-time.After(300 * time.Millisecond):
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill owner: %v", err)
	}
	_ = cmd.Wait()

	select {
	case <-gone:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not fire after the owner was killed")
	}
}

// TestWatchOwnerAlreadyGone covers a pid that is dead before the watcher starts:
// the carousel must not open and linger.
func TestWatchOwnerAlreadyGone(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start true: %v", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait()

	gone := watchOwner(pid)
	select {
	case <-gone:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not fire for an already-dead pid")
	}
}

// TestOwnerWatchNoPid pins the "no pid -> no watch" contract: absent, empty,
// non-numeric, zero, and negative values all start no watcher.
func TestOwnerWatchNoPid(t *testing.T) {
	for _, raw := range []string{"", "abc", "0", "-1", "1.5", " 12"} {
		t.Run("AEYE_OWNER_PID="+raw, func(t *testing.T) {
			t.Setenv("AEYE_OWNER_PID", raw)
			if got := ownerWatch(); got != nil {
				t.Fatalf("ownerWatch() = %v channel for %q; want nil", got, raw)
			}
		})
	}
}

// TestOwnerWatchValidPid covers the "pid set -> watch" half, and that the pid is
// parsed out of the environment.
func TestOwnerWatchValidPid(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	t.Setenv("AEYE_OWNER_PID", strconv.Itoa(cmd.Process.Pid))

	if got := ownerWatch(); got == nil {
		t.Fatal("ownerWatch() = nil for a valid pid; want a channel")
	}
}

// TestOwnerExitedQuitsProgram pins the program-side half of the fix: the owner
// exit message must quit through the normal tea.Quit path, or the viewer would
// stay up (and orphan itself) despite the watcher firing correctly.
func TestOwnerExitedQuitsProgram(t *testing.T) {
	_, cmd := galleryModel{}.handle(ownerExitedMsg{})
	if cmd == nil {
		t.Fatal("handle(ownerExitedMsg{}) returned nil cmd; want tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("handle(ownerExitedMsg{}) cmd() = %T; want tea.QuitMsg", cmd())
	}
}

// TestOwnerExitCmd covers the command that connects the watcher to the message:
// it stays parked until the channel closes, then delivers ownerExitedMsg.
func TestOwnerExitCmd(t *testing.T) {
	gone := make(chan struct{})
	cmd := ownerExitCmd(gone)
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case <-done:
		t.Fatal("ownerExitCmd fired before the owner channel closed")
	case <-time.After(100 * time.Millisecond):
	}
	close(gone)
	select {
	case msg := <-done:
		if _, ok := msg.(ownerExitedMsg); !ok {
			t.Fatalf("ownerExitCmd delivered %T; want ownerExitedMsg", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ownerExitCmd did not deliver after the owner channel closed")
	}
}

// TestOwnerWatchIgnoredWhenBridged pins that the remote bridge, where the viewer
// is not necessarily on the agent's host, neither passes nor honours the pid.
func TestOwnerWatchIgnoredWhenBridged(t *testing.T) {
	t.Setenv("AEYE_OWNER_PID", "12345")
	t.Setenv("AEYE_BRIDGED", "1")
	if got := ownerWatch(); got != nil {
		t.Fatalf("ownerWatch() = %v channel while bridged; want nil", got)
	}
}
