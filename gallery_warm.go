package main

import (
	"context"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
)

// warmDebounce is how long a layout must hold still before its thumbnails are
// transcoded: a border drag changes the layout every few milliseconds, and only
// the size it settles on is worth the work.
var warmDebounce = 150 * time.Millisecond

// transcodeSlots bounds concurrent transcodes. Each holds a full-resolution
// decode, so a burst of superseded fills must not all run at once.
var transcodeSlots = make(chan struct{}, 2)

type cacheJob struct {
	path       string
	cols, rows int
}

type cacheTarget struct {
	l         layout
	cursor, n int
	path      string
	mtime     int64
}

type cacheKickMsg struct {
	gen    uint64
	target cacheTarget
}

// cacheFilledMsg reports the urgent jobs (the selection and the visible strip)
// done. rest are the remaining images to warm under ctx.
type cacheFilledMsg struct {
	gen    uint64
	notify bool
	target cacheTarget
	failed map[string]cacheFailKind
	ctx    context.Context
	rest   []cacheJob
}

type cacheWarmedMsg struct{ failed map[string]cacheFailKind }

// cachedPNGOrMiss is the loop-side cachedPNG: it only looks. A miss reports
// ok=false and asks for a fill, which re-stores the view once it lands. A source
// that failed to transcode answers with itself, as cachedPNG would.
func (m *galleryModel) cachedPNGOrMiss(srcPath string, cols, rows int) (string, bool) {
	out, ok := pngCacheName(srcPath, cols, rows)
	if !ok {
		return srcPath, true
	}
	if _, err := os.Stat(out); err == nil {
		delete(m.cacheFailed, out) // a retry that landed clears its backoff record
		return out, true
	}
	if f, ok := m.cacheFailed[out]; ok {
		if f.decode || time.Now().Before(f.retryAt) {
			return srcPath, true
		}
		// A transient failure past its backoff: fall through to a miss so the
		// update arms a refill, which is how a disk that recovered gets its thumb back.
	}
	m.cacheMiss = true
	return "", false
}

func (m *galleryModel) cacheTarget() cacheTarget {
	if len(m.images) == 0 {
		return cacheTarget{l: m.l}
	}
	e := m.images[m.cursor]
	return cacheTarget{m.l, m.cursor, len(m.images), e.Path, e.Mtime}
}

func (m *galleryModel) cancelCache() {
	if m.cacheCancel != nil {
		m.cacheCancel()
		m.cacheCancel = nil
	}
}

// armCache returns the kick for a fill the last update called for, once per
// request: a loop-side cache miss, or a layout change (prev is the layout before
// the update) that left every thumbnail cold. A new request supersedes the
// running one — its kick and result arrive stale — and a layout change waits out
// warmDebounce so a resize storm transcodes only the size it settles on. The
// first sizing doesn't wait: nothing is on screen yet.
func (m *galleryModel) armCache(prev layout) tea.Cmd {
	miss := m.cacheMiss
	m.cacheMiss = false
	if (m.backend != backendKitty && m.backend != backendRaster) || !m.ready || len(m.images) == 0 {
		return nil
	}
	resized := m.l != prev
	target := m.cacheTarget()
	if !resized && miss && m.cachePending && m.cacheFor == target {
		// The pending kick is still waiting out its debounce: let it carry this miss
		// rather than superseding it with an undebounced one.
		m.cacheBusy = true
		return nil
	}
	if !resized && (!miss || (m.cacheBusy && m.cacheFor == target)) {
		return nil
	}
	m.cancelCache()
	m.cacheGen++
	m.cacheBusy, m.cachePending, m.cacheFor = miss, true, target
	g := m.cacheGen
	delay := time.Duration(0)
	if resized && prev != (layout{}) {
		delay = warmDebounce
	}
	return tea.Tick(delay, func(time.Time) tea.Msg { return cacheKickMsg{gen: g, target: target} })
}

func (m *galleryModel) startCache(msg cacheKickMsg) tea.Cmd {
	if msg.gen != m.cacheGen {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cacheCancel = cancel
	m.cachePending = false
	notify := m.cacheBusy
	urgent, rest := m.cacheJobs()
	return func() tea.Msg {
		failed := runCacheJobs(ctx, urgent)
		return cacheFilledMsg{gen: msg.gen, notify: notify, target: msg.target, failed: failed, ctx: ctx, rest: rest}
	}
}

// cacheFilled lands the urgent jobs. Failures are facts about files and are kept
// whatever generation reports them; only the current generation goes on to warm
// the rest, and only while the selection and layout are still what it filled for
// does it re-store the view.
func (m *galleryModel) cacheFilled(msg cacheFilledMsg) tea.Cmd {
	m.noteCacheFailed(msg.failed)
	if msg.gen != m.cacheGen {
		return nil
	}
	m.cacheBusy = false
	cmds := []tea.Cmd{func() tea.Msg { return cacheWarmedMsg{failed: runCacheJobs(msg.ctx, msg.rest)} }}
	if msg.notify && msg.target == m.cacheTarget() {
		// restoreView re-stores the bitmap over any sharp d2 frame, so render it again.
		cmds = append(cmds, m.restoreView(), m.kickVector())
	}
	return tea.Batch(cmds...)
}

// cacheFailure records a thumbnail the cache could not produce. decode marks a
// fact about the source (its bytes won't change at the stamp), which stays sticky
// for the session; otherwise the file is requested again once retryAt passes,
// tries setting the next backoff.
type cacheFailure struct {
	decode  bool
	retryAt time.Time
	tries   int
}

const (
	// cacheWriteRetryBase and cacheWriteRetryMax bound the backoff after a
	// transient cache failure, so a full disk (or EMFILE) is retried without being
	// spun on.
	cacheWriteRetryBase = 500 * time.Millisecond
	cacheWriteRetryMax  = 30 * time.Second
)

// cacheWriteRetryDelay doubles cacheWriteRetryBase per consecutive transient
// failure, up to cacheWriteRetryMax.
func cacheWriteRetryDelay(tries int) time.Duration {
	return min(cacheWriteRetryBase<<min(max(tries, 1)-1, 8), cacheWriteRetryMax)
}

// noteCacheFailed remembers transcodes that did not yield their cache file. A
// decode failure is sticky: the source bytes won't change at its stamp, so
// cachedPNGOrMiss keeps answering with the source. Any other failure is
// transient, so it carries a backoff deadline after which the thumbnail is
// requested again.
func (m *galleryModel) noteCacheFailed(failed map[string]cacheFailKind) {
	for out, kind := range failed {
		if m.cacheFailed == nil {
			m.cacheFailed = map[string]cacheFailure{}
		}
		if kind == cacheDecodeFail {
			m.cacheFailed[out] = cacheFailure{decode: true}
			continue
		}
		f := m.cacheFailed[out]
		f.tries++
		f.retryAt = time.Now().Add(cacheWriteRetryDelay(f.tries))
		m.cacheFailed[out] = f
	}
}

// cacheJobs orders the thumbnails the view shows now — the selection at preview
// size, then the visible strip nearest the cursor first — apart from every image
// at both sizes, nearest the cursor first.
func (m *galleryModel) cacheJobs() (urgent, rest []cacheJob) {
	n := len(m.images)
	if m.images == nil || n == 0 {
		return nil, nil
	}
	urgent = append(urgent, cacheJob{m.images[m.cursor].Path, m.l.previewW, m.l.previewH})
	start := stripStart(m.cursor, m.l.stripCols, n)
	for _, i := range nearestFirst(m.cursor, n) {
		if i >= start && i < start+m.l.stripCols {
			urgent = append(urgent, cacheJob{m.images[i].Path, m.l.stripW, m.l.stripH})
		}
		rest = append(rest,
			cacheJob{m.images[i].Path, m.l.previewW, m.l.previewH},
			cacheJob{m.images[i].Path, m.l.stripW, m.l.stripH})
	}
	return urgent, rest
}

// nearestFirst lists 0..n-1 ordered by distance from c, the left neighbour first.
func nearestFirst(c, n int) []int {
	out := make([]int, 0, n)
	out = append(out, c)
	for d := 1; len(out) < n; d++ {
		if c-d >= 0 {
			out = append(out, c-d)
		}
		if c+d < n {
			out = append(out, c+d)
		}
	}
	return out
}

// runCacheJobs transcodes each missing thumbnail, stopping once ctx is
// cancelled. It returns the cache files of the jobs that failed, classified,
// which cachedPNGOrMiss then answers with the source instead of asking again.
// Cancelled work is dropped, never recorded: the classifier answers cacheOK for
// a cancelled ctx, which noteCacheFailed must not read as a transient failure.
func runCacheJobs(ctx context.Context, jobs []cacheJob) (failed map[string]cacheFailKind) {
	for _, j := range jobs {
		out, ok := pngCacheName(j.path, j.cols, j.rows)
		if !ok {
			continue
		}
		if _, err := os.Stat(out); err == nil {
			continue
		}
		select {
		case transcodeSlots <- struct{}{}:
		case <-ctx.Done():
			return failed
		}
		got, kind := cachedPNGCtxKind(ctx, j.path, j.cols, j.rows)
		<-transcodeSlots
		if ctx.Err() != nil {
			return failed
		}
		if got != out {
			if failed == nil {
				failed = map[string]cacheFailKind{}
			}
			failed[out] = kind
		}
	}
	return failed
}
