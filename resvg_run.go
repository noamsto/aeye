package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// resvgMemLimitMiB is the default memory bound for one resvg render.
	resvgMemLimitMiB = 2048
	// resvgMemEnv overrides the bound with a positive integer MiB.
	resvgMemEnv = "AEYE_RESVG_MEM_MB"
	// resvgTimeout is the wall-clock limit for one resvg render.
	resvgTimeout = 60 * time.Second

	rssPollInterval = 50 * time.Millisecond
)

// errDiagramTooLarge marks a render stopped by the memory bound or the timeout.
var errDiagramTooLarge = errors.New("diagram too large to render")

// boundMode is how runBounded enforces the memory limit.
type boundMode int

const (
	// boundRLimit caps the child's address space (RLIMIT_AS) through a shell
	// wrapper, so an oversized allocation fails instead of happening.
	boundRLimit boundMode = iota
	// boundWatch polls the child's resident size and kills it over the limit; for
	// OSes that do not enforce RLIMIT_AS on malloc/mmap (macOS).
	boundWatch
)

// resvgBin is the resvg executable: AEYE_RESVG when set, else resvg on PATH.
func resvgBin() string {
	if bin := os.Getenv("AEYE_RESVG"); bin != "" {
		return bin
	}
	return "resvg"
}

// resvgAvailable reports whether the resvg executable can be found.
func resvgAvailable() bool {
	_, err := exec.LookPath(resvgBin())
	return err == nil
}

// resvgMemLimit is the memory bound in MiB: resvgMemEnv when it is a positive
// integer, else resvgMemLimitMiB.
func resvgMemLimit() int {
	if n, err := strconv.Atoi(os.Getenv(resvgMemEnv)); err == nil && n > 0 {
		return n
	}
	return resvgMemLimitMiB
}

// runResvg runs resvg with resvgFontArgs() ahead of args, under the memory bound
// and timeout, and returns its combined output. stdin may be nil. A limit hit
// wraps errDiagramTooLarge; a cancelled ctx returns ctx's error.
func runResvg(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	path, err := exec.LookPath(resvgBin())
	if err != nil {
		return nil, fmt.Errorf("resvg: %w", err)
	}
	mode := boundWatch
	if runtime.GOOS == "linux" {
		mode = boundRLimit
	}
	out, err := runBounded(ctx, stdin, path, append(resvgFontArgs(), args...), mode, int64(resvgMemLimit())<<20, resvgTimeout)
	if err == nil || errors.Is(err, errDiagramTooLarge) || ctx.Err() != nil {
		return out, err
	}
	return out, fmt.Errorf("resvg: %w: %s", err, out)
}

// runBounded runs path with args under limitBytes of memory and timeout.
// Failures other than a limit hit or a cancelled ctx come back as the raw exec
// error.
func runBounded(ctx context.Context, stdin io.Reader, path string, args []string, mode boundMode, limitBytes int64, timeout time.Duration) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if mode == boundRLimit {
		wrapped := append([]string{"-c", `ulimit -S -v ` + strconv.FormatInt(limitBytes>>10, 10) + ` 2>/dev/null; exec "$@"`, "sh", path}, args...)
		cmd = exec.CommandContext(runCtx, "/bin/sh", wrapped...) //nolint:gosec // args built by the tool; the shell only runs the fixed ulimit/exec line
	} else {
		cmd = exec.CommandContext(runCtx, path, args...) //nolint:gosec // args built by the tool, not shell-interpreted
	}
	cmd.Stdin = stdin
	cmd.WaitDelay = 2 * time.Second
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var killed atomic.Bool
	if mode == boundWatch {
		done := make(chan struct{})
		defer close(done)
		go watchRSS(cmd.Process, limitBytes>>10, done, &killed)
	}

	err := cmd.Wait()
	if err == nil {
		return out.Bytes(), nil
	}
	limitMiB := limitBytes >> 20
	switch {
	case ctx.Err() != nil:
		return out.Bytes(), ctx.Err()
	case killed.Load():
		return out.Bytes(), tooLargeMemory(limitMiB)
	case runCtx.Err() != nil:
		return out.Bytes(), fmt.Errorf("%w (render timed out after %.0fs)", errDiagramTooLarge, timeout.Seconds())
	case mode == boundRLimit && abortedOnAlloc(err, out.Bytes()):
		return out.Bytes(), tooLargeMemory(limitMiB)
	}
	return out.Bytes(), err
}

func tooLargeMemory(limitMiB int64) error {
	return fmt.Errorf("%w (needs more than %d MiB of memory; raise the limit with %s)", errDiagramTooLarge, limitMiB, resvgMemEnv)
}

// abortedOnAlloc reports whether the child died from the SIGABRT Rust raises
// when an allocation fails, which is how resvg behaves under RLIMIT_AS.
func abortedOnAlloc(err error, out []byte) bool {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	ws, ok := ee.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled() && ws.Signal() == syscall.SIGABRT && bytes.Contains(out, []byte("memory allocation of"))
}

// watchRSS kills proc once its resident size passes limitKiB. A tick whose size
// cannot be read is skipped.
func watchRSS(proc *os.Process, limitKiB int64, done <-chan struct{}, killed *atomic.Bool) {
	tick := time.NewTicker(rssPollInterval)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
		}
		rss, err := rssKiB(proc.Pid)
		if err != nil || rss <= limitKiB {
			continue
		}
		killed.Store(true)
		_ = proc.Kill()
		return
	}
}

// rssKiB is the resident set size of pid in KiB.
func rssKiB(pid int) (int64, error) {
	if runtime.GOOS == "linux" {
		b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm") //nolint:gosec // pid of our own child
		if err != nil {
			return 0, err
		}
		fields := strings.Fields(string(b))
		if len(fields) < 2 {
			return 0, fmt.Errorf("unexpected statm %q", b)
		}
		pages, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return pages * int64(os.Getpagesize()) >> 10, nil
	}
	b, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output() //nolint:gosec // pid of our own child
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
}
