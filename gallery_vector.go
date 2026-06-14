package main

import (
	"crypto/sha1"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
)

// vectorTargetW is the width to rasterize the whole SVG canvas at so the kept
// crop fills the preview box. Bounded by preview-box × zoom (≈ 1/min crop side),
// independent of the source's intrinsic resolution. A small over-estimate keeps
// both axes sharp.
func vectorTargetW(boxW, boxH int, c cropFrac) int {
	minSide := math.Min(c.w(), c.h())
	if minSide < 1e-6 {
		minSide = 1e-6
	}
	px := float64(boxW)
	if boxH > boxW {
		px = float64(boxH)
	}
	return int(math.Ceil(px / minSide))
}

// renderVector rasterizes the SVG to a scratch PNG at targetW pixels wide via
// resvg, caching on (vector path, mtime, targetW). Returns the PNG path, or ""
// if resvg is absent or the render fails (caller falls back to the bitmap crop).
func renderVector(vector string, targetW int) string {
	fi, err := os.Stat(vector)
	if err != nil {
		return ""
	}
	bin := os.Getenv("AGENT_CAROUSEL_RESVG")
	if bin == "" {
		bin = "resvg"
	}
	key := fmt.Sprintf("%s|%d|%d", vector, fi.ModTime().UnixNano(), targetW)
	out := filepath.Join(os.TempDir(), fmt.Sprintf("agent-carousel-vec-%x.png", sha1.Sum([]byte(key))))
	if _, err := os.Stat(out); err == nil {
		return out
	}
	if _, err := exec.LookPath(bin); err != nil {
		return ""
	}
	if err := exec.Command(bin, "--width", fmt.Sprint(targetW), vector, out).Run(); err != nil {
		os.Remove(out)
		return ""
	}
	return out
}
