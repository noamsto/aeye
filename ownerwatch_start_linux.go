//go:build linux

package main

import (
	"os"
	"strconv"
	"strings"
)

// processStartToken reads /proc/<pid>/stat's starttime (field 22), a value that
// is stable for a process and distinct across pid reuse. ok=false when the file
// is unreadable or malformed.
func processStartToken(pid int) (uint64, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	// comm is wrapped in parentheses and may itself contain spaces and
	// parentheses, so the fields after the LAST ')' are the reliable ones. They
	// start at field 3 (state), making starttime index 19.
	i := strings.LastIndexByte(string(data), ')')
	if i < 0 {
		return 0, false
	}
	fields := strings.Fields(string(data[i+1:]))
	if len(fields) < 20 {
		return 0, false
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, false
	}
	return start, true
}
