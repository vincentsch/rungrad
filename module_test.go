package rungrad_test

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/docsgen"
	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/output"
	"github.com/vincentsch/rungrad/testutil"
)

// storeModule is a realistic feature module fixture: it contributes a help
// group, a command family, subcommands, and matching catalog rows.
type storeModule struct{}

func (storeModule) Groups() []rungrad.Group {
	return []rungrad.Group{{ID: "store", Title: "Store:"}}
}

func (storeModule) Commands() []*rungrad.Command {
	item := &rungrad.Command{
		Use:     "item",
		Short:   "Work with items",
		GroupID: "store",
	}
	item.AddCommand(
		&rungrad.Command{
			Use:         "list",
			Short:       "List items",
			Examples:    []string{"rgmod item list", "rgmod item list --json"},
			Related:     []string{"rgmod item create"},
			OutputModes: []string{"table", "json"},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				rows := []map[string]string{{"id": "item-1", "name": "alpha"}}
				return f.WriteResult(rows, func(w io.Writer) {
					output.RenderTable(w, output.Table{
						Columns: []string{"ID", "Name"},
						Rows:    [][]string{{"item-1", "alpha"}},
					})
				})
			},
		},
		&rungrad.Command{
			Use:         "create <name>",
			Short:       "Create an item",
			Mutates:     true,
			Args:        cobra.ExactArgs(1),
			OutputModes: []string{"table", "json"},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return f.WriteResult(output.MutationSummary{Action: "Created", Resource: "item", Name: args[0]}, nil)
			},
		},
	)
	return []*rungrad.Command{item}
}

func (storeModule) Catalog() []rungrad.CommandSpec {
	return []rungrad.CommandSpec{
		{Path: "item", Summary: "Work with items", GroupID: "store"},
		{
			Path:        "item create",
			Summary:     "Create an item",
			OutputModes: []string{"table", "json"},
			Mutates:     true,
		},
		{
			Path:        "item list",
			Summary:     "List items",
			OutputModes: []string{"table", "json"},
			Examples:    []string{"rgmod item list", "rgmod item list --json"},
			Related:     []string{"rgmod item create"},
		},
	}
}

// identityModule covers a second root help group plus auth and metadata
// annotations, so the fixture proves modules compose across command families.
type identityModule struct{}

func (identityModule) Groups() []rungrad.Group {
	return []rungrad.Group{{ID: "identity", Title: "Identity:"}}
}

func (identityModule) Commands() []*rungrad.Command {
	return []*rungrad.Command{{
		Use:          "whoami",
		Short:        "Show the current identity",
		GroupID:      "identity",
		RequiresAuth: true,
		SupportsMeta: true,
		OutputModes:  []string{"table", "json"},
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			return f.WriteResultWithMeta(map[string]string{"token": f.Token}, output.Meta{}, nil)
		},
	}}
}

func (identityModule) Catalog() []rungrad.CommandSpec {
	return []rungrad.CommandSpec{{
		Path:         "whoami",
		Summary:      "Show the current identity",
		GroupID:      "identity",
		RequiresAuth: true,
		SupportsMeta: true,
		OutputModes:  []string{"table", "json"},
	}}
}

// stubModule keeps drift tests focused on one defect at a time without hiding
// behavior behind another test-only builder.
type stubModule struct {
	groups   []rungrad.Group
	commands []*rungrad.Command
	specs    []rungrad.CommandSpec
}

func (m stubModule) Groups() []rungrad.Group        { return m.groups }
func (m stubModule) Commands() []*rungrad.Command   { return m.commands }
func (m stubModule) Catalog() []rungrad.CommandSpec { return m.specs }

// moduleApp is the happy-path multi-module app used for manifest, docs,
// catalog, and determinism assertions.
func moduleApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:           "rgmod",
		Short:          "module test CLI",
		Version:        "0.0.0-test",
		EnvVar:         "RGMOD_TOKEN",
		AdvancedOutput: true,
	})
	app.AddModule(storeModule{}, identityModule{})
	return app
}

func TestFeatureModulesRegisterGroupsCommandsAndRun(t *testing.T) {
	app := moduleApp()
	help := testutil.Run(app, "--help")
	if help.Exit != rungrad.ExitSuccess {
		t.Fatalf("help exit %d stderr=%q", help.Exit, help.Stderr)
	}
	for _, want := range []string{"Store:", "Identity:", "item", "whoami"} {
		if !strings.Contains(help.Stdout, want) {
			t.Fatalf("help missing %q:\n%s", want, help.Stdout)
		}
	}

	res := testutil.Run(app, "item", "list", "--json")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("item list exit %d stderr=%q", res.Exit, res.Stderr)
	}
	var rows []map[string]string
	if err := res.JSON(&rows); err != nil {
		t.Fatalf("item list JSON: %v\n%s", err, res.Stdout)
	}
	if len(rows) != 1 || rows[0]["name"] != "alpha" {
		t.Fatalf("item list rows = %+v", rows)
	}
}

func TestFeatureModulesManifestProjection(t *testing.T) {
	app := moduleApp()
	m, res := readManifest(t, app)
	if err := manifest.Validate(&m); err != nil {
		t.Fatalf("Validate(manifest) = %v\n%s", err, res.Stdout)
	}

	item := findManifestCommand(&m, "item")
	if item == nil || item.Short != "Work with items" {
		t.Fatalf("item manifest = %+v", item)
	}
	create := findManifestCommand(&m, "item", "create")
	if create == nil || !create.Mutates || !create.SupportsDryRun || create.Destructive {
		t.Fatalf("item create manifest = %+v", create)
	}
	if !reflect.DeepEqual(create.OutputModes, []string{"table", "json"}) {
		t.Fatalf("item create output modes = %v", create.OutputModes)
	}
	list := findManifestCommand(&m, "item", "list")
	if list == nil {
		t.Fatal("manifest missing item list")
	}
	if !reflect.DeepEqual(list.OutputModes, []string{"table", "json"}) {
		t.Fatalf("item list output modes = %v", list.OutputModes)
	}
	if !reflect.DeepEqual(list.Examples, []string{"rgmod item list", "rgmod item list --json"}) {
		t.Fatalf("item list examples = %v", list.Examples)
	}
	if !reflect.DeepEqual(list.Related, []string{"rgmod item create"}) {
		t.Fatalf("item list related = %v", list.Related)
	}
	whoami := findManifestCommand(&m, "whoami")
	if whoami == nil || !whoami.RequiresAuth || !whoami.SupportsMeta {
		t.Fatalf("whoami manifest = %+v", whoami)
	}
	if !reflect.DeepEqual(whoami.OutputModes, []string{"table", "json"}) {
		t.Fatalf("whoami output modes = %v", whoami.OutputModes)
	}
}

func TestFeatureModulesDocsGeneration(t *testing.T) {
	docs := docsgen.Generate(moduleApp())
	for _, path := range []string{"rgmod_item.md", "rgmod_item_create.md", "rgmod_item_list.md", "rgmod_whoami.md"} {
		if _, ok := docs[path]; !ok {
			t.Fatalf("docs missing %s; got %v", path, moduleDocKeys(docs))
		}
	}
	index := docs["index.md"]
	for _, want := range []string{"rgmod item", "rgmod item list", "rgmod whoami"} {
		if !strings.Contains(index, want) {
			t.Fatalf("docs index missing %q:\n%s", want, index)
		}
	}
	if page := docs["rgmod_item_list.md"]; !strings.Contains(page, "## Examples") || !strings.Contains(page, "## Related commands") {
		t.Fatalf("item list docs missing catalog-facing sections:\n%s", page)
	}
}

func TestFeatureModuleCatalogPassesAndIsCopied(t *testing.T) {
	app := moduleApp()
	if err := app.ValidateCatalog(); err != nil {
		t.Fatalf("ValidateCatalog() = %v", err)
	}

	got := app.Catalog()
	wantPaths := []string{"item", "item create", "item list", "whoami"}
	if paths := specPathList(got); !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("Catalog paths = %v, want %v", paths, wantPaths)
	}
	got[0].Summary = "mutated by test"
	got[2].OutputModes[0] = "mutated"
	second := app.Catalog()
	if second[0].Summary == "mutated by test" || second[2].OutputModes[0] == "mutated" {
		t.Fatalf("Catalog returned mutable app state: %+v", second)
	}
}

func TestFeatureModuleCatalogDriftFailures(t *testing.T) {
	tests := []struct {
		name string
		mods []rungrad.FeatureModule
		want string
	}{
		{
			name: "visible command without spec",
			mods: []rungrad.FeatureModule{stubModule{
				commands: []*rungrad.Command{{Use: "probe", Short: "Probe command"}},
			}},
			want: `visible command "probe" has no catalog entry`,
		},
		{
			name: "spec without visible command",
			mods: []rungrad.FeatureModule{stubModule{
				specs: []rungrad.CommandSpec{{Path: "ghost", Summary: "Ghost command"}},
			}},
			want: `catalog entry "ghost" does not resolve to a visible command`,
		},
		{
			name: "summary mismatch",
			mods: []rungrad.FeatureModule{catalogProbeModule(func(spec *rungrad.CommandSpec) {
				spec.Summary = "Different summary"
			})},
			want: `summary "Different summary" does not match built command short "Probe command"`,
		},
		{
			name: "group mismatch",
			mods: []rungrad.FeatureModule{catalogProbeModule(func(spec *rungrad.CommandSpec) {
				spec.GroupID = "other"
			})},
			want: `group_id="other" does not match built command "core"`,
		},
		{
			name: "output modes mismatch",
			mods: []rungrad.FeatureModule{catalogProbeModule(func(spec *rungrad.CommandSpec) {
				spec.OutputModes = []string{"json"}
			})},
			want: `output modes [json] do not match built command [table json]`,
		},
		{
			name: "examples mismatch",
			mods: []rungrad.FeatureModule{catalogProbeModule(func(spec *rungrad.CommandSpec) {
				spec.Examples = []string{"rgmod probe --json"}
			})},
			want: `examples [rgmod probe --json] do not match built command [rgmod probe]`,
		},
		{
			name: "related mismatch",
			mods: []rungrad.FeatureModule{catalogProbeModule(func(spec *rungrad.CommandSpec) {
				spec.Related = []string{"rgmod another"}
			})},
			want: `related [rgmod another] do not match built command [rgmod other]`,
		},
		{
			name: "requires auth mismatch",
			mods: []rungrad.FeatureModule{catalogProbeModule(func(spec *rungrad.CommandSpec) {
				spec.RequiresAuth = false
			})},
			want: `requires_auth=false does not match built command true`,
		},
		{
			name: "mutates mismatch",
			mods: []rungrad.FeatureModule{catalogProbeModule(func(spec *rungrad.CommandSpec) {
				spec.Mutates = false
			})},
			want: `mutates=false does not match built command true`,
		},
		{
			name: "destructive mismatch",
			mods: []rungrad.FeatureModule{stubModule{
				groups: []rungrad.Group{{ID: "core", Title: "Core:"}},
				commands: []*rungrad.Command{{
					Use:         "probe",
					Short:       "Probe command",
					GroupID:     "core",
					Mutates:     true,
					Destructive: true,
				}},
				specs: []rungrad.CommandSpec{{
					Path:    "probe",
					Summary: "Probe command",
					GroupID: "core",
					Mutates: true,
				}},
			}},
			want: `destructive=false does not match built command true`,
		},
		{
			name: "supports meta mismatch",
			mods: []rungrad.FeatureModule{catalogProbeModule(func(spec *rungrad.CommandSpec) {
				spec.SupportsMeta = false
			})},
			want: `supports_meta=false does not match built command true`,
		},
		{
			name: "duplicate visible path",
			mods: []rungrad.FeatureModule{
				stubModule{
					commands: []*rungrad.Command{{Use: "dupe", Short: "First duplicate"}},
					specs:    []rungrad.CommandSpec{{Path: "dupe", Summary: "First duplicate"}},
				},
				stubModule{
					commands: []*rungrad.Command{{Use: "dupe", Short: "Second duplicate"}},
					specs:    []rungrad.CommandSpec{{Path: "dupe", Summary: "Second duplicate"}},
				},
			},
			want: `duplicate visible command path "dupe"`,
		},
		{
			name: "duplicate declared catalog path",
			mods: []rungrad.FeatureModule{stubModule{
				commands: []*rungrad.Command{{Use: "probe", Short: "Probe command"}},
				specs: []rungrad.CommandSpec{
					{Path: "probe", Summary: "Probe command"},
					{Path: "probe", Summary: "Probe command"},
				},
			}},
			want: `duplicate catalog path "probe"`,
		},
		{
			name: "top-level unregistered group",
			mods: []rungrad.FeatureModule{stubModule{
				commands: []*rungrad.Command{{Use: "probe", Short: "Probe command", GroupID: "missing"}},
				specs:    []rungrad.CommandSpec{{Path: "probe", Summary: "Probe command", GroupID: "missing"}},
			}},
			want: `command "probe" references unregistered help group "missing"`,
		},
		{
			name: "subcommand group resolved on parent",
			mods: []rungrad.FeatureModule{groupedSubcommandModule()},
			want: `command "parent child" references unregistered help group "core"`,
		},
		{
			name: "reserved manifest catalog path",
			mods: []rungrad.FeatureModule{stubModule{
				specs: []rungrad.CommandSpec{{Path: "__rungrad_manifest", Summary: "Reserved"}},
			}},
			want: `uses the reserved command name "__rungrad_manifest"`,
		},
		{
			name: "reserved nested catalog path",
			mods: []rungrad.FeatureModule{stubModule{
				specs: []rungrad.CommandSpec{{Path: "item help", Summary: "Reserved"}},
			}},
			want: `uses the reserved command name "help"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := rungrad.New(rungrad.AppConfig{Name: "rgmod", Short: "module test CLI"})
			app.AddModule(tt.mods...)
			err := app.ValidateCatalog()
			if err == nil {
				t.Fatal("ValidateCatalog() = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateCatalog() = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestFeatureModuleRegistrationPanics(t *testing.T) {
	tests := []struct {
		name string
		run  func(app *rungrad.App)
		want string
	}{
		{
			name: "nil module",
			run:  func(app *rungrad.App) { app.AddModule(nil) },
			want: "feature module at index 0 is nil",
		},
		{
			name: "reserved top-level manifest",
			run: func(app *rungrad.App) {
				app.AddModule(stubModule{commands: []*rungrad.Command{{Use: "__rungrad_manifest"}}})
			},
			want: "__rungrad_manifest",
		},
		{
			name: "reserved top-level help",
			run: func(app *rungrad.App) {
				app.AddModule(stubModule{commands: []*rungrad.Command{{Use: "help"}}})
			},
			want: "help",
		},
		{
			name: "reserved top-level completion",
			run: func(app *rungrad.App) {
				app.AddModule(stubModule{commands: []*rungrad.Command{{Use: "completion"}}})
			},
			want: "completion",
		},
		{
			name: "reserved subcommand manifest",
			run: func(app *rungrad.App) {
				parent := &rungrad.Command{Use: "item"}
				parent.AddCommand(&rungrad.Command{Use: "__rungrad_manifest"})
				app.AddModule(stubModule{commands: []*rungrad.Command{parent}})
			},
			want: "__rungrad_manifest",
		},
		{
			name: "reserved subcommand help",
			run: func(app *rungrad.App) {
				parent := &rungrad.Command{Use: "item"}
				parent.AddCommand(&rungrad.Command{Use: "help"})
				app.AddModule(stubModule{commands: []*rungrad.Command{parent}})
			},
			want: "help",
		},
		{
			name: "reserved subcommand completion",
			run: func(app *rungrad.App) {
				parent := &rungrad.Command{Use: "item"}
				parent.AddCommand(&rungrad.Command{Use: "completion"})
				app.AddModule(stubModule{commands: []*rungrad.Command{parent}})
			},
			want: "completion",
		},
		{
			name: "conflicting group titles",
			run: func(app *rungrad.App) {
				app.AddModule(
					stubModule{groups: []rungrad.Group{{ID: "core", Title: "Core:"}}},
					stubModule{groups: []rungrad.Group{{ID: "core", Title: "Different:"}}},
				)
			},
			want: `help group "core" already registered`,
		},
		{
			name: "empty group id",
			run: func(app *rungrad.App) {
				app.AddModule(stubModule{groups: []rungrad.Group{{Title: "Missing ID:"}}})
			},
			want: "help group requires a non-empty id and title",
		},
		{
			name: "empty group title",
			run: func(app *rungrad.App) {
				app.AddModule(stubModule{groups: []rungrad.Group{{ID: "missing-title"}}})
			},
			want: "help group requires a non-empty id and title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := rungrad.New(rungrad.AppConfig{Name: "rgmod", Short: "module test CLI"})
			assertPanicContains(t, tt.want, func() { tt.run(app) })
		})
	}
}

func TestFeatureModuleGroupSharing(t *testing.T) {
	app := rungrad.New(rungrad.AppConfig{Name: "rgmod", Short: "module test CLI"})
	app.AddModule(
		stubModule{
			groups:   []rungrad.Group{{ID: "shared", Title: "Shared:"}},
			commands: []*rungrad.Command{{Use: "alpha", Short: "Alpha command", GroupID: "shared"}},
			specs:    []rungrad.CommandSpec{{Path: "alpha", Summary: "Alpha command", GroupID: "shared"}},
		},
		stubModule{
			groups:   []rungrad.Group{{ID: "shared", Title: "Shared:"}},
			commands: []*rungrad.Command{{Use: "beta", Short: "Beta command", GroupID: "shared"}},
			specs:    []rungrad.CommandSpec{{Path: "beta", Summary: "Beta command", GroupID: "shared"}},
		},
	)
	if err := app.ValidateCatalog(); err != nil {
		t.Fatalf("ValidateCatalog() = %v", err)
	}
	help := testutil.Run(app, "--help")
	if help.Exit != rungrad.ExitSuccess {
		t.Fatalf("help exit %d stderr=%q", help.Exit, help.Stderr)
	}
	if got := strings.Count(help.Stdout, "Shared:"); got != 1 {
		t.Fatalf("Shared group count = %d, help:\n%s", got, help.Stdout)
	}
}

func TestFeatureModuleDeterminism(t *testing.T) {
	first := moduleApp()
	second := moduleApp()

	_, firstManifest := readManifest(t, first)
	_, secondManifest := readManifest(t, second)
	if firstManifest.Stdout != secondManifest.Stdout {
		t.Fatalf("manifest output not repeatable:\n%s\n---\n%s", firstManifest.Stdout, secondManifest.Stdout)
	}
	if firstDocs, secondDocs := docsgen.Generate(first), docsgen.Generate(second); !reflect.DeepEqual(firstDocs, secondDocs) {
		t.Fatalf("docs not repeatable:\n%v\n---\n%v", firstDocs, secondDocs)
	}
	if !reflect.DeepEqual(first.Catalog(), second.Catalog()) {
		t.Fatalf("catalog not repeatable:\n%+v\n---\n%+v", first.Catalog(), second.Catalog())
	}
}

// catalogProbeModule creates one command and one matching spec with every
// catalog field populated. Tests mutate one field to verify the first drift
// reported by ValidateCatalog.
func catalogProbeModule(mutators ...func(*rungrad.CommandSpec)) rungrad.FeatureModule {
	spec := rungrad.CommandSpec{
		Path:         "probe",
		Summary:      "Probe command",
		GroupID:      "core",
		OutputModes:  []string{"table", "json"},
		Examples:     []string{"rgmod probe"},
		Related:      []string{"rgmod other"},
		RequiresAuth: true,
		Mutates:      true,
		SupportsMeta: true,
	}
	for _, mutate := range mutators {
		mutate(&spec)
	}
	return stubModule{
		groups: []rungrad.Group{{ID: "core", Title: "Core:"}},
		commands: []*rungrad.Command{{
			Use:          "probe",
			Short:        "Probe command",
			GroupID:      "core",
			OutputModes:  []string{"table", "json"},
			Examples:     []string{"rgmod probe"},
			Related:      []string{"rgmod other"},
			RequiresAuth: true,
			Mutates:      true,
			SupportsMeta: true,
		}},
		specs: []rungrad.CommandSpec{spec},
	}
}

// groupedSubcommandModule sets a subcommand GroupID that names a root group.
// The spec matches that field, so only the parent-relative group walk can catch
// the error Cobra would otherwise raise during Execute.
func groupedSubcommandModule() rungrad.FeatureModule {
	parent := &rungrad.Command{Use: "parent", Short: "Parent command"}
	parent.AddCommand(&rungrad.Command{Use: "child", Short: "Child command", GroupID: "core"})
	return stubModule{
		groups:   []rungrad.Group{{ID: "core", Title: "Core:"}},
		commands: []*rungrad.Command{parent},
		specs: []rungrad.CommandSpec{
			{Path: "parent", Summary: "Parent command"},
			{Path: "parent child", Summary: "Child command", GroupID: "core"},
		},
	}
}

func specPathList(specs []rungrad.CommandSpec) []string {
	out := make([]string, len(specs))
	for i, spec := range specs {
		out[i] = spec.Path
	}
	return out
}

func moduleDocKeys(docs map[string]string) []string {
	out := make([]string, 0, len(docs))
	for k := range docs {
		out = append(out, k)
	}
	return out
}

func assertPanicContains(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic containing %q", want)
		}
		msg := fmt.Sprint(r)
		if !strings.Contains(msg, want) {
			t.Fatalf("panic = %q, want substring %q", msg, want)
		}
	}()
	fn()
}
