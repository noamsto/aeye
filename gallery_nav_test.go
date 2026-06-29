package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNeighborForKey(t *testing.T) {
	cases := map[string]string{"ctrl+h": "left", "ctrl+j": "down", "ctrl+k": "up", "ctrl+l": "right"}
	for key, want := range cases {
		if got, ok := neighborForKey(key); !ok || got != want {
			t.Errorf("neighborForKey(%q) = %q,%v; want %q,true", key, got, ok, want)
		}
	}
	if _, ok := neighborForKey("h"); ok {
		t.Error("neighborForKey(\"h\") should be false (bare h is gallery nav)")
	}
}

func TestKittyNeighbor(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		if shell, err = exec.LookPath("bash"); err != nil {
			t.Skip("no sh/bash available for stub")
		}
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	stub := "#!" + shell + "\necho \"$*\" >>\"" + log + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "kitty"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// No KITTY_LISTEN_ON → must be a no-op (stub never invoked).
	t.Setenv("KITTY_LISTEN_ON", "")
	kittyNeighbor("left")
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Error("kittyNeighbor ran kitty without KITTY_LISTEN_ON set")
	}

	// With KITTY_LISTEN_ON → calls `kitty @ action neighboring_window <dir>`.
	t.Setenv("KITTY_LISTEN_ON", "unix:/tmp/kitty-test")
	kittyNeighbor("right")
	out, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("kitty stub not invoked: %v", err)
	}
	if got := string(out); got != "@ action neighboring_window right\n" {
		t.Errorf("kitty args = %q; want @ action neighboring_window right", got)
	}
}
