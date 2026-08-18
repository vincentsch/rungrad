package testutil

import (
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/manifest"
)

func TestCheckConsistencyClean(t *testing.T) {
	m := manifest.Manifest{
		ToolName: "tool",
		Commands: []manifest.Command{{
			Path:         []string{"item", "list"},
			Examples:     []string{"tool item list"},
			Related:      []string{"tool item create"},
			OutputModes:  []string{"table", "json"},
			RequiresAuth: true,
			Mutates:      true,
			Destructive:  true,
			SupportsMeta: true,
		}},
	}
	docs := map[string]string{
		"tool_item_list.md": "tool item list\n\n## Output modes\n\ntable, json\n\n## Authentication\n\n## Changes state\n\n## Destructive\n\n## Metadata\n\n## Related commands\n\n- tool item create\n",
	}
	help := map[string]string{
		"item list": "Usage:\n  tool item list [flags]\n\nExamples:\ntool item list\n\nOutput modes:\n  table, json\nRelated commands:\n  tool item create\n",
	}
	if issues := CheckConsistency(m, docs, help); len(issues) != 0 {
		t.Fatalf("issues = %v, want none", issues)
	}
}

func TestCheckConsistencyReportsMissingExamples(t *testing.T) {
	m := manifest.Manifest{
		ToolName: "tool",
		Commands: []manifest.Command{{
			Path:     []string{"ping"},
			Examples: []string{"tool ping"},
		}},
	}
	docs := map[string]string{"tool_ping.md": "no example\n"}
	help := map[string]string{"ping": "no example\n"}
	got := CheckConsistency(m, docs, help)
	want := []string{
		"ping: examples missing from docs",
		"ping: examples missing from help",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %v, want %v", got, want)
	}
}

func TestCheckConsistencyReportsExamplesMissingFromHelpSection(t *testing.T) {
	m := manifest.Manifest{
		ToolName: "tool",
		Commands: []manifest.Command{{
			Path:     []string{"ping"},
			Examples: []string{"tool ping"},
		}},
	}
	docs := map[string]string{"tool_ping.md": "## Examples\n\n```\ntool ping\n```\n"}
	help := map[string]string{"ping": "Usage:\n  tool ping [flags]\n"}
	got := CheckConsistency(m, docs, help)
	want := []string{"ping: examples missing from help"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %v, want %v", got, want)
	}
}

func TestCheckConsistencyReportsMissingPrefixExampleFromHelpSection(t *testing.T) {
	m := manifest.Manifest{
		ToolName: "tool",
		Commands: []manifest.Command{{
			Path:     []string{"ping"},
			Examples: []string{"tool ping", "tool ping --json"},
		}},
	}
	docs := map[string]string{"tool_ping.md": "## Examples\n\n```\ntool ping\ntool ping --json\n```\n"}
	help := map[string]string{"ping": "Examples:\ntool ping --json\n"}
	got := CheckConsistency(m, docs, help)
	want := []string{"ping: examples missing from help"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %v, want %v", got, want)
	}
}

func TestCheckConsistencyReportsStaleOutputModes(t *testing.T) {
	m := manifest.Manifest{
		ToolName: "tool",
		Commands: []manifest.Command{{
			Path:        []string{"ping"},
			OutputModes: []string{"human", "json"},
		}},
	}
	docs := map[string]string{"tool_ping.md": "missing modes\n"}
	help := map[string]string{"ping": "missing modes\n"}
	got := CheckConsistency(m, docs, help)
	want := []string{
		"ping: output modes missing from docs",
		"ping: output modes missing from help",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %v, want %v", got, want)
	}
}

func TestCheckConsistencyReportsMissingDocsPage(t *testing.T) {
	m := manifest.Manifest{
		ToolName: "tool",
		Commands: []manifest.Command{{Path: []string{"ping"}}},
	}
	got := CheckConsistency(m, nil, map[string]string{"ping": "help\n"})
	want := []string{"ping: no generated docs page tool_ping.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues = %v, want %v", got, want)
	}
}

func TestAssertConsistentReferenceShapes(t *testing.T) {
	AssertConsistent(t, consistencyApp)
}

func TestAssertConsistentExtensionCatalog(t *testing.T) {
	AssertConsistent(t, consistencyExtensionApp)
}

func TestAssertConsistentReportsExtensionCatalogDrift(t *testing.T) {
	if os.Getenv("RUNGRAD_ASSERT_CONSISTENT_DRIFT") == "1" {
		AssertConsistent(t, consistencyExtensionDriftApp)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestAssertConsistentReportsExtensionCatalogDrift$")
	cmd.Env = append(os.Environ(), "RUNGRAD_ASSERT_CONSISTENT_DRIFT=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("AssertConsistent drift subprocess passed, want failure")
	}
	text := string(out)
	for _, want := range []string{"catalog:", "extensions"} {
		if !strings.Contains(text, want) {
			t.Fatalf("subprocess output missing %q:\n%s", want, text)
		}
	}
}

type consistencyModule struct{}

func (consistencyModule) Groups() []rungrad.Group {
	return []rungrad.Group{{ID: "core", Title: "Core:"}}
}

func (consistencyModule) Commands() []*rungrad.Command {
	item := &rungrad.Command{
		Use:         "item",
		Short:       "Work with items",
		GroupID:     "core",
		Examples:    []string{"rgcheck item list"},
		Related:     []string{"rgcheck whoami"},
		OutputModes: []string{"table", "json"},
	}
	item.AddCommand(
		&rungrad.Command{
			Use:          "list",
			Short:        "List items",
			Examples:     []string{"rgcheck item list", "rgcheck item list --json"},
			Related:      []string{"rgcheck whoami"},
			OutputModes:  []string{"table", "json"},
			SupportsMeta: true,
			Run:          noopRun,
		},
		&rungrad.Command{
			Use:         "create <name>",
			Short:       "Create an item",
			Examples:    []string{"rgcheck item create demo"},
			OutputModes: []string{"table", "json"},
			Mutates:     true,
			Run:         noopRun,
		},
		&rungrad.Command{
			Use:         "delete <name>",
			Short:       "Delete an item",
			Examples:    []string{"rgcheck item delete demo --dry-run"},
			Related:     []string{"rgcheck item list"},
			OutputModes: []string{"table", "json"},
			Destructive: true,
			Run:         noopRun,
		},
	)
	return []*rungrad.Command{
		item,
		{
			Use:          "whoami",
			Short:        "Show identity",
			GroupID:      "core",
			Examples:     []string{"rgcheck whoami"},
			Related:      []string{"rgcheck item list"},
			OutputModes:  []string{"table", "json"},
			RequiresAuth: true,
			SupportsMeta: true,
			Run:          noopRun,
		},
	}
}

func (consistencyModule) Catalog() []rungrad.CommandSpec {
	return []rungrad.CommandSpec{
		{
			Path:        "item",
			Summary:     "Work with items",
			GroupID:     "core",
			Examples:    []string{"rgcheck item list"},
			Related:     []string{"rgcheck whoami"},
			OutputModes: []string{"table", "json"},
		},
		{
			Path:         "item create",
			Summary:      "Create an item",
			Examples:     []string{"rgcheck item create demo"},
			OutputModes:  []string{"table", "json"},
			Mutates:      true,
			Destructive:  false,
			SupportsMeta: false,
		},
		{
			Path:        "item delete",
			Summary:     "Delete an item",
			Examples:    []string{"rgcheck item delete demo --dry-run"},
			Related:     []string{"rgcheck item list"},
			OutputModes: []string{"table", "json"},
			Destructive: true,
		},
		{
			Path:         "item list",
			Summary:      "List items",
			Examples:     []string{"rgcheck item list", "rgcheck item list --json"},
			Related:      []string{"rgcheck whoami"},
			OutputModes:  []string{"table", "json"},
			SupportsMeta: true,
		},
		{
			Path:         "whoami",
			Summary:      "Show identity",
			GroupID:      "core",
			Examples:     []string{"rgcheck whoami"},
			Related:      []string{"rgcheck item list"},
			OutputModes:  []string{"table", "json"},
			RequiresAuth: true,
			SupportsMeta: true,
		},
	}
}

func consistencyApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:           "rgcheck",
		Short:          "consistency test CLI",
		Version:        "0.0.0",
		AdvancedOutput: true,
	})
	app.AddModule(consistencyModule{})
	return app
}

type consistencyExtensionModule struct {
	specStatus string
}

func (consistencyExtensionModule) Groups() []rungrad.Group { return nil }
func (consistencyExtensionModule) Commands() []*rungrad.Command {
	return []*rungrad.Command{{
		Use:        "read",
		Short:      "Read data",
		Extensions: consistencyExtensionSet("beta"),
		Run:        noopRun,
	}}
}

func (m consistencyExtensionModule) Catalog() []rungrad.CommandSpec {
	status := m.specStatus
	if status == "" {
		status = "beta"
	}
	return []rungrad.CommandSpec{{
		Path:       "read",
		Summary:    "Read data",
		Extensions: consistencyExtensionSet(status),
	}}
}

func consistencyExtensionSet(status string) manifest.ExtensionSet {
	return manifest.ExtensionSet{"example.com/product": {
		"owner":     "platform",
		"status":    status,
		"docs_path": "docs/read.md",
	}}
}

func consistencyExtensionApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{Name: "rgextcheck", Short: "extension consistency CLI"})
	app.AddModule(consistencyExtensionModule{})
	return app
}

func consistencyExtensionDriftApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{Name: "rgextcheck", Short: "extension consistency CLI"})
	app.AddModule(consistencyExtensionModule{specStatus: "stable"})
	return app
}

func noopRun(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil }

func sortedMapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func sortedFileNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out
}

func findManifestPath(commands []manifest.Command, path ...string) *manifest.Command {
	for i := range commands {
		if reflect.DeepEqual(commands[i].Path, path) {
			return &commands[i]
		}
	}
	return nil
}
