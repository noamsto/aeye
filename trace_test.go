package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetTrace() {
	if traceFile != nil {
		traceFile.Close()
	}
	traceEnabled = false
	traceFile = nil
}

func TestTraceDisabledWhenUnset(t *testing.T) {
	t.Setenv("AEYE_DEBUG", "")
	resetTrace()
	traceInit("x")
	if traceEnabled {
		t.Fatal("tracing must be disabled when AEYE_DEBUG is unset")
	}
	tracef("no-op %d", 1) // must not panic or write
}

func TestTraceWritesToExplicitPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.log")
	t.Setenv("AEYE_DEBUG", path)
	resetTrace()
	traceInit("x")
	if !traceEnabled {
		t.Fatal("tracing must be enabled for an explicit path")
	}
	tracef("hello %d", 42)
	traceFile.Sync()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "=== aeye trace") {
		t.Errorf("missing header: %q", s)
	}
	if !strings.Contains(s, "hello 42") {
		t.Errorf("missing line: %q", s)
	}
}

func TestTraceDefaultPathFromPane(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AEYE_DIR", dir)
	t.Setenv("CLAUDE_STATUS_DIR", dir)
	t.Setenv("AEYE_DEBUG", "1")
	// The state dir must exist for O_CREATE to succeed.
	if err := os.MkdirAll(filepath.Dir(manifestPath("2")), 0o755); err != nil {
		t.Fatal(err)
	}
	resetTrace()
	traceInit("2")
	want := strings.TrimSuffix(manifestPath("2"), ".jsonl") + ".trace.log"
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected trace file at %s: %v", want, err)
	}
}

func TestTraceTruncatesOnInit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.log")
	t.Setenv("AEYE_DEBUG", path)
	resetTrace()
	traceInit("x")
	tracef("first-run-line")
	traceFile.Sync()
	resetTrace()
	traceInit("x") // reopening truncates
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "first-run-line") {
		t.Errorf("expected truncation on re-init, got: %q", b)
	}
}
