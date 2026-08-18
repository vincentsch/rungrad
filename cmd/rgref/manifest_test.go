package main

import (
	"reflect"
	"testing"

	rungrad "github.com/vincentsch/rungrad"
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

func TestManifestDeterminismAndValidity(t *testing.T) {
	a := testutil.Run(newApp(), "__rungrad_manifest")
	b := testutil.Run(newApp(), "__rungrad_manifest")
	if a.Exit != rungrad.ExitSuccess || b.Exit != rungrad.ExitSuccess {
		t.Fatalf("manifest exits %d/%d stderr=%q/%q", a.Exit, b.Exit, a.Stderr, b.Stderr)
	}
	if a.Stdout != b.Stdout {
		t.Fatalf("manifest output not repeatable:\n%s\n---\n%s", a.Stdout, b.Stdout)
	}
	var m manifest.Manifest
	if err := a.JSON(&m); err != nil {
		t.Fatalf("manifest JSON: %v\n%s", err, a.Stdout)
	}
	if err := manifest.Validate(&m); err != nil {
		t.Fatalf("Validate(manifest) = %v\n%s", err, a.Stdout)
	}
	if m.SchemaVersion != manifest.SchemaVersion {
		t.Fatalf("schema_version = %q", m.SchemaVersion)
	}
	if m.SpecVersion != "rungrad-spec/1" {
		t.Fatalf("spec_version = %q", m.SpecVersion)
	}
	if m.ToolName != "rgref" {
		t.Fatalf("tool_name = %q", m.ToolName)
	}
}

func TestManifestRootEntry(t *testing.T) {
	res := testutil.Run(newApp(), "__rungrad_manifest")
	var m manifest.Manifest
	if err := res.JSON(&m); err != nil {
		t.Fatalf("manifest JSON: %v\n%s", err, res.Stdout)
	}
	root := findManifestCommand(&m)
	if root == nil {
		t.Fatal("missing root command")
	}
	if root.Path == nil || len(root.Path) != 0 {
		t.Fatalf("root path = %#v, want []", root.Path)
	}
	wantExamples := []string{
		"rgref item list",
		"rgref item list --json",
		"rgref item create gamma --dry-run",
	}
	if !reflect.DeepEqual(root.Examples, wantExamples) {
		t.Fatalf("root examples = %v, want %v", root.Examples, wantExamples)
	}
	if len(root.Related) != 0 {
		t.Fatalf("root related = %v, want []", root.Related)
	}
	if len(root.LocalFlags) != 0 {
		t.Fatalf("root local flags = %v, want []", root.LocalFlags)
	}
}

func TestManifestUpdateExamples(t *testing.T) {
	res := testutil.Run(newApp(), "__rungrad_manifest")
	var m manifest.Manifest
	if err := res.JSON(&m); err != nil {
		t.Fatalf("manifest JSON: %v\n%s", err, res.Stdout)
	}
	upd := findManifestCommand(&m, "update")
	if upd == nil {
		t.Fatal("missing update command")
	}
	want := []string{"rgref update --check", "rgref update --check --json", "rgref update"}
	if !reflect.DeepEqual(upd.Examples, want) {
		t.Fatalf("update examples = %v, want %v", upd.Examples, want)
	}
	if len(upd.Related) != 0 {
		t.Fatalf("update related = %v, want [] (no version subcommand)", upd.Related)
	}
}

func TestManifestReferenceCommands(t *testing.T) {
	res := testutil.Run(newApp(), "__rungrad_manifest")
	var m manifest.Manifest
	if err := res.JSON(&m); err != nil {
		t.Fatalf("manifest JSON: %v\n%s", err, res.Stdout)
	}

	list := findManifestCommand(&m, "item", "list")
	if list == nil {
		t.Fatal("missing item list")
	}
	if list.Mutates || list.Destructive {
		t.Fatalf("item list metadata = %+v", list)
	}
	if !list.SupportsMeta {
		t.Fatalf("item list supports_meta = false, want true: %+v", list)
	}
	globals := map[string]bool{}
	for _, f := range m.GlobalFlags {
		globals[f.Name] = true
	}
	for _, name := range []string{"api-url", "profile", "auth-file"} {
		if !globals[name] {
			t.Errorf("manifest global flags missing %q: %v", name, m.GlobalFlags)
		}
	}

	create := findManifestCommand(&m, "item", "create")
	if create == nil || !create.Mutates || !create.SupportsDryRun || create.Destructive {
		t.Fatalf("item create metadata = %+v", create)
	}

	deleteCmd := findManifestCommand(&m, "item", "delete")
	if deleteCmd == nil || !deleteCmd.Mutates || !deleteCmd.Destructive || !deleteCmd.RequiresConfirmation {
		t.Fatalf("item delete metadata = %+v", deleteCmd)
	}
	var confirm *manifest.Flag
	for i := range deleteCmd.LocalFlags {
		if deleteCmd.LocalFlags[i].Name == "confirm" {
			confirm = &deleteCmd.LocalFlags[i]
			break
		}
	}
	if confirm == nil {
		t.Fatalf("item delete missing --confirm flag: %v", deleteCmd.LocalFlags)
	}
	if confirm.Type != "bool" || confirm.Default != "false" || confirm.Required {
		t.Fatalf("--confirm flag = %+v", confirm)
	}

	whoami := findManifestCommand(&m, "whoami")
	if whoami == nil || !whoami.RequiresAuth {
		t.Fatalf("whoami metadata = %+v", whoami)
	}

	if update := findManifestCommand(&m, "update"); update == nil {
		t.Fatal("missing update command")
	}
}

func TestManifestReferenceOutputModes(t *testing.T) {
	res := testutil.Run(newApp(), "__rungrad_manifest")
	var m manifest.Manifest
	if err := res.JSON(&m); err != nil {
		t.Fatalf("manifest JSON: %v\n%s", err, res.Stdout)
	}
	cases := []struct {
		path []string
		want []string
		meta bool
	}{
		{[]string{"item", "list"}, []string{"human", "json", "plain", "jq", "template"}, true},
		{[]string{"item", "get"}, []string{"human", "json", "jq", "template"}, false},
		{[]string{"item", "create"}, []string{"human", "json"}, false},
		{[]string{"item", "delete"}, []string{"human", "json"}, false},
		{[]string{"whoami"}, []string{"human", "json"}, false},
		{[]string{"update"}, []string{"human", "json"}, false},
	}
	for _, c := range cases {
		cmd := findManifestCommand(&m, c.path...)
		if cmd == nil {
			t.Fatalf("missing command %v", c.path)
		}
		if !reflect.DeepEqual(cmd.OutputModes, c.want) {
			t.Errorf("%v output modes = %v, want %v", c.path, cmd.OutputModes, c.want)
		}
		if cmd.SupportsMeta != c.meta {
			t.Errorf("%v supports_meta = %v, want %v", c.path, cmd.SupportsMeta, c.meta)
		}
	}
}

func TestManifestGlobalFlagsIncludeAdvanced(t *testing.T) {
	res := testutil.Run(newApp(), "__rungrad_manifest")
	var m manifest.Manifest
	if err := res.JSON(&m); err != nil {
		t.Fatalf("manifest JSON: %v\n%s", err, res.Stdout)
	}
	got := make([]string, 0, len(m.GlobalFlags))
	for _, f := range m.GlobalFlags {
		got = append(got, f.Name)
	}
	want := []string{
		"api-url", "auth-file", "config", "dry-run", "include-meta", "jq", "json",
		"no-ansi", "no-color", "no-pager", "no-prompt", "plain", "profile", "quiet", "template",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest global flags = %v, want %v", got, want)
	}
}
