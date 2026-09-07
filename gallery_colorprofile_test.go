package main

import (
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func TestColorProfileForPinsTrueColorOnlyForKitty(t *testing.T) {
	cases := []struct {
		name    string
		backend gridBackend
		want    colorprofile.Profile
	}{
		{"kitty", backendKitty, colorprofile.TrueColor},
		{"symbols", backendSymbols, colorprofile.Unknown},
		{"raster", backendRaster, colorprofile.Unknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := colorProfileFor(c.backend); got != c.want {
				t.Fatalf("colorProfileFor(%v) = %v, want %v", c.backend, got, c.want)
			}
		})
	}
}

func TestProgramOptionsOnlyPinsForKitty(t *testing.T) {
	if len(programOptions(backendKitty)) != 1 {
		t.Fatalf("programOptions(backendKitty) should pin one option")
	}
	if len(programOptions(backendSymbols)) != 0 {
		t.Fatalf("programOptions(backendSymbols) should not pin a color profile")
	}
	if len(programOptions(backendRaster)) != 0 {
		t.Fatalf("programOptions(backendRaster) should not pin a color profile")
	}
}
