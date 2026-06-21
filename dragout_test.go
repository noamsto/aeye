package main

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeBins makes a temp dir on PATH containing empty executable files for each
// given name, so lookPath finds them without running anything real.
func fakeBins(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestRunDragHelper(t *testing.T) {
	t.Run("returns name and launches when present", func(t *testing.T) {
		fakeBins(t, "ripdrag") // the fake is a no-op #!/bin/sh that exits immediately
		if name := runDragHelper("/x/a.png"); name != "ripdrag" {
			t.Fatalf("got %q, want ripdrag", name)
		}
	})
	t.Run("empty when no helper installed", func(t *testing.T) {
		fakeBins(t)
		if name := runDragHelper("/x/a.png"); name != "" {
			t.Fatalf("got %q, want empty", name)
		}
	})
}

func TestDragHelper(t *testing.T) {
	t.Run("prefers ripdrag when both present", func(t *testing.T) {
		fakeBins(t, "ripdrag", "dragon")
		name, args := dragHelper()
		if name != "ripdrag" || len(args) != 0 {
			t.Fatalf("got (%q, %v), want ripdrag with no leading args", name, args)
		}
	})
	t.Run("falls back to dragon with -x", func(t *testing.T) {
		fakeBins(t, "dragon")
		name, args := dragHelper()
		if name != "dragon" || len(args) != 1 || args[0] != "-x" {
			t.Fatalf("got (%q, %v), want dragon -x", name, args)
		}
	})
	t.Run("empty when neither present", func(t *testing.T) {
		fakeBins(t)
		if name, _ := dragHelper(); name != "" {
			t.Fatalf("got %q, want empty", name)
		}
	})
}
