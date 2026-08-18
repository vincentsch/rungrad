package update

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
)

// CommandConfig configures the update command for a tool.
type CommandConfig struct {
	// CurrentVersion is the running build's version.
	CurrentVersion string
	// Fetcher resolves the latest release. For offline or air-gapped tools, inject
	// a fetcher that returns a static release.
	Fetcher Fetcher
	// Apply installs the given release, replacing the executable. When nil, the
	// command reports that automatic install is unavailable instead of failing.
	Apply func(latest Release) error
	// ToolName is the program name used in the standard update examples
	// (e.g. "<tool> update --check"). When empty it falls back to "mytool" for
	// generic documentation and tests.
	ToolName string
}

// Command builds the standard `update` command. With --check it evaluates and
// reports without changing anything, which is the path agents and CI should use.
// Without --check it installs the latest release when an Apply function is set.
func Command(cc CommandConfig) *rungrad.Command {
	tool := cc.ToolName
	if tool == "" {
		tool = "mytool"
	}
	return &rungrad.Command{
		Use:         "update",
		Short:       "Check for and install the latest version",
		Examples:    []string{tool + " update --check", tool + " update --check --json", tool + " update"},
		OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
		Mutates:     true,
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().Bool("check", false, "Check for an update without installing it")
		},
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			check, _ := cmd.Flags().GetBool("check")
			if cc.Fetcher == nil {
				return rungrad.NewError(rungrad.ExitAPI, "no release fetcher configured")
			}
			latest, err := cc.Fetcher.Latest()
			if err != nil {
				return rungrad.NewError(rungrad.ExitAPI, "fetch latest release: "+err.Error())
			}
			result := Evaluate(cc.CurrentVersion, latest)

			if check || f.DryRun() || !result.Available {
				return f.WriteResult(result, func(w io.Writer) {
					fmt.Fprintf(w, "current %s, latest %s: %s\n", result.Current, result.Latest, result.Status)
				})
			}

			if cc.Apply == nil {
				return rungrad.NewError(rungrad.ExitAPI, "automatic install is unavailable; download the latest release manually")
			}
			if err := cc.Apply(latest); err != nil {
				return rungrad.NewError(rungrad.ExitAPI, "install update: "+err.Error())
			}
			installed := result
			installed.Current = result.Latest
			installed.Available = false
			installed.Status = StatusUpToDate
			return f.WriteResult(installed, func(w io.Writer) {
				fmt.Fprintf(w, "updated to %s\n", installed.Latest)
			})
		},
	}
}
