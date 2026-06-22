package main

import "os/exec"

// dragHelper picks an external GUI drag-source program and the args that precede
// the file path, or an empty name when none is on PATH. ripdrag and dragon are
// Linux-only GTK tools that pop a small window the user drags out of; prefer
// ripdrag (actively maintained), then dragon.
func dragHelper() (string, []string) {
	if _, ok := lookPath("ripdrag"); ok {
		return "ripdrag", nil
	}
	// mwh/dragon ships under different binary names: upstream "dragon", but
	// Debian and nixpkgs rename it to "dragon-drop" and also provide "xdragon".
	for _, d := range []string{"dragon", "dragon-drop", "xdragon"} {
		if _, ok := lookPath(d); ok {
			return d, []string{"-x"} // -x: exit after one drop
		}
	}
	return "", nil
}

// runDragHelper launches the GUI drag-source for path, detached so it doesn't
// block the TUI (the helper holds its window open until the user drops). Returns
// the helper's name for the status line, or "" when none is installed. Mirrors
// openSelected's detached exec.Command(...).Start().
func runDragHelper(path string) string {
	name, args := dragHelper()
	if name == "" {
		return ""
	}
	_ = exec.Command(name, append(args, path)...).Start()
	return name
}
