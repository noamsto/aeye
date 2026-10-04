package main

import (
	"os/exec"
	"strconv"
	"testing"
	"time"
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

// TestOwnerWatchIgnoredWhenBridged pins that the remote bridge, where the viewer
// is not necessarily on the agent's host, neither passes nor honours the pid.
func TestOwnerWatchIgnoredWhenBridged(t *testing.T) {
	t.Setenv("AEYE_OWNER_PID", "12345")
	t.Setenv("AEYE_BRIDGED", "1")
	if got := ownerWatch(); got != nil {
		t.Fatalf("ownerWatch() = %v channel while bridged; want nil", got)
	}
}
