package testutil

import (
	"strings"

	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/internal/cmdtree"
)

// CaptureHelp returns the --help output for the command identified by path
// (path segments after the program name; empty path means the root command).
// Help is captured through the in-process buffer, so it is deterministic and
// free of terminal width wrapping.
func CaptureHelp(app *rungrad.App, path ...string) string {
	args := append(append([]string{}, path...), "--help")
	return Run(app, args...).Stdout
}

// CaptureAllHelp returns CaptureHelp for every visible command in app, keyed by
// the space-joined command path ("" for the root). It walks the same visible
// command set docs generation and the manifest use, so its keys line up with
// generated doc pages and manifest command paths.
func CaptureAllHelp(app *rungrad.App) map[string]string {
	help := map[string]string{}
	for _, cmd := range cmdtree.VisibleCommands(app.Root()) {
		segs := strings.Fields(cmd.CommandPath())[1:]
		help[strings.Join(segs, " ")] = CaptureHelp(app, segs...)
	}
	return help
}
