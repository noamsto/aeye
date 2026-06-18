package main

import "strings"

// roleStyle is one role's look in one theme. An empty fill means "leave the
// theme default" — dark diagrams keep dark surfaces and carry the role color on
// the border + title instead, where a light pastel fill would read as a heavy
// block; light diagrams keep the soft pastel fill the role is known by.
type roleStyle struct {
	fill, stroke, font string
}

// palette maps a semantic role to its light and dark treatment. Light gets a soft
// pastel fill with a deeper (Catppuccin Latte) border/ink; dark drops the fill and
// carries a bright (Mocha) accent on the border and label. A diagram tags
// shapes/edges with `class: <role>`; aeye injects the matching definitions for the
// rendered theme, so the diagram source never names a color or a mode.
var palette = map[string]struct{ light, dark roleStyle }{
	"warn":   {light: roleStyle{"#fde8e8", "#d20f39", "#d20f39"}, dark: roleStyle{"", "#f38ba8", "#f38ba8"}},
	"good":   {light: roleStyle{"#e6f4ea", "#40a02b", "#40a02b"}, dark: roleStyle{"", "#a6e3a1", "#a6e3a1"}},
	"accent": {light: roleStyle{"#f3ecfb", "#8839ef", "#8839ef"}, dark: roleStyle{"", "#cba6f7", "#cba6f7"}},
	"info":   {light: roleStyle{"#e8f0fe", "#1e66f5", "#1e66f5"}, dark: roleStyle{"", "#89b4fa", "#89b4fa"}},
}

// roleOrder fixes the emission order so the injected block is deterministic.
var roleOrder = []string{"warn", "good", "accent", "info"}

// paletteMode maps a d2 theme id to its palette variant. d2's dark themes are
// numbered from 200 up; everything below is a light theme.
func paletteMode(themeID int64) string {
	if themeID >= 200 {
		return "dark"
	}
	return "light"
}

// classesBlock renders the d2 `classes:` block for the given mode ("light"/"dark").
// d2 merges this with any classes block already in the source, and a same-named
// class later in the source overrides it — so a diagram can still tweak a role.
func classesBlock(mode string) string {
	var b strings.Builder
	b.WriteString("classes: {\n")
	for _, name := range roleOrder {
		rs := palette[name].dark
		if mode == "light" {
			rs = palette[name].light
		}
		b.WriteString("  " + name + ": { style: {")
		if rs.fill != "" {
			b.WriteString(" fill: \"" + rs.fill + "\";")
		}
		b.WriteString(" stroke: \"" + rs.stroke + "\"; font-color: \"" + rs.font + "\" } }\n")
	}
	b.WriteString("}\n")
	return b.String()
}
