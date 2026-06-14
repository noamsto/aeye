package main

import (
	"math"
	"regexp"
	"strconv"
)

var (
	pathCmdRe = regexp.MustCompile(`[MmLlHhVvCcSsQqTtAaZz]`)
	pathNumRe = regexp.MustCompile(`-?\d*\.?\d+(?:[eE][-+]?\d+)?`)
)

// operandsPerCmd is how many numbers each command consumes per repetition.
var operandsPerCmd = map[byte]int{
	'M': 2, 'L': 2, 'T': 2, 'H': 1, 'V': 1, 'C': 6, 'S': 4, 'Q': 4, 'A': 7, 'Z': 0,
}

// pathBBox returns the bounding box of an SVG path "d" string in its own
// coordinate space. Handles absolute (upper) and relative (lower) M/L/H/V/C/S/
// Q/T/A/Z. Curve control points are included (a safe over-estimate); arc radii
// are excluded. ok is false when the path yields no coordinates.
func pathBBox(d string) (minX, minY, maxX, maxY float64, ok bool) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	var cx, cy float64 // current point
	add := func(x, y float64) {
		ok = true
		minX, minY = math.Min(minX, x), math.Min(minY, y)
		maxX, maxY = math.Max(maxX, x), math.Max(maxY, y)
	}

	cmds := pathCmdRe.FindAllStringIndex(d, -1)
	for i, ci := range cmds {
		cmd := d[ci[0]]
		end := len(d)
		if i+1 < len(cmds) {
			end = cmds[i+1][0]
		}
		numsStr := pathNumRe.FindAllString(d[ci[1]:end], -1)
		nums := make([]float64, len(numsStr))
		for j, s := range numsStr {
			nums[j], _ = strconv.ParseFloat(s, 64)
		}

		up := cmd &^ 0x20 // uppercase form
		rel := cmd >= 'a'
		n := operandsPerCmd[up]
		if n == 0 {
			continue // Z
		}
		for len(nums) >= n {
			seg := nums[:n]
			nums = nums[n:]
			switch up {
			case 'H':
				x := seg[0]
				if rel {
					x += cx
				}
				cx = x
				add(cx, cy)
			case 'V':
				y := seg[0]
				if rel {
					y += cy
				}
				cy = y
				add(cx, cy)
			case 'A':
				x, y := seg[5], seg[6] // endpoint only
				if rel {
					x, y = x+cx, y+cy
				}
				cx, cy = x, y
				add(cx, cy)
			default: // M,L,T (1 pair), Q,S (2 pairs), C (3 pairs)
				for k := 0; k+1 < len(seg); k += 2 {
					x, y := seg[k], seg[k+1]
					if rel {
						x, y = x+cx, y+cy
					}
					add(x, y)
					cx, cy = x, y
				}
			}
		}
	}
	return
}
