package main

import (
	_ "embed"
	"encoding/json"

	"github.com/alecthomas/kong"
)

// releaseManifest is release-please's source of truth for the version, embedded
// so every build path reports the real release — no hardcoded constant to drift.
//
//go:embed .release-please-manifest.json
var releaseManifest []byte

// buildSuffix is appended to the version via -ldflags (the git shortrev in nix
// builds) to pin which commit a binary was built from. Empty for plain go build.
var buildSuffix string

// version is the released version from the manifest, plus the build suffix.
func version() string {
	var m map[string]string
	if json.Unmarshal(releaseManifest, &m) != nil || m["."] == "" {
		return "dev"
	}
	v := m["."]
	if buildSuffix != "" {
		v += "-" + buildSuffix
	}
	return v
}

var cli struct {
	Version kong.VersionFlag `short:"v" help:"Print the build version."`

	Open struct {
		Key string `arg:"" optional:"" help:"Manifest key: a tmux pane id (%N) or a Claude Code session id."`
	} `cmd:"" default:"withargs" help:"Open the image carousel (default)."`

	RenderDiagram struct {
		In  string `arg:"" help:"Input .d2 file."`
		Out string `arg:"" help:"Output .png file (a sibling .svg is written too)."`
	} `cmd:"" name:"render-diagram" help:"Compile a .d2 to PNG: d2 -> fix-fonts -> contrast -> resvg."`
}

func main() {
	ctx := kong.Parse(&cli,
		kong.Name("aeye"),
		kong.Description("Image carousel for coding agents, plus the diagram render pipeline."),
		kong.Vars{"version": version()},
		kong.UsageOnError(),
	)
	switch ctx.Command() {
	case "render-diagram <in> <out>":
		ctx.FatalIfErrorf(runRenderDiagram(cli.RenderDiagram.In, cli.RenderDiagram.Out))
	default: // "open" or "open <key>"
		ctx.FatalIfErrorf(runGallery(cli.Open.Key))
	}
}
