package main

// dragHelper picks an external GUI drag-source program and the args that precede
// the file path, or an empty name when none is on PATH. ripdrag and dragon are
// Linux-only GTK tools that pop a small window the user drags out of; prefer
// ripdrag (actively maintained), then dragon.
func dragHelper() (string, []string) {
	if _, ok := lookPath("ripdrag"); ok {
		return "ripdrag", nil
	}
	if _, ok := lookPath("dragon"); ok {
		return "dragon", []string{"-x"} // -x: exit after one drop
	}
	return "", nil
}
