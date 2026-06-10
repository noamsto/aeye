package main

import (
	"fmt"
	imgcolor "image/color"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	previewID      = 250 // kitty image id for the big preview
	stripThumbW    = 18  // filmstrip thumbnail width in cells
	stripGutter    = 1   // blank columns between filmstrip thumbs (borders add separation)
	previewBoxCols = 355 // preview box cols per 100 rows (~16:9 given ~1:2.1 cells)

	galleryTitleIcon = "󰋩" // nerd: nf-md-image_multiple
)

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// layout is the carousel geometry for a given pane size.
type layout struct {
	previewW, previewH int
	stripW, stripH     int
	stripCols          int // visible filmstrip thumbnails
}

// computeLayout splits the pane into a big preview on top and a filmstrip row
// above a one-line status bar. The preview is the largest ~16:9 box that fits
// the area left over after the filmstrip (so landscape images barely letterbox).
func computeLayout(paneW, paneH int) layout {
	stripH := clamp(paneH/4, 5, 12)
	stripW := stripThumbW
	// +2 per thumb for its border frame.
	stripCols := clamp((paneW+stripGutter)/(stripW+2+stripGutter), 1, maxCellDim)

	// Area left for the preview after title(1) + subtitle(1) +
	// filmstrip(stripH+2 border) + hints(1), minus 2 for the preview border.
	availW := clamp(paneW-2, 1, maxCellDim)
	availH := clamp(paneH-stripH-7, 1, maxCellDim)

	// Largest box with cols:rows ≈ previewBoxCols/100 that fits availW × availH.
	previewW := availW
	previewH := availW * 100 / previewBoxCols
	if previewH > availH {
		previewH = availH
		previewW = clamp(availH*previewBoxCols/100, 1, availW)
	}
	previewW = clamp(previewW, 1, maxCellDim)
	previewH = clamp(previewH, 1, maxCellDim)
	return layout{previewW: previewW, previewH: previewH, stripW: stripW, stripH: stripH, stripCols: stripCols}
}

// stripStart is the first filmstrip index for a window centered on cursor.
func stripStart(cursor, stripCols, n int) int {
	if n <= stripCols {
		return 0
	}
	return clamp(cursor-stripCols/2, 0, n-stripCols)
}

// ---------------------------------------------------------------------------
// Gallery bubbletea model — carousel (big preview + filmstrip)
// ---------------------------------------------------------------------------

type galleryModel struct {
	pane    string
	images  []imageEntry
	backend gridBackend
	theme   string
	l       layout
	cursor  int      // selected image index
	width   int
	height  int
	tty     *os.File // raw graphics sink (bypasses bubbletea's stdout)
	mtime   int64    // manifest mtime at last load (for auto-refresh)
	ready   bool

	// Theme colors, resolved once at startup (tmux options are session-invariant).
	selColor, dimColor, hintFg, textFg imgcolor.Color
}

func (m galleryModel) Init() tea.Cmd { return galleryTickCmd() }

type galleryTickMsg struct{}

// galleryTickCmd polls the manifest so the carousel auto-refreshes while the
// plugin hook appends new images.
func galleryTickCmd() tea.Cmd {
	return tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg { return galleryTickMsg{} })
}

// transmitView stores the preview + visible filmstrip images store-only (kitty
// backend). Writes to /dev/tty so the APC bytes never interleave with
// bubbletea's frame output.
func (m *galleryModel) transmitView() {
	if m.backend != backendKitty || m.tty == nil || len(m.images) == 0 {
		return
	}
	fmt.Fprint(m.tty, deleteAll())
	fmt.Fprint(m.tty, transmitVirtual(previewID, cachedPNG(m.images[m.cursor].Path, m.l.previewW, m.l.previewH), m.l.previewW, m.l.previewH))
	start := stripStart(m.cursor, m.l.stripCols, len(m.images))
	for s := 0; s < m.l.stripCols; s++ {
		idx := start + s
		if idx >= len(m.images) {
			break
		}
		fmt.Fprint(m.tty, transmitVirtual(s+1, cachedPNG(m.images[idx].Path, m.l.stripW, m.l.stripH), m.l.stripW, m.l.stripH))
	}
}

// selectIndex moves the selection (clamped) and re-transmits.
func (m *galleryModel) selectIndex(idx int) {
	idx = clamp(idx, 0, max(0, len(m.images)-1))
	if idx != m.cursor {
		m.cursor = idx
		m.transmitView()
	}
}

func (m *galleryModel) reload() {
	m.mtime = manifestMtime(m.pane)
	m.images = loadManifest(m.pane)
	m.cursor = clamp(m.cursor, 0, max(0, len(m.images)-1))
	if m.ready {
		m.l = computeLayout(m.width, m.height)
		m.transmitView()
	}
}

func (m galleryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.l = computeLayout(m.width, m.height)
		m.ready = true
		m.transmitView()
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "right", "l", "down", "j":
			m.selectIndex(m.cursor + 1)
		case "left", "h", "up", "k":
			m.selectIndex(m.cursor - 1)
		case "n":
			m.selectIndex(m.cursor + m.l.stripCols)
		case "p":
			m.selectIndex(m.cursor - m.l.stripCols)
		case "g", "home":
			m.selectIndex(0)
		case "G", "end":
			m.selectIndex(len(m.images) - 1)
		case "o", "enter":
			m.openSelected("")
		case "O":
			m.openSelected("dir")
		case "r":
			m.reload()
		default:
			if d := digitKey(msg.String()); d >= 1 && d-1 < len(m.images) {
				m.selectIndex(d - 1)
			}
		}
	case galleryTickMsg:
		if mt := manifestMtime(m.pane); mt != m.mtime {
			m.reload()
		}
		return m, galleryTickCmd()
	}
	return m, nil
}

// openSelected launches the default app for the selected image (mode "") or its
// containing folder (mode "dir"), detached so it doesn't block the TUI.
func (m galleryModel) openSelected(mode string) {
	if len(m.images) == 0 {
		return
	}
	target := m.images[m.cursor].Path
	if mode == "dir" {
		target = filepath.Dir(target)
	}
	_ = exec.Command("xdg-open", target).Start()
}

func (m galleryModel) View() tea.View {
	content := "Loading..."
	if m.ready {
		content = m.renderView()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m galleryModel) renderView() string {
	if len(m.images) == 0 {
		return "no images yet"
	}

	selColor, dimColor := m.selColor, m.dimColor

	// Big preview of the selected image, framed and centered above the filmstrip.
	var preview string
	if m.backend == backendKitty {
		preview = placeholderBlock(previewID, m.l.previewW, m.l.previewH)
	} else {
		preview = symbolsBlock(m.images[m.cursor].Path, m.l.previewW, m.l.previewH)
	}
	preview = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(selColor).Render(preview)
	previewH := m.height - m.l.stripH - 5 // title + subtitle + filmstrip(stripH+2) + hints
	previewArea := lipgloss.Place(m.width, previewH, lipgloss.Center, lipgloss.Center, preview)

	// Centered title + subtitle (current image).
	hintFg, textFg := m.hintFg, m.textFg
	center := func(s string) string { return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, s) }
	title := center(lipgloss.NewStyle().Foreground(selColor).Bold(true).Render(galleryTitleIcon + "  Claude Images"))
	subtitle := center(lipgloss.NewStyle().Foreground(textFg).Render(
		truncateToWidth(fmt.Sprintf("[%d/%d]  %s", m.cursor+1, len(m.images), filepath.Base(m.images[m.cursor].Path)), m.width)))

	// Filmstrip window: each thumb framed; the selected thumb's frame is colored.
	start := stripStart(m.cursor, m.l.stripCols, len(m.images))
	hgap := lipgloss.NewStyle().Width(stripGutter).Height(m.l.stripH + 2).Render("")
	var cells []string
	for s := 0; s < m.l.stripCols; s++ {
		idx := start + s
		if idx >= len(m.images) {
			break
		}
		if s > 0 {
			cells = append(cells, hgap)
		}
		var thumb string
		if m.backend == backendKitty {
			thumb = placeholderBlock(s+1, m.l.stripW, m.l.stripH)
		} else {
			thumb = symbolsBlock(m.images[idx].Path, m.l.stripW, m.l.stripH)
		}
		border := dimColor
		if idx == m.cursor {
			border = selColor
		}
		cells = append(cells, lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(border).Render(thumb))
	}
	filmstrip := lipgloss.PlaceHorizontal(m.width, lipgloss.Center,
		lipgloss.JoinHorizontal(lipgloss.Top, cells...))

	// Centered key hints at the bottom.
	hints := center(lipgloss.NewStyle().Foreground(hintFg).Render(
		"↵/o open · O folder · h/l move · n/p page · q quit"))

	return lipgloss.JoinVertical(lipgloss.Left, title, subtitle, previewArea, filmstrip, hints)
}

// truncateToWidth cuts s to at most w display columns (ASCII-safe; bar text is
// filenames + hints, no wide runes).
func truncateToWidth(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 0 {
		return ""
	}
	return s[:w]
}

// thmColor reads a tmux @thm_* color option, falling back per theme.
func (m galleryModel) thmColor(opt, dark, light string) imgcolor.Color {
	out, err := exec.Command("tmux", "show", "-gv", opt).Output()
	if err == nil {
		if v := strings.TrimSpace(string(out)); v != "" {
			return lipgloss.Color(v)
		}
	}
	if m.theme == "light" {
		return lipgloss.Color(light)
	}
	return lipgloss.Color(dark)
}

// runGallery is the entry point dispatched from main for `--gallery <pane>`.
func runGallery(pane string) error {
	tty, _ := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	m := galleryModel{
		pane:    pane,
		images:  loadManifest(pane),
		backend: chooseGridBackend(termName()),
		theme:   detectTheme(),
		tty:     tty,
		mtime:   manifestMtime(pane),
	}
	// Resolve theme colors once (each is a tmux subprocess; don't do it per frame).
	m.selColor = m.thmColor("@thm_mauve", "#cba6f7", "#8839ef")
	m.dimColor = m.thmColor("@thm_surface_1", "#45475a", "#bcc0cc")
	m.hintFg = m.thmColor("@thm_subtext_0", "#a6adc8", "#6c6f85")
	m.textFg = m.thmColor("@thm_text", "#cdd6f4", "#4c4f69")
	// Teardown on pane-kill (toggle-off SIGTERM/SIGHUP), not just q.
	if tty != nil {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP)
		go func() {
			<-sig
			fmt.Fprint(tty, deleteAll())
			os.Exit(0)
		}()
	}
	_, err := tea.NewProgram(m).Run()
	if tty != nil {
		fmt.Fprint(tty, deleteAll())
		_ = tty.Close()
	}
	return err
}

// termName returns the outer client terminal name (for backend selection).
func termName() string {
	out, err := exec.Command("tmux", "display-message", "-p", "#{client_termname}").Output()
	if err != nil {
		return os.Getenv("TERM")
	}
	return strings.TrimSpace(string(out))
}

// manifestMtime returns the manifest file's mtime in ns, or 0 if absent.
func manifestMtime(pane string) int64 {
	fi, err := os.Stat(manifestPath(pane))
	if err != nil {
		return 0
	}
	return fi.ModTime().UnixNano()
}

// digitKey maps "1".."9" to 1..9, else 0.
func digitKey(s string) int {
	if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		return int(s[0] - '0')
	}
	return 0
}
