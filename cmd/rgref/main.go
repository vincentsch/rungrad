// Command rgref is the rungrad reference CLI. It is a small but complete tool
// built on the framework that exercises every property of the spec, so it serves
// as a worked example and as the target the conformance scorer checks against the
// spec the framework ships.
package main

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/config"
	"github.com/vincentsch/rungrad/output"
	"github.com/vincentsch/rungrad/resolve"
	"github.com/vincentsch/rungrad/update"
)

const version = "v0.2.1"

// item is a fixture resource. Two items share the name "dup" so name resolution
// has something ambiguous to resolve. Size and Label are deliberately visible in
// machine output but not in the human/plain list view; they prove transforms run
// over the full stable model rather than over the human projection.
type item struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Label string `json:"label"`
}

var items = []item{
	{ID: "1", Name: "alpha", Size: 9007199254740993, Label: "A&B <demo> café"},
	{ID: "2", Name: "dup", Size: 2, Label: "two"},
	{ID: "3", Name: "dup", Size: 3, Label: "three"},
}

func sortedItems() []item {
	out := append([]item(nil), items...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Pointer helpers keep explicit zero/false metadata values from being omitted.
func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }
func boolPtr(v bool) *bool    { return &v }

func itemLookup(name string) ([]resolve.Match, error) {
	var matches []resolve.Match
	for _, it := range items {
		if it.Name == name {
			matches = append(matches, resolve.Match{ID: it.ID, Name: it.Name})
		}
	}
	return matches, nil
}

// fixedFetcher reports a single release without any network access, so the
// reference tool's `update --check` is deterministic and offline.
type fixedFetcher struct{ version string }

func (f fixedFetcher) Latest() (update.Release, error) {
	return update.Release{Version: f.version}, nil
}

// itemsModule registers the item command family, declares the "core" help group,
// and supplies the matching catalog rows. Like the framework module fixtures,
// each method returns fresh values on every call.
type itemsModule struct{}

func (itemsModule) Groups() []rungrad.Group {
	return []rungrad.Group{{ID: "core", Title: "Core Commands"}}
}

// Commands builds fresh command values because AddModule takes ownership of the
// Cobra commands it registers. Reusing command pointers across app instances
// would leak flags, parents, and runtime state between tests.
func (itemsModule) Commands() []*rungrad.Command {
	itemCmd := &rungrad.Command{
		Use:      "item",
		Short:    "Work with items",
		Examples: []string{"rgref item list", "rgref item get alpha", "rgref item create gamma --dry-run"},
		Related:  []string{"rgref whoami", "rgref update"},
		GroupID:  "core",
	}
	itemCmd.AddCommand(
		&rungrad.Command{
			Use:   "list",
			Short: "List items",
			Examples: []string{
				"rgref item list",
				"rgref item list --json",
				"rgref item list --plain",
				"rgref item list --jq '.[].name'",
				`rgref item list --template '{{range .}}{{.id}} {{.name}}{{"\n"}}{{end}}'`,
			},
			Related: []string{"rgref item get", "rgref item create"},
			OutputModes: []string{
				rungrad.OutputModeHuman,
				rungrad.OutputModeJSON,
				rungrad.OutputModePlain,
				rungrad.OutputModeJQ,
				rungrad.OutputModeTemplate,
			},
			SupportsMeta: true,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				data := sortedItems()
				// Read the resolved endpoint through the generic API; never duplicate
				// flag/env/config precedence. Resolution is populated for every command.
				api, _ := f.Service("api")
				// The metadata fixture is fixed on purpose: tests can assert the
				// envelope exactly while ordinary output stays deterministic.
				meta := output.Meta{
					RequestID: "req_rgref_items_0001",
					Pagination: &output.Pagination{
						Page:       intPtr(1),
						PerPage:    intPtr(3),
						TotalPages: intPtr(1),
						TotalItems: intPtr(3),
						HasMore:    boolPtr(false),
					},
					RateLimit: &output.RateLimit{
						Limit:     int64Ptr(1000),
						Remaining: int64Ptr(997),
						Reset:     int64Ptr(1893456000),
					},
					Retry:       &output.Retry{Attempts: 1},
					Idempotency: &output.Idempotency{Key: "rgref-item-list-fixture", Replayed: boolPtr(false)},
					Extra:       map[string]any{"service_url": api.Value},
				}
				return f.WriteOutput(rungrad.Output{
					Model: data,
					Meta:  meta,
					Human: func(w io.Writer) {
						rows := make([][]string, 0, len(data))
						for _, it := range data {
							rows = append(rows, []string{it.ID, it.Name})
						}
						output.RenderTable(w, output.Table{Columns: []string{"ID", "Name"}, Rows: rows, Empty: "No items."})
					},
					Plain: func(w io.Writer) {
						// Plain output is intentionally headerless and tab-separated so
						// callers can pipe it without stripping table formatting.
						for _, it := range data {
							fmt.Fprintf(w, "%s\t%s\n", it.ID, it.Name)
						}
					},
				})
			},
		},
		&rungrad.Command{
			Use:   "get <name>",
			Short: "Resolve an item by name or id",
			Examples: []string{
				"rgref item get alpha",
				"rgref item get 1",
				"rgref item get alpha --jq .id",
				"rgref item get alpha --template '{{.id}}'",
			},
			Related: []string{"rgref item list"},
			OutputModes: []string{
				rungrad.OutputModeHuman,
				rungrad.OutputModeJSON,
				rungrad.OutputModeJQ,
				rungrad.OutputModeTemplate,
			},
			Args: cobra.ExactArgs(1),
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				// These reserved names are conformance fixtures. Handle them before
				// name resolution so they exercise 2/4/6 instead of becoming not-found.
				switch args[0] {
				case "broken":
					return rungrad.NewError(rungrad.ExitAPI, "api error fixture")
				case "forbidden":
					return rungrad.NewError(rungrad.ExitForbidden, "forbidden fixture")
				case "throttled":
					return rungrad.NewError(rungrad.ExitRateLimited, "rate limited fixture")
				}
				id, err := f.Resolve(args[0], itemLookup, resolve.Options{
					ResourceType: "item",
					AllowPrompt:  true,
					IsID:         resolve.IsNumericID,
				})
				if err != nil {
					return err
				}
				return f.WriteResult(map[string]string{"id": id}, func(w io.Writer) {
					fmt.Fprintln(w, id)
				})
			},
		},
		&rungrad.Command{
			Use:         "create <name>",
			Short:       "Create an item",
			Examples:    []string{"rgref item create gamma", "rgref item create gamma --dry-run", "rgref item create gamma --quiet"},
			Related:     []string{"rgref item list"},
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			Mutates:     true,
			Args:        cobra.ExactArgs(1),
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				summary := output.MutationSummary{Action: "Created", Resource: "item", Name: args[0]}
				if f.DryRun() {
					return f.WritePreview(output.DryRunPreview{
						Method: "POST", Path: "/items",
						Body: []output.Field{
							{Name: "name", Value: args[0]},
							{Name: "token", Value: "reference-secret", Secret: true},
						},
					})
				}
				// This hint is non-essential and sits after the dry-run return, so
				// previews stay focused on the operation that would happen.
				f.Infof("hint: pass --quiet to hide informational messages")
				return f.WriteResult(summary, func(w io.Writer) {
					output.RenderMutationMode(w, summary, f.TerminalMode())
				})
			},
		},
		&rungrad.Command{
			Use:         "delete <name>",
			Short:       "Delete an item",
			Examples:    []string{"rgref item delete alpha --dry-run", "rgref item delete alpha --confirm"},
			Related:     []string{"rgref item list"},
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			Mutates:     true,
			Destructive: true,
			Args:        cobra.ExactArgs(1),
			Configure: func(cmd *cobra.Command) {
				cmd.Flags().Bool("confirm", false, "Confirm the destructive action without a prompt")
			},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				id, err := f.Resolve(args[0], itemLookup, resolve.Options{
					ResourceType: "item",
					AllowPrompt:  true,
					IsID:         resolve.IsNumericID,
				})
				if err != nil {
					return err
				}
				preview := output.DryRunPreview{Method: "DELETE", Path: "/items/" + id}
				if f.DryRun() {
					return f.WritePreview(preview)
				}
				confirmed, _ := cmd.Flags().GetBool("confirm")
				if err := f.ConfirmDestructive(rungrad.ConfirmOptions{
					Action:    "delete item",
					Target:    id,
					Confirmed: confirmed,
				}); err != nil {
					return err
				}
				// rgref is a fixture: do not mutate the package-level items slice, so
				// other in-process tests and repeated scorer runs stay deterministic.
				summary := output.MutationSummary{Action: "Deleted", Resource: "item", ID: id}
				return f.WriteResult(summary, func(w io.Writer) {
					output.RenderMutationMode(w, summary, f.TerminalMode())
				})
			},
		},
	)
	return []*rungrad.Command{itemCmd}
}

// Catalog deliberately restates the user-visible command metadata instead of
// deriving it from Commands. ValidateCatalog compares these rows with the built
// tree, so a future command edit has to update both sides or fail fast.
func (itemsModule) Catalog() []rungrad.CommandSpec {
	return []rungrad.CommandSpec{
		{
			Path:     "item",
			Summary:  "Work with items",
			GroupID:  "core",
			Examples: []string{"rgref item list", "rgref item get alpha", "rgref item create gamma --dry-run"},
			Related:  []string{"rgref whoami", "rgref update"},
		},
		{
			Path:    "item list",
			Summary: "List items",
			OutputModes: []string{
				rungrad.OutputModeHuman,
				rungrad.OutputModeJSON,
				rungrad.OutputModePlain,
				rungrad.OutputModeJQ,
				rungrad.OutputModeTemplate,
			},
			Examples: []string{
				"rgref item list",
				"rgref item list --json",
				"rgref item list --plain",
				"rgref item list --jq '.[].name'",
				`rgref item list --template '{{range .}}{{.id}} {{.name}}{{"\n"}}{{end}}'`,
			},
			Related:      []string{"rgref item get", "rgref item create"},
			SupportsMeta: true,
		},
		{
			Path:    "item get",
			Summary: "Resolve an item by name or id",
			OutputModes: []string{
				rungrad.OutputModeHuman,
				rungrad.OutputModeJSON,
				rungrad.OutputModeJQ,
				rungrad.OutputModeTemplate,
			},
			Examples: []string{
				"rgref item get alpha",
				"rgref item get 1",
				"rgref item get alpha --jq .id",
				"rgref item get alpha --template '{{.id}}'",
			},
			Related: []string{"rgref item list"},
		},
		{
			Path:        "item create",
			Summary:     "Create an item",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			Examples:    []string{"rgref item create gamma", "rgref item create gamma --dry-run", "rgref item create gamma --quiet"},
			Related:     []string{"rgref item list"},
			Mutates:     true,
		},
		{
			Path:        "item delete",
			Summary:     "Delete an item",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			Examples:    []string{"rgref item delete alpha --dry-run", "rgref item delete alpha --confirm"},
			Related:     []string{"rgref item list"},
			// Destructive commands are mutating in the built tree; keeping both fields
			// explicit makes the catalog row read like the command definition.
			Mutates:     true,
			Destructive: true,
		},
	}
}

// identityModule registers whoami with no help group, demonstrating a groupless
// ordinary module.
type identityModule struct{}

func (identityModule) Groups() []rungrad.Group { return nil }

// Commands returns whoami without a group, so Cobra keeps it in the default
// "Additional Commands" help section.
func (identityModule) Commands() []*rungrad.Command {
	return []*rungrad.Command{{
		Use:          "whoami",
		Short:        "Show the authenticated identity",
		Examples:     []string{"rgref whoami"},
		Related:      []string{"rgref item list"},
		OutputModes:  []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
		RequiresAuth: true,
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			masked := config.Mask(f.Token)
			return f.WriteResult(map[string]string{"token": masked}, func(w io.Writer) {
				fmt.Fprintf(w, "authenticated (%s)\n", masked)
			})
		},
	}}
}

// Catalog mirrors whoami's authentication requirement. The empty GroupID is
// intentional: this command is not part of the "core" help group.
func (identityModule) Catalog() []rungrad.CommandSpec {
	return []rungrad.CommandSpec{{
		Path:         "whoami",
		Summary:      "Show the authenticated identity",
		OutputModes:  []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
		Examples:     []string{"rgref whoami"},
		Related:      []string{"rgref item list"},
		RequiresAuth: true,
	}}
}

// updateSpecModule is a spec-only module: it registers no groups or commands and
// only supplies the catalog row for the framework-built update command. The
// update command is added directly with app.AddCommand because the update package
// owns its construction, and ValidateCatalog validates the whole visible surface,
// so a directly-added command needs a matching row from some module.
type updateSpecModule struct{}

func (updateSpecModule) Groups() []rungrad.Group      { return nil }
func (updateSpecModule) Commands() []*rungrad.Command { return nil }

// Catalog mirrors the update command built below with ToolName "rgref". The
// row has no Related commands because update.Command does not declare any.
func (updateSpecModule) Catalog() []rungrad.CommandSpec {
	return []rungrad.CommandSpec{{
		Path:        "update",
		Summary:     "Check for and install the latest version",
		OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
		Examples:    []string{"rgref update --check", "rgref update --check --json", "rgref update"},
		Mutates:     true,
	}}
}

func newApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:           "rgref",
		Short:          "rungrad reference CLI",
		Long:           "rgref is the worked example tool for the rungrad framework.",
		Version:        version,
		EnvVar:         "RGREF_TOKEN",
		AdvancedOutput: true,
		Resolution: &rungrad.ResolutionConfig{
			Profile:  true,
			AuthFile: true,
			Services: []rungrad.Service{{
				Name:      "api",
				Flag:      "api-url",
				EnvVar:    "RGREF_API_URL",
				ConfigKey: "api_url",
				Default:   "https://api.rgref.invalid",
				Usage:     "Base URL for the reference API service",
			}},
		},
	})
	app.Root().Example = "rgref item list\nrgref item list --json\nrgref item create gamma --dry-run"
	app.Root().Long += "\n\nRelated commands:\n  rgref item list\n  rgref update"

	// rgref is assembled from compiled feature modules. itemsModule contributes
	// the core group and the item family; identityModule contributes whoami; the
	// spec-only updateSpecModule supplies the catalog row for the framework-built
	// update command, which the update package constructs.
	app.AddModule(itemsModule{}, identityModule{}, updateSpecModule{})
	app.AddCommand(update.Command(update.CommandConfig{
		CurrentVersion: version,
		Fetcher:        fixedFetcher{version: version},
		ToolName:       "rgref",
	}))
	return app
}

func main() {
	os.Exit(newApp().Run(os.Args[1:], os.Stdout, os.Stderr))
}
