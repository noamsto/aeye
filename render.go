package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/noamsto/mermaid2d2"
)

// runRender renders one diagram from in ("-" reads stdin) to out: an SVG when
// out ends in .svg, else a PNG via resvg. Mermaid is converted to D2 first, so a
// chart type D2 has no analog for (pie, gantt, ...) fails before out is touched,
// letting a caller fall back to another renderer.
func runRender(from, in, out string, stdin io.Reader) error {
	var (
		src []byte
		err error
	)
	inputPath := ""
	if in == "-" {
		src, err = io.ReadAll(stdin)
	} else {
		src, err = os.ReadFile(in)
		inputPath = in
	}
	if err != nil {
		return err
	}

	d2src := string(src)
	if from == "mermaid" {
		if d2src, err = mermaid2d2.MermaidToD2(d2src); err != nil {
			return err
		}
	}
	svg, err := compileD2SVG(d2src, inputPath)
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}

	if strings.EqualFold(filepath.Ext(out), ".svg") {
		return os.WriteFile(out, svg, 0o644)
	}
	cmd := exec.Command(resvgBin(), append(resvgFontArgs(), "-", out)...)
	cmd.Stdin = bytes.NewReader(fixFonts(svg))
	if stderr, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("resvg: %w: %s", err, stderr)
	}
	return nil
}
