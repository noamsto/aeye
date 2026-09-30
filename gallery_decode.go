package main

import (
	"image"
	"os"
)

// imageSize reads the pixel size from path's header without decoding pixels.
// Zero when the file is missing or its header is unreadable.
func imageSize(path string) image.Point {
	f, err := os.Open(path) //nolint:gosec // path is the user's own image/manifest file
	if err != nil {
		return image.Point{}
	}
	defer f.Close() //nolint:errcheck // read-only file or already-failing path; close error is not actionable
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return image.Point{}
	}
	return image.Pt(cfg.Width, cfg.Height)
}
