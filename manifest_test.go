package rungrad_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/config"
	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/testutil"
)

func findManifestCommand(m *manifest.Manifest, path ...string) *manifest.Command {
	if path == nil {
		path = []string{}
	}
	for i := range m.Commands {
		if reflect.DeepEqual(m.Commands[i].Path, path) {
			return &m.Commands[i]
		}
	}
	return nil
}

func readManifest(t *testing.T, app *rungrad.App) (manifest.Manifest, testutil.Result) {
	t.Helper()
	res := testutil.Run(app, "__rungrad_manifest")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("manifest exit %d, stderr=%q", res.Exit, res.Stderr)
	}
	var m manifest.Manifest
	if err := res.JSON(&m); err != nil {
		t.Fatalf("manifest JSON: %v\n%s", err, res.Stdout)
	}
	return m, res
}

func TestManifestCommandExistsAndExitsZero(t *testing.T) {
	res := testutil.Run(demoApp(), "__rungrad_manifest")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("manifest exit %d, stderr=%q", res.Exit, res.Stderr)
	}
	if res.Stdout == "" {
		t.Fatal("manifest stdout is empty")
	}
}

func TestManifestCommandEmitsValidStableJSON(t *testing.T) {
	var m manifest.Manifest
	res := testutil.Run(demoApp(), "__rungrad_manifest")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("manifest exit %d, stderr=%q", res.Exit, res.Stderr)
	}
	testutil.AssertStableJSON(t, res.Stdout, &m)
	if err := manifest.Validate(&m); err != nil {
		t.Fatalf("Validate(manifest) = %v\n%s", err, res.Stdout)
	}
}

func TestManifestEmptyArraysAreNotNull(t *testing.T) {
	_, res := readManifest(t, demoApp())
	for _, want := range []string{`"path": []`, `"related": []`, `"local_flags": []`} {
		if !strings.Contains(res.Stdout, want) {
			t.Fatalf("manifest missing %s:\n%s", want, res.Stdout)
		}
	}
	if strings.Contains(res.Stdout, "null") {
		t.Fatalf("manifest contains null arrays:\n%s", res.Stdout)
	}
}

func TestManifestCommandIsByteIdenticalAcrossRuns(t *testing.T) {
	a := testutil.Run(demoApp(), "__rungrad_manifest")
	b := testutil.Run(demoApp(), "__rungrad_manifest")
	if a.Exit != rungrad.ExitSuccess || b.Exit != rungrad.ExitSuccess {
		t.Fatalf("manifest exits %d/%d stderr=%q/%q", a.Exit, b.Exit, a.Stderr, b.Stderr)
	}
	if a.Stdout != b.Stdout {
		t.Fatalf("manifest output not repeatable:\n%s\n---\n%s", a.Stdout, b.Stdout)
	}
}

func TestManifestCommandHiddenFromHelp(t *testing.T) {
	res := testutil.Run(demoApp(), "--help")
	if strings.Contains(res.Stdout, "__rungrad_manifest") {
		t.Fatalf("manifest command appears in help:\n%s", res.Stdout)
	}
}

func TestManifestCommandAbsentFromManifestCommands(t *testing.T) {
	m, _ := readManifest(t, demoApp())
	for _, cmd := range m.Commands {
		if cmd.Use == "__rungrad_manifest" {
			t.Fatalf("manifest command appeared by use: %+v", cmd)
		}
		for _, seg := range cmd.Path {
			if seg == "__rungrad_manifest" {
				t.Fatalf("manifest command appeared by path: %+v", cmd)
			}
		}
	}
}

func TestManifestHermeticUnderRequiredRootFlagAuthAndRequiredLocalFlag(t *testing.T) {
	t.Setenv("RGDEMO_TOKEN", "")
	app := rungrad.New(rungrad.AppConfig{Name: "rgdemo", Short: "demo", EnvVar: "RGDEMO_TOKEN"})
	app.Root().PersistentFlags().String("tenant", "", "tenant")
	if err := app.Root().MarkPersistentFlagRequired("tenant"); err != nil {
		t.Fatalf("mark tenant required: %v", err)
	}
	app.AddCommand(
		&rungrad.Command{
			Use:          "whoami",
			RequiresAuth: true,
			Run:          func(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil },
		},
		&rungrad.Command{
			Use: "needs-flag",
			Configure: func(cmd *cobra.Command) {
				cmd.Flags().String("name", "", "name")
				if err := cmd.MarkFlagRequired("name"); err != nil {
					t.Fatalf("mark name required: %v", err)
				}
			},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil },
		},
	)

	m, res := readManifest(t, app)
	if err := manifest.Validate(&m); err != nil {
		t.Fatalf("Validate(manifest) = %v\n%s", err, res.Stdout)
	}
}

func TestManifestLeavesFactoryStoreAndTokenUnset(t *testing.T) {
	app := rungrad.New(rungrad.AppConfig{Name: "rgdemo", Short: "demo", EnvVar: "RGDEMO_TOKEN"})
	res := testutil.Run(app, "__rungrad_manifest")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("manifest exit %d, stderr=%q", res.Exit, res.Stderr)
	}
	if app.Factory().Store != (config.Store{}) {
		t.Fatalf("factory store = %+v, want zero", app.Factory().Store)
	}
	if app.Factory().Token != "" {
		t.Fatalf("factory token = %q, want empty", app.Factory().Token)
	}
}

func TestManifestReservedCommandNamePanics(t *testing.T) {
	app := rungrad.New(rungrad.AppConfig{Name: "rgdemo", Short: "demo"})
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("AddCommand with reserved manifest name did not panic")
		}
		if !strings.Contains(r.(string), "__rungrad_manifest") {
			t.Fatalf("panic %q does not name reserved command", r)
		}
	}()
	app.AddCommand(&rungrad.Command{
		Use: "__rungrad_manifest",
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil },
	})
}

func TestManifestIncludesCommandAddedAfterNew(t *testing.T) {
	app := rungrad.New(rungrad.AppConfig{Name: "rgdemo", Short: "demo"})
	app.AddCommand(&rungrad.Command{
		Use: "ping",
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil },
	})
	m, _ := readManifest(t, app)
	if findManifestCommand(&m, "ping") == nil {
		t.Fatalf("manifest missing command added after New: %+v", m.Commands)
	}
}

func TestManifestRootEntryShape(t *testing.T) {
	app := demoApp()
	app.Root().Example = "rgdemo ping\nrgdemo create demo"
	m, _ := readManifest(t, app)
	root := findManifestCommand(&m)
	if root == nil {
		t.Fatal("manifest missing root entry")
	}
	if root.Path == nil || len(root.Path) != 0 {
		t.Fatalf("root path = %#v, want []", root.Path)
	}
	if want := []string{"rgdemo ping", "rgdemo create demo"}; !reflect.DeepEqual(root.Examples, want) {
		t.Fatalf("root examples = %v, want %v", root.Examples, want)
	}
	if len(root.Related) != 0 {
		t.Fatalf("root related = %v, want []", root.Related)
	}
	if len(root.LocalFlags) != 0 {
		t.Fatalf("root local flags = %v, want []", root.LocalFlags)
	}
}

func TestManifestMetadataMatchesAnnotations(t *testing.T) {
	m, _ := readManifest(t, demoApp())
	create := findManifestCommand(&m, "create")
	if create == nil || !create.Mutates || !create.SupportsDryRun || create.Destructive {
		t.Fatalf("create metadata = %+v", create)
	}
	deleteCmd := findManifestCommand(&m, "delete")
	if deleteCmd == nil || !deleteCmd.Mutates || !deleteCmd.SupportsDryRun || !deleteCmd.Destructive || !deleteCmd.RequiresConfirmation {
		t.Fatalf("delete metadata = %+v", deleteCmd)
	}
	whoami := findManifestCommand(&m, "whoami")
	if whoami == nil || !whoami.RequiresAuth {
		t.Fatalf("whoami metadata = %+v", whoami)
	}
}

func TestManifestAdvancedOutputFlagsAndModes(t *testing.T) {
	m, first := readManifest(t, newAdvancedOutputApp(true, nil))
	if err := manifest.Validate(&m); err != nil {
		t.Fatalf("Validate(manifest) = %v\n%s", err, first.Stdout)
	}
	_, second := readManifest(t, newAdvancedOutputApp(true, nil))
	if first.Stdout != second.Stdout {
		t.Fatalf("advanced manifest not repeatable:\n%s\n---\n%s", first.Stdout, second.Stdout)
	}

	for _, tt := range []struct {
		name string
		typ  string
		want string
	}{
		{"plain", "bool", "Print unstyled"},
		{"jq", "string", "Transform stable JSON"},
		{"template", "string", "Render stable JSON"},
		{"include-meta", "bool", "Wrap machine output"},
		{"no-color", "bool", "Disable color"},
		{"no-ansi", "bool", "Disable all ANSI"},
		{"no-pager", "bool", "Never use a pager"},
	} {
		flag := findManifestFlag(m.GlobalFlags, tt.name)
		if flag == nil {
			t.Fatalf("manifest missing global flag %q: %+v", tt.name, m.GlobalFlags)
		}
		if flag.Type != tt.typ || !strings.Contains(flag.Usage, tt.want) {
			t.Fatalf("flag %q = %+v, want type %q usage containing %q", tt.name, *flag, tt.typ, tt.want)
		}
	}

	read := findManifestCommand(&m, "read")
	if read == nil {
		t.Fatal("manifest missing read command")
	}
	wantModes := []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModeJQ, rungrad.OutputModeTemplate}
	if !reflect.DeepEqual(read.OutputModes, wantModes) {
		t.Fatalf("read output modes = %v, want %v", read.OutputModes, wantModes)
	}
}

func TestManifestNonAdvancedOmitsAdvancedOutputFlags(t *testing.T) {
	m, _ := readManifest(t, newAdvancedOutputApp(false, nil))
	for _, name := range []string{"plain", "jq", "template", "include-meta", "no-color", "no-ansi", "no-pager"} {
		if flag := findManifestFlag(m.GlobalFlags, name); flag != nil {
			t.Fatalf("non-advanced manifest unexpectedly contains %q: %+v", name, *flag)
		}
	}
}

func TestManifestResolutionFlagsAreOptIn(t *testing.T) {
	m, _ := readManifest(t, resolutionTestApp(nil, nil))
	for _, tt := range []struct {
		name string
		typ  string
		want string
	}{
		{"profile", "string", "Profile"},
		{"auth-file", "string", "credentials file"},
		{"base-url", "string", "API base URL"},
	} {
		flag := findManifestFlag(m.GlobalFlags, tt.name)
		if flag == nil {
			t.Fatalf("manifest missing global flag %q: %+v", tt.name, m.GlobalFlags)
		}
		if flag.Type != tt.typ || !strings.Contains(flag.Usage, tt.want) {
			t.Fatalf("flag %q = %+v, want type %q usage containing %q", tt.name, *flag, tt.typ, tt.want)
		}
	}

	compact, _ := readManifest(t, demoApp())
	for _, name := range []string{"profile", "auth-file", "base-url"} {
		if flag := findManifestFlag(compact.GlobalFlags, name); flag != nil {
			t.Fatalf("compact manifest unexpectedly contains %q: %+v", name, *flag)
		}
	}
}

func TestManifestHostOwnedSurfaceProjection(t *testing.T) {
	app := newSurfaceApp(surfaceHostSurface(surfaceHostBindings()), nil)
	m, _ := readManifest(t, app)
	for _, name := range []string{"machine-json", "query", "tmpl", "api-endpoint"} {
		if flag := findManifestFlag(m.GlobalFlags, name); flag == nil {
			t.Fatalf("host-owned manifest missing %q: %+v", name, m.GlobalFlags)
		}
	}
	for _, name := range []string{"json", "jq", "template", "api-url"} {
		if flag := findManifestFlag(m.GlobalFlags, name); flag != nil {
			t.Fatalf("host-owned manifest included old name %q: %+v", name, *flag)
		}
	}

	hidden := surfaceHostBindings()
	hidden.JQ.Hidden = true
	app = newSurfaceApp(surfaceHostSurface(hidden), nil)
	m, _ = readManifest(t, app)
	if flag := findManifestFlag(m.GlobalFlags, "query"); flag != nil {
		t.Fatalf("hidden host-owned flag appeared in manifest: %+v", *flag)
	}
}

func TestManifestHostOwnedCompletionIsVisible(t *testing.T) {
	app := rungrad.New(surfaceConfig(rungrad.SurfaceConfig{Completion: rungrad.SurfaceHostOwned}))
	completion := &rungrad.Command{Use: "completion", Short: "shell completion"}
	completion.AddCommand(&rungrad.Command{Use: "bash", Short: "bash completion", Run: func(*rungrad.Factory, *cobra.Command, []string) error { return nil }})
	app.AddCommand(completion)
	m, _ := readManifest(t, app)
	if findManifestCommand(&m, "completion") == nil || findManifestCommand(&m, "completion", "bash") == nil {
		t.Fatalf("host-owned completion missing from manifest: %+v", m.Commands)
	}
}

func TestManifestVersionSuppressedKeepsToolVersion(t *testing.T) {
	app := newSurfaceApp(rungrad.SurfaceConfig{Version: rungrad.SurfaceDisabled}, nil)
	m, _ := readManifest(t, app)
	if m.ToolVersion != "v9.8.7" {
		t.Fatalf("tool_version = %q", m.ToolVersion)
	}
	if flag := findManifestFlag(m.GlobalFlags, "version"); flag != nil {
		t.Fatalf("manifest unexpectedly contains version flag: %+v", *flag)
	}
}

func findManifestFlag(flags []manifest.Flag, name string) *manifest.Flag {
	for i := range flags {
		if flags[i].Name == name {
			return &flags[i]
		}
	}
	return nil
}
