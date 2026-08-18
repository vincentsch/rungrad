package docsgen_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/docsgen"
	"github.com/vincentsch/rungrad/manifest"
)

func demoApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{Name: "rgdemo", Short: "demo tool", Version: "1.0.0"})
	app.AddCommand(&rungrad.Command{
		Use:          "ping",
		Short:        "print pong",
		Examples:     []string{"rgdemo ping"},
		Related:      []string{"whoami"},
		OutputModes:  []string{"table", "json"},
		RequiresAuth: false,
		Run:          func(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil },
	})
	return app
}

func destructiveApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{Name: "rgdemo", Short: "demo tool", Version: "1.0.0"})
	app.AddCommand(&rungrad.Command{
		Use:         "delete <name>",
		Short:       "delete a widget",
		Destructive: true,
		Args:        cobra.ExactArgs(1),
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().Bool("confirm", false, "Confirm without a prompt")
		},
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil },
	})
	return app
}

func advancedOutputApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:           "rgadvanced",
		Short:          "advanced output tool",
		Version:        "1.0.0",
		AdvancedOutput: true,
	})
	app.AddCommand(&rungrad.Command{
		Use:         "read",
		Short:       "read data",
		OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
		Run:         func(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil },
	})
	return app
}

func resolutionApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:  "rgres",
		Short: "resolution tool",
		Resolution: &rungrad.ResolutionConfig{
			Profile:  true,
			AuthFile: true,
			Services: []rungrad.Service{
				{Name: "api", Flag: "base-url", EnvVar: "RGRES_BASE_URL", ConfigKey: "base_url", Default: "https://api.default", Usage: "API base URL"},
				{Name: "region", EnvVar: "RGRES_REGION", ConfigKey: "region", Default: "us"},
			},
		},
	})
	app.AddCommand(&rungrad.Command{
		Use:   "show",
		Short: "show resolution",
		Run:   func(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil },
	})
	return app
}

func metaApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:           "rgmeta",
		Short:          "metadata output tool",
		Version:        "1.0.0",
		AdvancedOutput: true,
	})
	app.AddCommand(&rungrad.Command{
		Use:          "show",
		Short:        "show data",
		OutputModes:  []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
		SupportsMeta: true,
		Run:          func(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil },
	})
	app.AddCommand(&rungrad.Command{
		Use:          "jsononly",
		Short:        "show json-only data",
		OutputModes:  []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
		SupportsMeta: true,
		Run:          func(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil },
	})
	return app
}

func extensionDocsApp(ext manifest.ExtensionSet) *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{Name: "rgextdocs", Short: "extension docs tool", Version: "1.0.0"})
	app.AddCommand(&rungrad.Command{
		Use:        "read",
		Short:      "read data",
		Examples:   []string{"rgextdocs read"},
		Extensions: ext,
		Run:        func(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil },
	})
	return app
}

func docsSurfaceBindings() rungrad.GlobalFlagBindings {
	return rungrad.GlobalFlagBindings{
		JSON:     rungrad.GlobalFlagBinding{Name: "machine-json"},
		DryRun:   rungrad.GlobalFlagBinding{Name: "preview"},
		NoPrompt: rungrad.GlobalFlagBinding{Name: "silent"},
		Quiet:    rungrad.GlobalFlagBinding{Name: "hush"},
		Config:   rungrad.GlobalFlagBinding{Name: "conf"},
	}
}

func docsSurfaceApp(surface rungrad.SurfaceConfig) *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:    "rgdocsurf",
		Short:   "docs surface",
		Version: "1.2.3",
		Surface: surface,
	})
	app.AddCommand(&rungrad.Command{
		Use:   "read",
		Short: "read data",
		Run:   func(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil },
	})
	return app
}

func TestGenerateProducesPagesAndIndex(t *testing.T) {
	docs := docsgen.Generate(demoApp())
	if _, ok := docs["index.md"]; !ok {
		t.Fatalf("missing index.md")
	}
	page, ok := docs["rgdemo_ping.md"]
	if !ok {
		t.Fatalf("missing ping page; got %v", keys(docs))
	}
	for _, want := range []string{"# rgdemo ping", "## Usage", "## Examples", "## Related commands"} {
		if !strings.Contains(page, want) {
			t.Errorf("ping page missing %q:\n%s", want, page)
		}
	}
	if idx := docs["index.md"]; !strings.Contains(idx, "## Global flags") || !strings.Contains(idx, "--json") {
		t.Fatalf("index missing global flags section:\n%s", idx)
	}
}

func TestSurfaceDocsRenderHostOwnedAndHiddenGlobals(t *testing.T) {
	bindings := docsSurfaceBindings()
	app := docsSurfaceApp(rungrad.SurfaceConfig{
		GlobalFlags: rungrad.GlobalFlagSurface{Mode: rungrad.SurfaceHostOwned, Bindings: bindings},
	})
	index := docsgen.Generate(app)["index.md"]
	for _, want := range []string{"--machine-json", "--conf"} {
		if !strings.Contains(index, want) {
			t.Fatalf("index missing host-owned flag %q:\n%s", want, index)
		}
	}
	for _, notWant := range []string{"--json", "--config"} {
		if strings.Contains(index, notWant) {
			t.Fatalf("index contains old rungrad flag %q:\n%s", notWant, index)
		}
	}

	bindings.JSON.Hidden = true
	app = docsSurfaceApp(rungrad.SurfaceConfig{
		GlobalFlags: rungrad.GlobalFlagSurface{Mode: rungrad.SurfaceHostOwned, Bindings: bindings},
	})
	index = docsgen.Generate(app)["index.md"]
	if strings.Contains(index, "--machine-json") {
		t.Fatalf("hidden host-owned flag appeared in docs index:\n%s", index)
	}
}

func TestSurfaceDocsHostOwnedCompletionIsVisible(t *testing.T) {
	app := docsSurfaceApp(rungrad.SurfaceConfig{Completion: rungrad.SurfaceHostOwned})
	completion := &rungrad.Command{Use: "completion", Short: "shell completion"}
	completion.AddCommand(&rungrad.Command{Use: "bash", Short: "bash completion", Run: func(*rungrad.Factory, *cobra.Command, []string) error { return nil }})
	app.AddCommand(completion)
	docs := docsgen.Generate(app)
	if _, ok := docs["rgdocsurf_completion.md"]; !ok {
		t.Fatalf("missing host-owned completion page; got %v", keys(docs))
	}
	if !strings.Contains(docs["index.md"], "rgdocsurf completion") {
		t.Fatalf("index missing host-owned completion:\n%s", docs["index.md"])
	}
}

func TestSurfaceDocsVersionDisabledOmitsVersionFlag(t *testing.T) {
	index := docsgen.Generate(docsSurfaceApp(rungrad.SurfaceConfig{Version: rungrad.SurfaceDisabled}))["index.md"]
	if strings.Contains(index, "--version") {
		t.Fatalf("version-disabled index contains --version:\n%s", index)
	}
}

func TestAdvancedOutputDocsRenderFlagsModesAndRepeat(t *testing.T) {
	first := docsgen.Generate(advancedOutputApp())
	second := docsgen.Generate(advancedOutputApp())
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("advanced docs not repeatable:\n%v\n---\n%v", first, second)
	}
	index := first["index.md"]
	for _, want := range []string{"--plain", "--jq", "--template", "--include-meta", "--no-color", "--no-ansi", "--no-pager"} {
		if !strings.Contains(index, want) {
			t.Fatalf("index missing %q:\n%s", want, index)
		}
	}
	page := first["rgadvanced_read.md"]
	if !strings.Contains(page, "## Output modes\n\nhuman, json, jq, template\n") {
		t.Fatalf("read page missing advanced output modes:\n%s", page)
	}
}

func TestResolutionDocsRenderFlagsAndCompactOmission(t *testing.T) {
	index := docsgen.Generate(resolutionApp())["index.md"]
	for _, want := range []string{"--profile", "--auth-file", "--base-url"} {
		if !strings.Contains(index, want) {
			t.Fatalf("index missing %q:\n%s", want, index)
		}
	}

	compact := docsgen.Generate(demoApp())["index.md"]
	for _, notWant := range []string{"--profile", "--auth-file", "--base-url"} {
		if strings.Contains(compact, notWant) {
			t.Fatalf("compact index unexpectedly contains %q:\n%s", notWant, compact)
		}
	}
}

func TestMetadataCommandRendersMetadataSection(t *testing.T) {
	docs := docsgen.Generate(metaApp())
	page := docs["rgmeta_show.md"]
	if !strings.Contains(page, "## Metadata") || !strings.Contains(page, "--include-meta") || !strings.Contains(page, "supported machine output mode") {
		t.Fatalf("metadata command page missing metadata section:\n%s", page)
	}
	jsonOnly := docs["rgmeta_jsononly.md"]
	if strings.Contains(jsonOnly, "`--jq`") || strings.Contains(jsonOnly, "`--template`") {
		t.Fatalf("json-only metadata page advertised unsupported transform flags:\n%s", jsonOnly)
	}

	plain := docsgen.Generate(demoApp())["rgdemo_ping.md"]
	if strings.Contains(plain, "## Metadata") {
		t.Fatalf("non-metadata command page unexpectedly has metadata section:\n%s", plain)
	}
}

func TestCommandExtensionsDoNotRenderInDocs(t *testing.T) {
	without := docsgen.Generate(extensionDocsApp(nil))
	with := docsgen.Generate(extensionDocsApp(manifest.ExtensionSet{
		"example.com/product": {
			"owner":     "platform",
			"status":    "beta",
			"docs_path": "docs/read.md",
		},
	}))
	if !reflect.DeepEqual(with, without) {
		t.Fatalf("extension docs changed:\n%v\n---\n%v", with, without)
	}
}

func TestCommandPageExactOutput(t *testing.T) {
	docs := docsgen.Generate(demoApp())
	// This pins the committed-doc formatting contract, including a single final
	// newline and no extra blank line at EOF.
	want := "# rgdemo ping\n\n" +
		"print pong\n\n" +
		"## Usage\n\n" +
		"```\n" +
		"rgdemo ping\n" +
		"```\n\n" +
		"## Examples\n\n" +
		"```\n" +
		"rgdemo ping\n" +
		"```\n\n" +
		"## Output modes\n\n" +
		"table, json\n\n" +
		"## Related commands\n\n" +
		"- whoami\n"
	if got := docs["rgdemo_ping.md"]; got != want {
		t.Fatalf("rgdemo_ping.md =\n%q\nwant\n%q", got, want)
	}
}

func TestRootExamplesRenderFromCobraExample(t *testing.T) {
	app := demoApp()
	app.Root().Example = "rgdemo ping\nrgdemo ping --json"
	docs := docsgen.Generate(app)
	root, ok := docs["rgdemo.md"]
	if !ok {
		t.Fatalf("missing root page; got %v", keys(docs))
	}
	for _, want := range []string{"## Examples", "rgdemo ping --json"} {
		if !strings.Contains(root, want) {
			t.Errorf("root page missing %q:\n%s", want, root)
		}
	}
}

func TestDestructiveCommandRendersBothSections(t *testing.T) {
	docs := docsgen.Generate(destructiveApp())
	page, ok := docs["rgdemo_delete.md"]
	if !ok {
		t.Fatalf("missing delete page; got %v", keys(docs))
	}
	for _, want := range []string{"## Destructive", "## Changes state", "- `--confirm`"} {
		if !strings.Contains(page, want) {
			t.Errorf("delete page missing %q:\n%s", want, page)
		}
	}
}

func TestCheckDetectsDrift(t *testing.T) {
	app := demoApp()
	dir := t.TempDir()
	result, err := docsgen.Check(app, dir)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if want := keys(docsgen.Generate(app)); !reflect.DeepEqual(result.Missing, want) {
		t.Fatalf("missing = %v, want %v", result.Missing, want)
	}
	if len(result.Stale) != 0 || len(result.Orphaned) != 0 {
		t.Fatalf("unexpected stale/orphaned drift: %+v", result)
	}
}

func TestCheckReportsAlteredPathExactly(t *testing.T) {
	app := demoApp()
	dir := t.TempDir()
	writeDocs(t, dir, docsgen.Generate(app))
	if err := os.WriteFile(filepath.Join(dir, "rgdemo_ping.md"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := docsgen.Check(app, dir)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if want := []string{"rgdemo_ping.md"}; !reflect.DeepEqual(result.Stale, want) {
		t.Fatalf("stale = %v, want %v", result.Stale, want)
	}
	if len(result.Missing) != 0 || len(result.Orphaned) != 0 {
		t.Fatalf("unexpected missing/orphaned drift: %+v", result)
	}
}

func TestCheckReportsOrphanedCommittedPage(t *testing.T) {
	app := demoApp()
	dir := t.TempDir()
	writeDocs(t, dir, docsgen.Generate(app))
	if err := os.WriteFile(filepath.Join(dir, "old_command.md"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := docsgen.Check(app, dir)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if want := []string{"old_command.md"}; !reflect.DeepEqual(result.Orphaned, want) {
		t.Fatalf("orphaned = %v, want %v", result.Orphaned, want)
	}
	if len(result.Missing) != 0 || len(result.Stale) != 0 {
		t.Fatalf("unexpected missing/stale drift: %+v", result)
	}
}

func TestWriteRegeneratesAndChecksClean(t *testing.T) {
	app := demoApp()
	dir := t.TempDir()
	if err := docsgen.Write(app, dir); err != nil {
		t.Fatalf("write: %v", err)
	}
	result, err := docsgen.Check(app, dir)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !result.OK() {
		t.Fatalf("write then check drifted: %+v", result)
	}

	if err := os.WriteFile(filepath.Join(dir, "rgdemo_ping.md"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err = docsgen.Check(app, dir)
	if err != nil {
		t.Fatalf("check stale: %v", err)
	}
	if want := []string{"rgdemo_ping.md"}; !reflect.DeepEqual(result.Stale, want) {
		t.Fatalf("stale = %v, want %v", result.Stale, want)
	}

	if err := docsgen.Write(app, dir); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	result, err = docsgen.Check(app, dir)
	if err != nil {
		t.Fatalf("check rewritten: %v", err)
	}
	if !result.OK() {
		t.Fatalf("rewrite left drift: %+v", result)
	}
}

func TestWritePrunesOrphans(t *testing.T) {
	app := demoApp()
	dir := t.TempDir()
	if err := docsgen.Write(app, dir); err != nil {
		t.Fatalf("write: %v", err)
	}
	orphan := filepath.Join(dir, "old_command.md")
	if err := os.WriteFile(orphan, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := docsgen.Check(app, dir)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if want := []string{"old_command.md"}; !reflect.DeepEqual(result.Orphaned, want) {
		t.Fatalf("orphaned = %v, want %v", result.Orphaned, want)
	}

	if err := docsgen.Write(app, dir); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan stat error = %v, want not exist", err)
	}
	result, err = docsgen.Check(app, dir)
	if err != nil {
		t.Fatalf("check rewritten: %v", err)
	}
	if !result.OK() {
		t.Fatalf("rewrite left drift: %+v", result)
	}
}

func writeDocs(t *testing.T, dir string, docs map[string]string) {
	t.Helper()
	for path, content := range docs {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
