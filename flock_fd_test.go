//go:build !windows

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestFlockFDHelper(t *testing.T) {
	if os.Getenv("AEYE_FLOCK_HELPER") == "" {
		t.Skip("subprocess helper")
	}
	// The harness hands the lock file down as fd 3 (first ExtraFiles entry).
	os.Exit(runFlockFD(3, os.Getenv("AEYE_FLOCK_HELPER") == "nb", os.Stderr))
}

func flockChild(t *testing.T, f *os.File, mode string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestFlockFDHelper$") //nolint:gosec // re-exec of this test binary
	cmd.Env = append(os.Environ(), "AEYE_FLOCK_HELPER="+mode)
	cmd.ExtraFiles = []*os.File{f}
	err := cmd.Run()
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	ok := errors.As(err, &ee)
	if !ok {
		t.Fatal(err)
	}
	return ee.ExitCode()
}

// The lock must survive the locking process: it belongs to the open file
// description the parent still holds, so a second description is refused until
// the parent closes its fd.
func TestFlockFDLockOutlivesProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.lock")
	holder, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // test temp path
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // test temp path
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()

	if code := flockChild(t, holder, "block"); code != 0 {
		t.Fatalf("lock child exited %d, want 0", code)
	}
	if code := flockChild(t, other, "nb"); code != 1 {
		t.Fatalf("second nb lock exited %d, want 1 while first fd is open", code)
	}
	if err := holder.Close(); err != nil {
		t.Fatal(err)
	}
	if code := flockChild(t, other, "nb"); code != 0 {
		t.Fatalf("nb lock exited %d, want 0 after first fd closed", code)
	}
}

func TestFlockFDBadFD(t *testing.T) {
	var stderr bytes.Buffer
	if code := runFlockFD(1<<20, false, &stderr); code != 2 {
		t.Fatalf("bad fd exited %d, want 2", code)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("fd "+strconv.Itoa(1<<20))) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
