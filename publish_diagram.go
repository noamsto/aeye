package main

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
)

// The manifest lifecycle and render helpers live in shell (the hooks source
// them), so publish-diagram embeds that one implementation and runs it rather
// than forking a Go copy that could drift from the hooks.
//
//go:embed adapters/core/manifest-extract.sh adapters/core/manifest-lifecycle.sh adapters/core/publish-diagram.sh
var publishScripts embed.FS

// runPublishDiagram renders file into pane's carousel via adapters/core's shell
// pipeline and returns the script's exit code (it reports its own errors on
// stderr). The scripts are unpacked next to each other because they source
// their siblings by path.
func runPublishDiagram(file, pane string, open bool) (int, error) {
	dir, err := os.MkdirTemp("", "aeye-publish-")
	if err != nil {
		return 1, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	entries, err := fs.ReadDir(publishScripts, "adapters/core")
	if err != nil {
		return 1, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := publishScripts.ReadFile("adapters/core/" + e.Name())
		if err != nil {
			return 1, err
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o600); err != nil {
			return 1, err
		}
	}

	openArg := ""
	if open {
		openArg = "1"
	}
	cmd := exec.Command("bash", filepath.Join(dir, "publish-diagram.sh"), file, pane, openArg) //nolint:gosec // script is embedded
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if os.Getenv("AEYE_BIN") == "" {
		if self, err := os.Executable(); err == nil {
			cmd.Env = append(cmd.Env, "AEYE_BIN="+self)
		}
	}
	err = cmd.Run()
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}
