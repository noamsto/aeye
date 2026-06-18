package main

import (
	"github.com/alecthomas/kong"
)

// version is stamped at build time via -ldflags "-X main.version=...".
// Plain `go build` / devShell runs leave it "dev".
var version = "dev"

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
		kong.Vars{"version": version},
		kong.UsageOnError(),
	)
	switch ctx.Command() {
	case "render-diagram <in> <out>":
		ctx.FatalIfErrorf(runRenderDiagram(cli.RenderDiagram.In, cli.RenderDiagram.Out))
	default: // "open" or "open <key>"
		ctx.FatalIfErrorf(runGallery(cli.Open.Key))
	}
}
