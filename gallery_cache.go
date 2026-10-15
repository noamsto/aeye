package main

import (
	"context"
	"crypto/sha1" //nolint:gosec // sha1 only names cache files; not a security use
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sync"

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

var imgCacheDir = filepath.Join(os.TempDir(), "aeye-imgcache")

// cachedPNG returns a path safe to hand to kitty's f=100 (PNG-only) transmit.
// It decodes srcPath (png/jpeg/gif/webp), downscales it to fit cols×rows cells
// (never upscaling), and writes a PNG cached by path+mtime+size+target. kitty
// then decodes a small pre-scaled PNG instead of re-reading the full-resolution
// original on every navigation. Falls back to srcPath on any failure — already
// a PNG that's small enough is returned untouched (no transcode).
func cachedPNG(srcPath string, cols, rows int) string {
	return cachedPNGCtx(context.Background(), srcPath, cols, rows)
}

// pngCacheName is the cache file cachedPNG uses for srcPath at cols×rows cells.
// ok is false when srcPath can't be stat'ed, where cachedPNG returns srcPath.
func pngCacheName(srcPath string, cols, rows int) (out string, ok bool) {
	fi, err := os.Stat(srcPath)
	if err != nil {
		return "", false
	}
	tw, th := cols*cellPxW, rows*cellPxH
	key := fmt.Sprintf("%s|%d|%d|%dx%d", srcPath, fi.ModTime().UnixNano(), fi.Size(), tw, th)
	sum := sha1.Sum([]byte(key)) //nolint:gosec // sha1 only names cache files; not a security use
	return filepath.Join(imgCacheDir, hex.EncodeToString(sum[:])+".png"), true
}

// cacheFailKind classifies why a thumbnail transcode did not yield its cache
// file. Only a decode failure is sticky: it is a fact about the source bytes at
// their stamp (they don't change until the stamp does). Every other failure is
// access or resource exhaustion (EACCES/EMFILE/EIO opening the source, ENOSPC
// writing the cache file), which is transient and retried after a backoff.
type cacheFailKind int

const (
	cacheOK cacheFailKind = iota
	cacheDecodeFail
	cacheTransientFail
)

// cachedPNGCtx is cachedPNG that gives up once ctx is cancelled — before the
// decode, and again before the scale and encode that follow it — returning srcPath
// like any other failure.
func cachedPNGCtx(ctx context.Context, srcPath string, cols, rows int) string {
	out, _ := cachedPNGCtxKind(ctx, srcPath, cols, rows)
	return out
}

// cachedPNGCtxKind is cachedPNGCtx with the failure classified: cacheOK when the
// returned path is the cache file, else why it fell back to srcPath. A cancelled
// ctx is cacheOK — the caller drops it instead of recording a failure.
func cachedPNGCtxKind(ctx context.Context, srcPath string, cols, rows int) (string, cacheFailKind) {
	out, ok := pngCacheName(srcPath, cols, rows)
	if !ok {
		return srcPath, cacheTransientFail
	}
	if _, err := os.Stat(out); err == nil {
		return out, cacheOK
	}
	if ctx.Err() != nil {
		return srcPath, cacheOK
	}

	f, err := os.Open(srcPath) //nolint:gosec // path is the user's own image file
	if err != nil {
		// A source that stats but can't be opened is an access failure (EACCES,
		// EMFILE, EIO), not a fact about its bytes: retry it like any other
		// transient failure.
		return srcPath, cacheTransientFail
	}
	defer f.Close() //nolint:errcheck // read-only file; close error is irrelevant
	src, _, err := image.Decode(ctxReader{ctx, f})
	if err != nil {
		return srcPath, cacheDecodeFail
	}
	if ctx.Err() != nil {
		return srcPath, cacheOK
	}

	b := src.Bounds()
	scale := min(float64(cols*cellPxW)/float64(b.Dx()), float64(rows*cellPxH)/float64(b.Dy()))
	if scale >= 1 {
		// Small enough to send as-is, but still write a viewer-owned copy rather
		// than returning srcPath: kitty fetches the transmit path asynchronously
		// (t=f), so handing it the live manifest path races a hook deleting or
		// re-rendering that file (a d2 prune, GC, or an edited diagram) before the
		// fetch lands — which paints an empty cell. The cache dir is ours; the
		// capture hooks never sweep it.
		return cachedPNGWrite(out, src, srcPath)
	}

	dst := image.NewRGBA(image.Rect(0, 0, int(float64(b.Dx())*scale), int(float64(b.Dy())*scale)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	if ctx.Err() != nil {
		return srcPath, cacheOK
	}
	return cachedPNGWrite(out, dst, srcPath)
}

// cachedPNGWrite writes img to out and classifies a miss: a write that did not
// land is transient, not a fact about the source.
func cachedPNGWrite(out string, img image.Image, fallback string) (string, cacheFailKind) {
	if got := writePNG(out, img, fallback); got == out {
		return out, cacheOK
	}
	return fallback, cacheTransientFail
}

// warmGate serializes warm passes so overlapping reloads don't transcode the
// same image concurrently. Queued passes are near-instant (already-warm sizes
// short-circuit), so this bounds duplicate work without dropping the latest.
var warmGate sync.Mutex

// warmCacheAsync runs warmCache in a goroutine, off the bubbletea loop.
func warmCacheAsync(paths []string, previewW, previewH, stripW, stripH int) {
	go func() {
		warmGate.Lock()
		defer warmGate.Unlock()
		warmCache(paths, previewW, previewH, stripW, stripH)
	}()
}

// warmCache transcodes each path at the preview and strip cell sizes so a
// post-capture navigation hits the cache instead of decoding the full-resolution
// original on the bubbletea loop. cachedPNG is idempotent and concurrency-safe
// (atomic temp+rename), so already-warm sizes short-circuit and a warm racing a
// concurrent transmitView is harmless.
func warmCache(paths []string, previewW, previewH, stripW, stripH int) {
	for _, p := range paths {
		cachedPNG(p, previewW, previewH)
		cachedPNG(p, stripW, stripH)
	}
}

// fastPNG trades file size for encode speed — used for the interactive zoom
// scratch, where a new PNG is produced on every pan/zoom keystroke.
var fastPNG = png.Encoder{CompressionLevel: png.BestSpeed}

// writePNG encodes img to out atomically (temp file + rename) and returns out,
// or fallback if the cache can't be written.
func writePNG(out string, img image.Image, fallback string) string {
	return writePNGEnc(out, img, fallback, png.Encode)
}

// writePNGEnc is writePNG with a caller-chosen encoder (default vs fast).
func writePNGEnc(out string, img image.Image, fallback string, encode func(io.Writer, image.Image) error) string {
	if err := os.MkdirAll(imgCacheDir, 0o755); err != nil { //nolint:gosec // cache dir holds user-viewable images, not secrets
		return fallback
	}
	tmp, err := os.CreateTemp(imgCacheDir, "tmp-*.png")
	if err != nil {
		return fallback
	}
	if err := encode(tmp, img); err != nil {
		tmp.Close()           //nolint:errcheck,gosec // already failing; the write error wins
		os.Remove(tmp.Name()) //nolint:errcheck,gosec // best-effort temp cleanup
		return fallback
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name()) //nolint:errcheck,gosec // best-effort temp cleanup
		return fallback
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		os.Remove(tmp.Name()) //nolint:errcheck,gosec // best-effort temp cleanup
		return fallback
	}
	return out
}

// writeRaw dumps an RGBA image's pixels to out with no encoding, via a temp file
// renamed into place so a reader never sees a partial frame. Returns "" on any
// failure, leaving the caller to fall back to an encoded write.
//
// This is the pan/zoom hot path: kitty accepts raw RGBA (f=32) and skipping the PNG
// encode measured 2.5x faster per frame. img.Pix is already tightly packed RGBA for
// an image created at its own bounds, which is what cropRaster produces.
func writeRaw(out string, img *image.RGBA) string {
	if img.Stride != img.Bounds().Dx()*4 {
		return "" // a sub-image view; not tightly packed, so not safe to dump
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil { //nolint:gosec // cache dir holds user-viewable images, not secrets
		return ""
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), "raw-*")
	if err != nil {
		return ""
	}
	if _, err := tmp.Write(img.Pix); err != nil {
		tmp.Close()           //nolint:errcheck,gosec // already failing; the write error wins
		os.Remove(tmp.Name()) //nolint:errcheck,gosec // best-effort temp cleanup
		return ""
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name()) //nolint:errcheck,gosec // best-effort temp cleanup
		return ""
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		os.Remove(tmp.Name()) //nolint:errcheck,gosec // best-effort temp cleanup
		return ""
	}
	return out
}
