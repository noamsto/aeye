package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Trace facility (issue #125): AEYE_DEBUG toggles a best-effort, per-launch
// trace of the carousel's first-frame lifecycle. Mirrors logDropped: a file we
// can't write is never worth failing the viewer.
var (
	traceEnabled bool
	traceFile    *os.File
	traceStart   time.Time
	traceMu      sync.Mutex
)

// traceInit resolves AEYE_DEBUG and, when set, opens the trace file (truncated
// fresh each launch) and writes a header. Called once from runGallery. pane is
// the manifest key, used for the default per-pane path.
//
//	AEYE_DEBUG=1|true|on -> <state-dir>/<key>.trace.log (beside <key>.jsonl)
//	AEYE_DEBUG=<path>    -> that file
func traceInit(pane string) {
	v := os.Getenv("AEYE_DEBUG")
	if v == "" {
		return
	}
	var path string
	switch v {
	case "1", "true", "on":
		path = strings.TrimSuffix(manifestPath(pane), ".jsonl") + ".trace.log"
	default:
		path = v
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	traceFile = f
	traceStart = time.Now()
	traceEnabled = true
	fmt.Fprintf(f, "=== aeye trace %s pid=%d version=%s ===\n",
		traceStart.Format(time.RFC3339), os.Getpid(), version())
}

// tracef appends a timestamped line when tracing is enabled; a no-op otherwise.
// Guard hot (per-message/per-frame) call sites with `if traceEnabled` so the
// variadic slice isn't allocated when tracing is off.
func tracef(format string, args ...any) {
	if !traceEnabled {
		return
	}
	traceMu.Lock()
	defer traceMu.Unlock()
	ms := time.Since(traceStart).Milliseconds()
	fmt.Fprintf(traceFile, "[+%6dms] "+format+"\n", append([]any{ms}, args...)...)
}
