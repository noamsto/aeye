package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// imageEntry is one line of the images/<pane>.jsonl manifest (shared format
// with the v1 viewer; only the fields the grid needs are decoded).
type imageEntry struct {
	Path   string `json:"path"`
	Source string `json:"source"`
	Vector string `json:"vector,omitempty"`
}

// parseManifest decodes JSONL bytes into entries, skipping blank/unparseable
// lines and entries with no path.
func parseManifest(data []byte) []imageEntry {
	var out []imageEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e imageEntry
		if json.Unmarshal([]byte(line), &e) != nil || e.Path == "" {
			continue
		}
		out = append(out, e)
	}
	return out
}

// manifestPath is images/<pane>.jsonl (leading % stripped) under the carousel
// status dir (AGENT_CAROUSEL_DIR, then CLAUDE_STATUS_DIR, then /tmp/claude-status).
func manifestPath(pane string) string {
	dir := os.Getenv("AGENT_CAROUSEL_DIR")
	if dir == "" {
		dir = os.Getenv("CLAUDE_STATUS_DIR")
	}
	if dir == "" {
		dir = "/tmp/claude-status"
	}
	return dir + "/images/" + strings.TrimPrefix(pane, "%") + ".jsonl"
}

// loadManifest reads and parses the pane's image manifest, dropping entries
// that don't decode as an image so neither a deleted file nor a truncated stub
// (e.g. a failed diagram render that left a few bytes on disk) renders as a
// blank cell. A path that becomes valid later is picked up on the next reload.
func loadManifest(pane string) []imageEntry {
	data, err := os.ReadFile(manifestPath(pane))
	if err != nil {
		return nil
	}
	entries := parseManifest(data)
	out := entries[:0]
	for _, e := range entries {
		if err := decodeErr(e.Path); err != nil {
			logDropped(pane, e.Path, err)
			continue
		}
		out = append(out, e)
	}
	return out
}

// decodeErr returns nil if path's header decodes as a registered image format.
// DecodeConfig reads only the header (cheap), so this validates every manifest
// entry on load without paying a full decode.
func decodeErr(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, _, err = image.DecodeConfig(f)
	return err
}

// dropped tracks paths already logged this process so a stale entry isn't
// re-logged on every reload — each new image bumps the manifest mtime, which
// re-checks the whole list.
var dropped = struct {
	sync.Mutex
	seen map[string]bool
}{seen: map[string]bool{}}

// logDropped records, once per path per process, why a manifest entry was
// skipped — observability for "where did my image go?" without cluttering the
// carousel. Best-effort: a log we can't write isn't worth failing a reload.
func logDropped(pane, path string, reason error) {
	dropped.Lock()
	defer dropped.Unlock()
	if dropped.seen[path] {
		return
	}
	dropped.seen[path] = true
	logPath := filepath.Join(filepath.Dir(manifestPath(pane)), "dropped.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s\t%s\t%s\n", time.Now().Format(time.RFC3339), path, reason)
}

type gridBackend int

const (
	backendKitty gridBackend = iota
	backendSymbols
)

// chooseGridBackend picks the grid renderer from the OUTER terminal name only.
// The kitty backend uses the raw graphics protocol (not `kitten icat`), so —
// unlike the v1 focus view — it does not depend on `kitten` being on PATH.
func chooseGridBackend(termname string) gridBackend {
	if strings.HasPrefix(termname, "xterm-kitty") || strings.HasPrefix(termname, "xterm-ghostty") {
		return backendKitty
	}
	return backendSymbols
}

// tmuxPassthrough wraps an escape sequence so tmux forwards it to the outer
// terminal verbatim: \ePtmux;<seq with every ESC doubled>\e\\
func tmuxPassthrough(seq string) string {
	if os.Getenv("TMUX") == "" {
		return seq
	}
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// transmitVirtual stores a PNG (by file path) as a virtual placement (U=1) under
// a known id, sized to cols x rows cells, emitting NO visible cells. Placeholder
// cells in the View reference it. q=2 suppresses kitty's responses — without it
// the "OK"/error replies land on our stdin and get misparsed as keystrokes.
func transmitVirtual(id int, path string, cols, rows int) string {
	enc := base64.StdEncoding.EncodeToString([]byte(path))
	return tmuxPassthrough(fmt.Sprintf("\x1b_Gi=%d,a=T,U=1,q=2,f=100,c=%d,r=%d,t=f;%s\x1b\\",
		id, cols, rows, enc))
}

// deleteAll removes all stored images + placements (kitty graphics a=d,d=A).
// q=2 suppresses the response (see transmitVirtual).
func deleteAll() string { return tmuxPassthrough("\x1b_Ga=d,d=A,q=2\x1b\\") }

// placeholderRune is kitty's Unicode image placeholder (U+10EEEE).
const placeholderRune = "\U0010EEEE"

// rowColDiacritics are kitty's full rowcolumn-diacritics list (combining class
// 230, 297 entries). The Nth entry encodes row/column index N. Source: kitty
// share/doc/kitty/.../rowcolumn-diacritics.txt. Block dims are clamped to
// len(rowColDiacritics), so this caps the largest image at 297 cells/dimension.
var rowColDiacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F,
	0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357,
	0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369,
	0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F, 0x0483, 0x0484,
	0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1,
	0x05A8, 0x05A9, 0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611,
	0x0612, 0x0613, 0x0614, 0x0615, 0x0616, 0x0617, 0x0657, 0x0658,
	0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6, 0x06D7, 0x06D8,
	0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2,
	0x06E4, 0x06E7, 0x06E8, 0x06EB, 0x06EC, 0x0730, 0x0732, 0x0733,
	0x0735, 0x0736, 0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743,
	0x0745, 0x0747, 0x0749, 0x074A, 0x07EB, 0x07EC, 0x07ED, 0x07EE,
	0x07EF, 0x07F0, 0x07F1, 0x07F3, 0x0816, 0x0817, 0x0818, 0x0819,
	0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082A, 0x082B, 0x082C,
	0x082D, 0x0951, 0x0953, 0x0954, 0x0F82, 0x0F83, 0x0F86, 0x0F87,
	0x135D, 0x135E, 0x135F, 0x17DD, 0x193A, 0x1A17, 0x1A75, 0x1A76,
	0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C, 0x1B6B, 0x1B6D,
	0x1B6E, 0x1B6F, 0x1B70, 0x1B71, 0x1B72, 0x1B73, 0x1CD0, 0x1CD1,
	0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4,
	0x1DC5, 0x1DC6, 0x1DC7, 0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1,
	0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5, 0x1DD6, 0x1DD7, 0x1DD8, 0x1DD9,
	0x1DDA, 0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF, 0x1DE0, 0x1DE1,
	0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5, 0x1DE6, 0x1DFE, 0x20D0, 0x20D1,
	0x20D4, 0x20D5, 0x20D6, 0x20D7, 0x20DB, 0x20DC, 0x20E1, 0x20E7,
	0x20E9, 0x20F0, 0x2CEF, 0x2CF0, 0x2CF1, 0x2DE0, 0x2DE1, 0x2DE2,
	0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6, 0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA,
	0x2DEB, 0x2DEC, 0x2DED, 0x2DEE, 0x2DEF, 0x2DF0, 0x2DF1, 0x2DF2,
	0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8, 0x2DF9, 0x2DFA,
	0x2DFB, 0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D,
	0xA6F0, 0xA6F1, 0xA8E0, 0xA8E1, 0xA8E2, 0xA8E3, 0xA8E4, 0xA8E5,
	0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9, 0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED,
	0xA8EE, 0xA8EF, 0xA8F0, 0xA8F1, 0xAAB0, 0xAAB2, 0xAAB3, 0xAAB7,
	0xAAB8, 0xAABE, 0xAABF, 0xAAC1, 0xFE20, 0xFE21, 0xFE22, 0xFE23,
	0xFE24, 0xFE25, 0xFE26, 0x10A0F, 0x10A38, 0x1D185, 0x1D186, 0x1D187,
	0x1D188, 0x1D189, 0x1D1AA, 0x1D1AB, 0x1D1AC, 0x1D1AD, 0x1D242, 0x1D243,
	0x1D244,
}

// placeholderBlock builds h lines of w placeholder cells referencing image id.
// The image id is encoded as the 24-bit cell foreground color, set once per row.
// w and h are clamped to len(rowColDiacritics) by the caller (geometry).
func placeholderBlock(id, w, h int) string {
	var b strings.Builder
	for r := 0; r < h; r++ {
		fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm", (id>>16)&0xff, (id>>8)&0xff, id&0xff)
		for c := 0; c < w; c++ {
			b.WriteString(placeholderRune)
			b.WriteRune(rowColDiacritics[r])
			b.WriteRune(rowColDiacritics[c])
		}
		b.WriteString("\x1b[39m")
		if r < h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// maxCellDim is the largest thumbnail cell extent placeholderBlock supports.
var maxCellDim = len(rowColDiacritics)

// symbolsArgs builds the chafa arg list for a block-art thumbnail of the given
// cell size. No --clear (that would clear the whole screen per file).
func symbolsArgs(path string, w, h int) []string {
	return []string{"-f", "symbols", "--size", fmt.Sprintf("%dx%d", w, h), path}
}

// symbolsBlock renders a path to chafa symbols text sized to w x h cells, or a
// textual placeholder on failure. chafa wraps output in cursor hide/show
// (\e[?25l … \e[?25h); strip them so they don't leak into the TUI's frame.
func symbolsBlock(path string, w, h int) string {
	out, err := exec.Command("chafa", symbolsArgs(path, w, h)...).Output()
	if err != nil {
		return "[img]"
	}
	s := string(out)
	s = strings.ReplaceAll(s, "\x1b[?25l", "")
	s = strings.ReplaceAll(s, "\x1b[?25h", "")
	return strings.TrimRight(s, "\n")
}
