// Package adopterfixture holds a committed adopter-style CLI used by tests.
//
// NewApp keeps the canonical machine-output flag named --json so the scorer can
// drive it while still exercising host-owned global bindings. NewMachineJSONApp
// renames that binding to --machine-json and omits the host error policy, which
// isolates the raw-argv machine-error detector seam.
package adopterfixture

import (
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/output"
	"github.com/vincentsch/rungrad/update"
)

const (
	version      = "v0.1.0"
	extNamespace = "acme.example/cli"
)

type productError struct {
	code int
	msg  string
}

func (e productError) Error() string { return e.msg }

// offlineFetcher keeps the update command deterministic and network-free while
// still exercising the standard update.Command wiring.
type offlineFetcher struct{}

func (offlineFetcher) Latest() (update.Release, error) {
	return update.Release{Version: version}, nil
}

// widget is the stable model used by widget list. Other commands intentionally
// use literal positionals instead of resolving against this slice, so scorer
// fixtures do not depend on fixture data.
type widget struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Owner  string `json:"owner"`
	Status string `json:"status"`
}

var widgets = []widget{
	{ID: "w-001", Name: "alpha", Owner: "platform", Status: "active"},
	{ID: "w-002", Name: "demo", Owner: "platform", Status: "planned"},
}

func sortedWidgets() []widget {
	out := append([]widget(nil), widgets...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// widgetListExtensions returns a fresh extension set for command and catalog
// declarations. The docs_path value is illustrative product metadata; tests
// validate its shape, not the existence of that product-owned file.
func widgetListExtensions() manifest.ExtensionSet {
	return manifest.ExtensionSet{
		extNamespace: manifest.ExtensionObject{
			"owner":     "platform",
			"status":    "beta",
			"docs_path": "docs/widget-list.md",
		},
	}
}

// widgetModule is the adopter-style feature module used to prove command
// metadata, manifest extensions, and catalog rows stay in lockstep.
type widgetModule struct{}

func (widgetModule) Groups() []rungrad.Group {
	return []rungrad.Group{{ID: "core", Title: "Core Commands"}}
}

// Commands returns fresh command builders so repeated test apps do not share
// Cobra parents, flags, or runtime state.
func (widgetModule) Commands() []*rungrad.Command {
	widgetCmd := &rungrad.Command{
		Use:      "widget",
		Short:    "Work with widgets",
		Examples: []string{"acmectl widget list", "acmectl widget create demo --dry-run"},
		Related:  []string{"acmectl update"},
		GroupID:  "core",
	}
	widgetCmd.AddCommand(
		&rungrad.Command{
			Use:         "list",
			Short:       "List widgets",
			Examples:    []string{"acmectl widget list", "acmectl widget list --json"},
			Related:     []string{"acmectl widget get", "acmectl widget create"},
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			Extensions:  widgetListExtensions(),
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				data := sortedWidgets()
				return f.WriteResult(data, func(w io.Writer) {
					rows := make([][]string, 0, len(data))
					for _, it := range data {
						rows = append(rows, []string{it.ID, it.Name, it.Status})
					}
					output.RenderTable(w, output.Table{
						Columns: []string{"ID", "Name", "Status"},
						Rows:    rows,
						Empty:   "No widgets.",
					})
				})
			},
		},
		&rungrad.Command{
			Use:         "create <name>",
			Short:       "Create a widget",
			Examples:    []string{"acmectl widget create demo", "acmectl widget create demo --dry-run"},
			Related:     []string{"acmectl widget list"},
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			Mutates:     true,
			Args:        cobra.ExactArgs(1),
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				// Use the literal name. Adding resolution here would couple the
				// scorer's mutate fixture to this package's widget data.
				name := args[0]
				if f.DryRun() {
					return f.WritePreview(output.DryRunPreview{
						Method: "POST",
						Path:   "/widgets",
						Body: []output.Field{
							{Name: "name", Value: name},
							{Name: "token", Value: "fixture-secret", Secret: true},
						},
					})
				}
				summary := output.MutationSummary{Action: "Created", Resource: "widget", Name: name}
				return f.WriteResult(summary, func(w io.Writer) {
					output.RenderMutationMode(w, summary, f.TerminalMode())
				})
			},
		},
		&rungrad.Command{
			Use:         "delete <name>",
			Short:       "Delete a widget",
			Examples:    []string{"acmectl widget delete alpha --dry-run", "acmectl widget delete alpha --confirm"},
			Related:     []string{"acmectl widget list"},
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			Mutates:     true,
			Destructive: true,
			Args:        cobra.ExactArgs(1),
			Configure: func(cmd *cobra.Command) {
				cmd.Flags().Bool("confirm", false, "Confirm the destructive action without a prompt")
			},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				// Use the literal name so "widget delete alpha" always reaches the
				// destructive-confirmation path, even if fixture data changes.
				name := args[0]
				preview := output.DryRunPreview{Method: "DELETE", Path: "/widgets/" + name}
				if f.DryRun() {
					return f.WritePreview(preview)
				}
				confirmed, _ := cmd.Flags().GetBool("confirm")
				if err := f.ConfirmDestructive(rungrad.ConfirmOptions{
					Action:    "delete widget",
					Target:    name,
					Confirmed: confirmed,
				}); err != nil {
					return err
				}
				summary := output.MutationSummary{Action: "Deleted", Resource: "widget", Name: name}
				return f.WriteResult(summary, func(w io.Writer) {
					output.RenderMutationMode(w, summary, f.TerminalMode())
				})
			},
		},
		&rungrad.Command{
			Use:         "get <id>",
			Short:       "Get a widget",
			Examples:    []string{"acmectl widget get alpha"},
			Related:     []string{"acmectl widget list"},
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			Args:        cobra.ExactArgs(1),
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				if args[0] == "locked" {
					return productError{code: rungrad.ExitForbidden, msg: "widget locked"}
				}
				// This command demonstrates the no-resolution rule too: the
				// argument is echoed as the resource identity.
				model := widget{ID: args[0], Name: args[0], Owner: "platform", Status: "active"}
				return f.WriteResult(model, func(w io.Writer) {
					fmt.Fprintf(w, "%s\t%s\n", model.ID, model.Status)
				})
			},
		},
	)
	return []*rungrad.Command{widgetCmd}
}

// Catalog mirrors the visible command metadata instead of deriving it from
// Commands. That keeps ValidateCatalog meaningful: command and catalog edits
// must stay aligned, including the extension metadata on widget list.
func (widgetModule) Catalog() []rungrad.CommandSpec {
	return []rungrad.CommandSpec{
		{
			Path:     "widget",
			Summary:  "Work with widgets",
			GroupID:  "core",
			Examples: []string{"acmectl widget list", "acmectl widget create demo --dry-run"},
			Related:  []string{"acmectl update"},
		},
		{
			Path:        "widget list",
			Summary:     "List widgets",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			Examples:    []string{"acmectl widget list", "acmectl widget list --json"},
			Related:     []string{"acmectl widget get", "acmectl widget create"},
			Extensions:  widgetListExtensions(),
		},
		{
			Path:        "widget create",
			Summary:     "Create a widget",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			Examples:    []string{"acmectl widget create demo", "acmectl widget create demo --dry-run"},
			Related:     []string{"acmectl widget list"},
			Mutates:     true,
		},
		{
			Path:        "widget delete",
			Summary:     "Delete a widget",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			Examples:    []string{"acmectl widget delete alpha --dry-run", "acmectl widget delete alpha --confirm"},
			Related:     []string{"acmectl widget list"},
			Mutates:     true,
			Destructive: true,
		},
		{
			Path:        "widget get",
			Summary:     "Get a widget",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			Examples:    []string{"acmectl widget get alpha"},
			Related:     []string{"acmectl widget list"},
		},
	}
}

// updateSpecModule supplies the catalog row for the update command, which is
// constructed by the update package and added directly to the app.
type updateSpecModule struct{}

func (updateSpecModule) Groups() []rungrad.Group      { return nil }
func (updateSpecModule) Commands() []*rungrad.Command { return nil }

func (updateSpecModule) Catalog() []rungrad.CommandSpec {
	return []rungrad.CommandSpec{{
		Path:        "update",
		Summary:     "Check for and install the latest version",
		OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
		Examples:    []string{"acmectl update --check", "acmectl update --check --json", "acmectl update"},
		Mutates:     true,
	}}
}

// newApp parameterizes only the canonical JSON flag name and error policy. The
// main fixture keeps --json for scorer compatibility; the seam fixture renames
// it to --machine-json so early machine-error detection can be tested directly.
func newApp(jsonFlagName string, withPolicy bool) *rungrad.App {
	cfg := rungrad.AppConfig{
		Name:    "acmectl",
		Short:   "Acme product CLI on rungrad",
		Long:    "acmectl is a small adopter fixture that exercises host-owned rungrad extension points together.",
		Version: version,
		EnvVar:  "ACMECTL_TOKEN",
		Surface: rungrad.SurfaceConfig{
			GlobalFlags: rungrad.GlobalFlagSurface{
				Mode: rungrad.SurfaceHostOwned,
				// These are exactly the rungrad globals active for a compact app.
				// Adding resolution or advanced output would require additional
				// host bindings by design.
				Bindings: rungrad.GlobalFlagBindings{
					JSON:     rungrad.GlobalFlagBinding{Name: jsonFlagName},
					DryRun:   rungrad.GlobalFlagBinding{Name: "dry-run"},
					NoPrompt: rungrad.GlobalFlagBinding{Name: "no-prompt"},
					Quiet:    rungrad.GlobalFlagBinding{Name: "quiet"},
					Config:   rungrad.GlobalFlagBinding{Name: "config"},
				},
			},
		},
	}
	if withPolicy {
		cfg.ErrorPolicy = &rungrad.ErrorPolicy{
			Classify: func(ctx rungrad.ErrorContext) int {
				// Only product errors override classification; everything else
				// keeps rungrad's default exit-code contract for scorer probes.
				var pe productError
				if errors.As(ctx.Err, &pe) {
					return pe.code
				}
				return ctx.DefaultExitCode
			},
			Render: func(ctx rungrad.ErrorContext) error {
				if ctx.MachineOutput {
					// Keep the product-shaped fields while retaining error and
					// exit_code so structured-stderr scorer probes can still read it.
					body, err := output.StableJSON(map[string]any{
						"error":     ctx.Err.Error(),
						"exit_code": ctx.ExitCode,
						"message":   ctx.Err.Error(),
						"status":    ctx.ExitCode,
					})
					if err != nil {
						return err
					}
					_, err = ctx.Stderr.Write(ctx.RedactJSON(body))
					return err
				}
				_, err := fmt.Fprintf(ctx.Stderr, "acmectl: %s\n", ctx.RedactString(ctx.Err.Error()))
				return err
			},
		}
	}
	app := rungrad.New(cfg)
	app.AddModule(widgetModule{}, updateSpecModule{})
	app.AddCommand(update.Command(update.CommandConfig{
		CurrentVersion: version,
		ToolName:       "acmectl",
		Fetcher:        offlineFetcher{},
	}))
	return app
}

// NewApp is the combined extension-point fixture: host-owned globals keep the
// JSON binding named --json, a host ErrorPolicy owns rendering and exit, and a
// namespaced manifest extension is mirrored on the catalog.
func NewApp() *rungrad.App { return newApp("json", true) }

// NewMachineJSONApp binds canonical JSON to --machine-json with no ErrorPolicy,
// isolating the p12-002/p12-003 raw-argument machine-output detector seam.
func NewMachineJSONApp() *rungrad.App { return newApp("machine-json", false) }
