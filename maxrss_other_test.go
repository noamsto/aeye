//go:build !linux

package main

func childMaxRSSKiB() (int64, bool) { return 0, false }
