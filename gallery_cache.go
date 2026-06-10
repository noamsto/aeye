package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"

	_ "image/gif"  // register GIF decoder for image.Decode
	_ "image/jpeg" // register JPEG decoder for image.Decode

	_ "golang.org/x/image/webp" // register WebP decoder for image.Decode
)

// cellPxW/cellPxH estimate a terminal cell's pixel size. kitty rescales the
// transmitted image to the cell box regardless, so these only set how many
// source pixels we keep — sharpness traded against decode cost — not the
// on-screen size.
const (
	cellPxW = 10
	cellPxH = 20
)

var imgCacheDir = filepath.Join(os.TempDir(), "lazytmux-imgcache")

// cachedPNG returns a path safe to hand to kitty's f=100 (PNG-only) transmit.
// It decodes srcPath (png/jpeg/gif/webp), downscales it to fit cols×rows cells
// (never upscaling), and writes a PNG cached by path+mtime+size+target. kitty
// then decodes a small pre-scaled PNG instead of re-reading the full-resolution
// original on every navigation. Falls back to srcPath on any failure — already
// a PNG that's small enough is returned untouched (no transcode).
func cachedPNG(srcPath string, cols, rows int) string {
	fi, err := os.Stat(srcPath)
	if err != nil {
		return srcPath
	}
	tw, th := cols*cellPxW, rows*cellPxH
	key := fmt.Sprintf("%s|%d|%d|%dx%d", srcPath, fi.ModTime().UnixNano(), fi.Size(), tw, th)
	sum := sha1.Sum([]byte(key))
	out := filepath.Join(imgCacheDir, hex.EncodeToString(sum[:])+".png")
	if _, err := os.Stat(out); err == nil {
		return out
	}

	f, err := os.Open(srcPath)
	if err != nil {
		return srcPath
	}
	defer f.Close()
	src, format, err := image.Decode(f)
	if err != nil {
		return srcPath
	}

	b := src.Bounds()
	scale := min(float64(tw)/float64(b.Dx()), float64(th)/float64(b.Dy()))
	if scale >= 1 {
		if format == "png" {
			return srcPath // already a PNG no larger than the cell box
		}
		return writePNG(out, src, srcPath) // small but wrong format: transcode only
	}

	dst := image.NewRGBA(image.Rect(0, 0, int(float64(b.Dx())*scale), int(float64(b.Dy())*scale)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return writePNG(out, dst, srcPath)
}

// writePNG encodes img to out atomically (temp file + rename) and returns out,
// or fallback if the cache can't be written.
func writePNG(out string, img image.Image, fallback string) string {
	if err := os.MkdirAll(imgCacheDir, 0o755); err != nil {
		return fallback
	}
	tmp, err := os.CreateTemp(imgCacheDir, "tmp-*.png")
	if err != nil {
		return fallback
	}
	if err := png.Encode(tmp, img); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fallback
	}
	tmp.Close()
	if err := os.Rename(tmp.Name(), out); err != nil {
		os.Remove(tmp.Name())
		return fallback
	}
	return out
}
