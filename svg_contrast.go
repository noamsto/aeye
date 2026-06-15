package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Soft ink endpoints — high contrast without the harshness of pure black/white,
// keeping the sketch register.
const (
	contrastDarkInk  = "#13111C"
	contrastLightInk = "#F5F5F5"
)

var (
	tagRe       = regexp.MustCompile(`<[^>]+>`)
	classAttrRe = regexp.MustCompile(`\bclass="([^"]*)"`)
	fillAttrRe  = regexp.MustCompile(`\bfill="([^"]*)"`)
	styleAttrRe = regexp.MustCompile(`\bstyle="([^"]*)"`)
	styleFillRe = regexp.MustCompile(`fill\s*:\s*[^;]*;?`)
	hexFillRe   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// runSVGContrast rewrites node-label text fills in the SVG at path so each label
// contrasts its node's shape fill, then writes the result back atomically.
func runSVGContrast(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, contrastSVG(data), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// contrastSVG recolors node/container label text against the most recent shape
// fill, leaving every other byte untouched. It relies on d2's emission order:
// within a group the shape (or connection path) precedes its label. A shape with
// a solid fill sets the tracked fill; a connection path clears it, so the edge
// label that follows — which sits on the canvas, not on a node — is left alone.
// The color is set via the inline style attribute, which overrides d2's fill-N
// class rule (a presentation fill attribute would not).
func contrastSVG(src []byte) []byte {
	currentFill := ""
	return tagRe.ReplaceAllFunc(src, func(tag []byte) []byte {
		t := string(tag)
		if strings.HasPrefix(t, "</") || strings.HasPrefix(t, "<!") || strings.HasPrefix(t, "<?") {
			return tag
		}
		class := attrVal(classAttrRe, t)
		switch {
		case strings.Contains(class, "connection"):
			currentFill = ""
			return tag
		case strings.Contains(class, "shape"):
			if f := attrVal(fillAttrRe, t); hexFillRe.MatchString(f) {
				currentFill = f
			}
			return tag
		case tagName(t) == "text":
			if currentFill == "" {
				return tag
			}
			return []byte(setStyleFill(t, contrastInk(currentFill)))
		default:
			return tag
		}
	})
}

// setStyleFill returns tag with its inline style's fill set to ink, replacing any
// existing fill declaration (so a re-run is idempotent) and adding a style
// attribute when none is present.
func setStyleFill(tag, ink string) string {
	decl := "fill:" + ink
	if m := styleAttrRe.FindStringSubmatch(tag); m != nil {
		body := strings.Trim(styleFillRe.ReplaceAllString(m[1], ""), "; ")
		if body != "" {
			body += ";"
		}
		return strings.Replace(tag, m[0], `style="`+body+decl+`"`, 1)
	}
	i := strings.IndexAny(tag, " \t\r\n")
	if i < 0 {
		return tag
	}
	return tag[:i] + ` style="` + decl + `"` + tag[i:]
}

// tagName returns the element name of an SVG tag like "<text …>" or "</g>".
func tagName(tag string) string {
	s := strings.TrimLeft(tag, "</")
	if i := strings.IndexAny(s, " \t\r\n/>"); i >= 0 {
		return s[:i]
	}
	return s
}

func attrVal(re *regexp.Regexp, tag string) string {
	if m := re.FindStringSubmatch(tag); m != nil {
		return m[1]
	}
	return ""
}

// contrastInk picks dark or light ink by the relative luminance of a #rrggbb fill.
func contrastInk(hexFill string) string {
	r, _ := strconv.ParseInt(hexFill[1:3], 16, 0)
	g, _ := strconv.ParseInt(hexFill[3:5], 16, 0)
	b, _ := strconv.ParseInt(hexFill[5:7], 16, 0)
	l := (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 255
	if l > 0.5 {
		return contrastDarkInk
	}
	return contrastLightInk
}
