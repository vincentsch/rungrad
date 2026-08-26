package agentmeta

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/spec"
)

const extensionNamespace = "example.com/product"

func validAgentManifest() manifest.Manifest {
	return manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		SpecVersion:   spec.Version,
		ToolName:      "demo",
		ToolVersion:   "v1.2.3",
		GlobalFlags: []manifest.Flag{
			boolFlag("dry-run"),
			boolFlag("json"),
			boolFlag("no-prompt"),
		},
		Commands: []manifest.Command{
			{
				Path:        []string{},
				Use:         "demo",
				Short:       "Demo CLI",
				Examples:    []string{"demo item list"},
				Related:     []string{},
				OutputModes: []string{},
				LocalFlags:  []manifest.Flag{},
			},
			{
				Path:        []string{"item"},
				Use:         "item",
				Short:       "Work with items",
				Examples:    []string{"demo item list"},
				Related:     []string{"demo whoami"},
				OutputModes: []string{},
				LocalFlags:  []manifest.Flag{},
			},
			{
				Path:         []string{"item", "list"},
				Use:          "list",
				Short:        "List items",
				Examples:     []string{"demo item list", "demo item list --json"},
				Related:      []string{"demo item get"},
				OutputModes:  []string{"human", "json"},
				SupportsMeta: true,
				LocalFlags:   []manifest.Flag{},
			},
			{
				Path:           []string{"item", "create"},
				Use:            "create <name>",
				Short:          "Create an item",
				Examples:       []string{"demo item create alpha"},
				Related:        []string{"demo item list"},
				OutputModes:    []string{"human", "json"},
				Mutates:        true,
				SupportsDryRun: true,
				LocalFlags: []manifest.Flag{
					{Name: "project", Usage: "Project ID", Default: "", Type: "string", Required: true},
					{Name: "tag", Usage: "Tag", Default: "", Type: "string"},
				},
			},
			{
				Path:                 []string{"item", "delete"},
				Use:                  "delete <name>",
				Short:                "Delete an item",
				Examples:             []string{"demo item delete alpha --dry-run"},
				Related:              []string{"demo item list"},
				OutputModes:          []string{"human", "json"},
				Mutates:              true,
				SupportsDryRun:       true,
				Destructive:          true,
				RequiresConfirmation: true,
				LocalFlags: []manifest.Flag{
					{Name: "confirm", Usage: "Confirm deletion", Default: "false", Type: "bool"},
				},
			},
			{
				Path:         []string{"whoami"},
				Use:          "whoami",
				Short:        "Show identity",
				Examples:     []string{"demo whoami"},
				Related:      []string{"demo item list"},
				OutputModes:  []string{"human", "json"},
				RequiresAuth: true,
				LocalFlags:   []manifest.Flag{},
			},
		},
	}
}

func boolFlag(name string) manifest.Flag {
	return manifest.Flag{Name: name, Default: "false", Type: "bool"}
}

func findCommand(doc Document, path ...string) *Command {
	if path == nil {
		path = []string{}
	}
	for i := range doc.Commands {
		if reflect.DeepEqual(doc.Commands[i].Path, path) {
			return &doc.Commands[i]
		}
	}
	return nil
}

func TestFromManifestProjectsCommandMetadataAndTemplates(t *testing.T) {
	doc, err := FromManifest(validAgentManifest())
	if err != nil {
		t.Fatalf("FromManifest() = %v", err)
	}
	if doc.SchemaVersion != schemaVersion {
		t.Fatalf("schema_version = %q", doc.SchemaVersion)
	}
	if doc.SpecVersion != spec.Version || doc.ToolName != "demo" || doc.ToolVersion != "v1.2.3" {
		t.Fatalf("document identity = %+v", doc)
	}
	wantPaths := [][]string{
		{},
		{"item"},
		{"item", "list"},
		{"item", "create"},
		{"item", "delete"},
		{"whoami"},
	}
	if len(doc.Commands) != len(wantPaths) {
		t.Fatalf("commands len = %d, want %d", len(doc.Commands), len(wantPaths))
	}
	for i, want := range wantPaths {
		if !reflect.DeepEqual(doc.Commands[i].Path, want) {
			t.Fatalf("command %d path = %v, want %v", i, doc.Commands[i].Path, want)
		}
	}

	root := findCommand(doc)
	if root == nil {
		t.Fatal("missing root command")
	}
	if !root.IsRoot || !root.HasChildren {
		t.Fatalf("root IsRoot/HasChildren = %v/%v", root.IsRoot, root.HasChildren)
	}
	if root.CommandTemplate != "demo" || root.UsageTemplate != "demo" {
		t.Fatalf("root templates = %q / %q", root.CommandTemplate, root.UsageTemplate)
	}

	item := findCommand(doc, "item")
	if item == nil || item.IsRoot || !item.HasChildren {
		t.Fatalf("item metadata = %+v", item)
	}
	if item.CommandTemplate != "demo item" || item.UsageTemplate != "demo item" {
		t.Fatalf("item templates = %q / %q", item.CommandTemplate, item.UsageTemplate)
	}

	list := findCommand(doc, "item", "list")
	if list == nil {
		t.Fatal("missing item list")
	}
	if list.HasChildren || !list.SupportsMeta {
		t.Fatalf("item list metadata = %+v", list)
	}
	if list.CommandTemplate != "demo item list" || list.UsageTemplate != "demo item list" {
		t.Fatalf("list templates = %q / %q", list.CommandTemplate, list.UsageTemplate)
	}
	if list.MachineUsageTemplate != "demo item list --json --no-prompt" {
		t.Fatalf("list machine template = %q", list.MachineUsageTemplate)
	}

	create := findCommand(doc, "item", "create")
	if create == nil {
		t.Fatal("missing item create")
	}
	if !create.Mutates || !create.SupportsDryRun || create.Destructive {
		t.Fatalf("create mutation metadata = %+v", create)
	}
	if create.MachineUsageTemplate != "" {
		t.Fatalf("create machine template = %q, want empty", create.MachineUsageTemplate)
	}
	if create.DryRunUsageTemplate != "demo item create <name> --dry-run --json --no-prompt" {
		t.Fatalf("create dry-run template = %q", create.DryRunUsageTemplate)
	}
	wantRequired := []Flag{{Name: "project", Usage: "Project ID", Default: "", Type: "string", Required: true}}
	if !reflect.DeepEqual(create.RequiredLocalFlags, wantRequired) {
		t.Fatalf("required local flags = %+v, want %+v", create.RequiredLocalFlags, wantRequired)
	}
	if strings.Contains(create.UsageTemplate, "--project") || strings.Contains(create.DryRunUsageTemplate, "--project") {
		t.Fatalf("required local flag was appended to templates: %+v", create)
	}

	deleteCmd := findCommand(doc, "item", "delete")
	if deleteCmd == nil {
		t.Fatal("missing item delete")
	}
	if !deleteCmd.Mutates || !deleteCmd.Destructive || !deleteCmd.RequiresConfirmation {
		t.Fatalf("delete destructive metadata = %+v", deleteCmd)
	}
	if deleteCmd.MachineUsageTemplate != "" {
		t.Fatalf("delete machine template = %q, want empty", deleteCmd.MachineUsageTemplate)
	}
	if deleteCmd.DryRunUsageTemplate != "demo item delete <name> --dry-run --json --no-prompt" {
		t.Fatalf("delete dry-run template = %q", deleteCmd.DryRunUsageTemplate)
	}
	if !strings.Contains(deleteCmd.ConfirmationNote, "explicit user confirmation") ||
		!strings.Contains(deleteCmd.ConfirmationNote, "product documentation") {
		t.Fatalf("confirmation note = %q", deleteCmd.ConfirmationNote)
	}
	if strings.Contains(deleteCmd.DryRunUsageTemplate, "--confirm") {
		t.Fatalf("destructive template includes confirmation flag: %q", deleteCmd.DryRunUsageTemplate)
	}

	whoami := findCommand(doc, "whoami")
	if whoami == nil || !whoami.RequiresAuth {
		t.Fatalf("whoami auth metadata = %+v", whoami)
	}
	if whoami.MachineUsageTemplate != "demo whoami --json --no-prompt" {
		t.Fatalf("whoami machine template = %q", whoami.MachineUsageTemplate)
	}
}

func TestFromManifestPreservesOrderWithoutSynthesizingCommands(t *testing.T) {
	m := validAgentManifest()
	m.Commands = []manifest.Command{
		m.Commands[0],
		{
			Path:        []string{"zeta"},
			Use:         "zeta",
			Examples:    []string{},
			Related:     []string{},
			OutputModes: []string{},
			LocalFlags:  []manifest.Flag{},
		},
		{
			Path:        []string{"alpha", "list"},
			Use:         "list",
			Examples:    []string{},
			Related:     []string{},
			OutputModes: []string{"json"},
			LocalFlags:  []manifest.Flag{},
		},
	}
	doc, err := FromManifest(m)
	if err != nil {
		t.Fatalf("FromManifest() = %v", err)
	}
	got := make([][]string, 0, len(doc.Commands))
	for _, cmd := range doc.Commands {
		got = append(got, cmd.Path)
	}
	want := [][]string{{}, {"zeta"}, {"alpha", "list"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command order = %v, want %v", got, want)
	}
	if findCommand(doc, "alpha") != nil {
		t.Fatalf("synthesized missing parent command: %+v", doc.Commands)
	}
	if !doc.Commands[0].HasChildren || doc.Commands[1].HasChildren || doc.Commands[2].HasChildren {
		t.Fatalf("HasChildren values = %+v", doc.Commands)
	}
}

func TestFromManifestCopiesInputAndOutputData(t *testing.T) {
	owner := "platform"
	tags := []any{"stable", map[string]any{"stage": "beta"}}
	m := validAgentManifest()
	m.Commands[2].Extensions = manifest.ExtensionSet{extensionNamespace: {
		"owner": &owner,
		"tags":  tags,
		"nested": map[string]any{
			"levels": []any{"one"},
		},
	}}

	doc, err := FromManifest(m)
	if err != nil {
		t.Fatalf("FromManifest() = %v", err)
	}

	m.GlobalFlags[0].Name = "changed-global"
	m.Commands[2].Path[1] = "changed-list"
	m.Commands[2].Examples[0] = "changed-example"
	m.Commands[2].LocalFlags = append(m.Commands[2].LocalFlags, manifest.Flag{Name: "late"})
	owner = "changed-owner"
	tags[0] = "changed-tag"
	tags[1].(map[string]any)["stage"] = "changed-stage"
	m.Commands[2].Extensions[extensionNamespace]["added"] = "from-input"

	list := findCommand(doc, "item", "list")
	if list == nil {
		t.Fatal("missing copied item list")
	}
	if doc.GlobalFlags[0].Name != "dry-run" {
		t.Fatalf("global flag changed through input mutation: %+v", doc.GlobalFlags[0])
	}
	if list.Examples[0] != "demo item list" {
		t.Fatalf("examples changed through input mutation: %v", list.Examples)
	}
	copiedOwner := list.Extensions[extensionNamespace]["owner"].(*string)
	if *copiedOwner != "platform" {
		t.Fatalf("extension pointer changed through input mutation: %q", *copiedOwner)
	}
	copiedTags := list.Extensions[extensionNamespace]["tags"].([]any)
	if copiedTags[0] != "stable" || copiedTags[1].(map[string]any)["stage"] != "beta" {
		t.Fatalf("extension tags changed through input mutation: %#v", copiedTags)
	}
	if _, ok := list.Extensions[extensionNamespace]["added"]; ok {
		t.Fatalf("extension map shares input: %#v", list.Extensions[extensionNamespace])
	}

	list.Path[0] = "doc-mutated"
	list.Examples[0] = "doc example"
	*copiedOwner = "doc-owner"
	copiedTags[1].(map[string]any)["stage"] = "doc-stage"
	list.Extensions[extensionNamespace]["doc-added"] = "from-doc"

	if m.Commands[2].Path[0] != "item" || m.Commands[2].Examples[0] != "changed-example" {
		t.Fatalf("input command changed through document mutation: %+v", m.Commands[2])
	}
	inputOwner := m.Commands[2].Extensions[extensionNamespace]["owner"].(*string)
	if *inputOwner != "changed-owner" {
		t.Fatalf("input owner changed through document mutation: %q", *inputOwner)
	}
	if tags[1].(map[string]any)["stage"] != "changed-stage" {
		t.Fatalf("input nested map changed through document mutation: %#v", tags)
	}
	if _, ok := m.Commands[2].Extensions[extensionNamespace]["doc-added"]; ok {
		t.Fatalf("document extension map shares input: %#v", m.Commands[2].Extensions[extensionNamespace])
	}
}

func TestFromManifestVersionAndValidationErrors(t *testing.T) {
	t.Run("unsupported schema propagates typed error", func(t *testing.T) {
		m := validAgentManifest()
		m.SchemaVersion = "rungrad-manifest/2"
		m.Commands = nil
		_, err := FromManifest(m)
		var unsupported *manifest.UnsupportedVersionError
		if !errors.As(err, &unsupported) {
			t.Fatalf("FromManifest() = %v, want UnsupportedVersionError", err)
		}
		if unsupported.Version != "rungrad-manifest/2" {
			t.Fatalf("unsupported version = %q", unsupported.Version)
		}
	})

	t.Run("unsupported spec rejected after validation", func(t *testing.T) {
		m := validAgentManifest()
		m.SpecVersion = "rungrad-spec/2"
		_, err := FromManifest(m)
		if err == nil || !strings.Contains(err.Error(), `unsupported manifest spec version "rungrad-spec/2"`) {
			t.Fatalf("FromManifest() = %v, want unsupported spec version", err)
		}
	})

	t.Run("zero value manifest returns validation error", func(t *testing.T) {
		if _, err := FromManifest(manifest.Manifest{}); err == nil {
			t.Fatal("FromManifest(zero) = nil error")
		}
	})

	t.Run("root must be first", func(t *testing.T) {
		m := validAgentManifest()
		m.Commands[0], m.Commands[1] = m.Commands[1], m.Commands[0]
		_, err := FromManifest(m)
		if err == nil || !strings.Contains(err.Error(), "index 0 is not root") {
			t.Fatalf("FromManifest(root not first) = %v", err)
		}
	})

	t.Run("nil path rejected by manifest validation", func(t *testing.T) {
		m := validAgentManifest()
		m.Commands[1].Path = nil
		_, err := FromManifest(m)
		if err == nil || !strings.Contains(err.Error(), "no path array") {
			t.Fatalf("FromManifest(nil path) = %v", err)
		}
	})
}

func TestFromManifestRejectsMalformedUse(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*manifest.Manifest)
		want   string
	}{
		{
			name: "empty root use",
			mutate: func(m *manifest.Manifest) {
				m.Commands[0].Use = ""
			},
			want: "empty use",
		},
		{
			name: "root use mismatch",
			mutate: func(m *manifest.Manifest) {
				m.Commands[0].Use = "other"
			},
			want: `does not match "demo"`,
		},
		{
			name: "empty child use",
			mutate: func(m *manifest.Manifest) {
				m.Commands[2].Use = ""
			},
			want: "empty use",
		},
		{
			name: "child use mismatch",
			mutate: func(m *manifest.Manifest) {
				m.Commands[2].Use = "show"
			},
			want: `does not match "list"`,
		},
		{
			name: "root usage operands are appended",
			mutate: func(m *manifest.Manifest) {
				m.Commands[0].Use = "demo [command]"
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validAgentManifest()
			tt.mutate(&m)
			doc, err := FromManifest(m)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("FromManifest() = %v", err)
				}
				if doc.Commands[0].UsageTemplate != "demo [command]" {
					t.Fatalf("root usage template = %q", doc.Commands[0].UsageTemplate)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("FromManifest() = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestFromManifestCanonicalGlobalTemplateRules(t *testing.T) {
	base := func(flags []manifest.Flag) manifest.Manifest {
		return manifest.Manifest{
			SchemaVersion: manifest.SchemaVersion,
			SpecVersion:   spec.Version,
			ToolName:      "demo",
			GlobalFlags:   flags,
			Commands: []manifest.Command{
				{Path: []string{}, Use: "demo", Examples: []string{}, Related: []string{}, OutputModes: []string{}, LocalFlags: []manifest.Flag{}},
				{Path: []string{"read"}, Use: "read", Examples: []string{}, Related: []string{}, OutputModes: []string{"json"}, LocalFlags: []manifest.Flag{}},
				{Path: []string{"write"}, Use: "write <name>", Examples: []string{}, Related: []string{}, OutputModes: []string{"json"}, Mutates: true, SupportsDryRun: true, LocalFlags: []manifest.Flag{}},
				{Path: []string{"destroy"}, Use: "destroy <name>", Examples: []string{}, Related: []string{}, OutputModes: []string{"json"}, Mutates: true, SupportsDryRun: true, Destructive: true, RequiresConfirmation: true, LocalFlags: []manifest.Flag{}},
				{Path: []string{"human"}, Use: "human", Examples: []string{}, Related: []string{}, OutputModes: []string{"human"}, LocalFlags: []manifest.Flag{}},
			},
		}
	}

	tests := []struct {
		name       string
		flags      []manifest.Flag
		read       string
		writeDry   string
		destroyDry string
	}{
		{
			name:       "canonical flags visible",
			flags:      []manifest.Flag{boolFlag("json"), boolFlag("no-prompt"), boolFlag("dry-run")},
			read:       "demo read --json --no-prompt",
			writeDry:   "demo write <name> --dry-run --json --no-prompt",
			destroyDry: "demo destroy <name> --dry-run --json --no-prompt",
		},
		{
			name:       "renamed json is absent",
			flags:      []manifest.Flag{boolFlag("machine-json"), boolFlag("no-prompt"), boolFlag("dry-run")},
			writeDry:   "demo write <name> --dry-run",
			destroyDry: "demo destroy <name> --dry-run",
		},
		{
			name:       "renamed no prompt is absent",
			flags:      []manifest.Flag{boolFlag("json"), boolFlag("never-prompt"), boolFlag("dry-run")},
			writeDry:   "demo write <name> --dry-run",
			destroyDry: "demo destroy <name> --dry-run",
		},
		{
			name: "wrong type and default are absent",
			flags: []manifest.Flag{
				{Name: "json", Type: "string", Default: ""},
				{Name: "no-prompt", Type: "bool", Default: "true"},
				boolFlag("dry-run"),
			},
			writeDry:   "demo write <name> --dry-run",
			destroyDry: "demo destroy <name> --dry-run",
		},
		{
			name:  "hidden or disabled globals are represented by absent entries",
			flags: []manifest.Flag{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := FromManifest(base(tt.flags))
			if err != nil {
				t.Fatalf("FromManifest() = %v", err)
			}
			read := findCommand(doc, "read")
			write := findCommand(doc, "write")
			destroy := findCommand(doc, "destroy")
			human := findCommand(doc, "human")
			if read.MachineUsageTemplate != tt.read {
				t.Fatalf("read machine template = %q, want %q", read.MachineUsageTemplate, tt.read)
			}
			if write.MachineUsageTemplate != "" {
				t.Fatalf("mutating machine template = %q, want empty", write.MachineUsageTemplate)
			}
			if destroy.MachineUsageTemplate != "" {
				t.Fatalf("destructive machine template = %q, want empty", destroy.MachineUsageTemplate)
			}
			if write.DryRunUsageTemplate != tt.writeDry {
				t.Fatalf("write dry-run template = %q, want %q", write.DryRunUsageTemplate, tt.writeDry)
			}
			if destroy.DryRunUsageTemplate != tt.destroyDry {
				t.Fatalf("destroy dry-run template = %q, want %q", destroy.DryRunUsageTemplate, tt.destroyDry)
			}
			if human.MachineUsageTemplate != "" || human.DryRunUsageTemplate != "" {
				t.Fatalf("human-only templates = %q / %q", human.MachineUsageTemplate, human.DryRunUsageTemplate)
			}
		})
	}
}

func TestFromManifestNilFreeSlices(t *testing.T) {
	doc, err := FromManifest(validAgentManifest())
	if err != nil {
		t.Fatalf("FromManifest() = %v", err)
	}
	if doc.GlobalFlags == nil || doc.Commands == nil {
		t.Fatalf("top-level slices are nil: %+v", doc)
	}
	for _, cmd := range doc.Commands {
		if cmd.Path == nil || cmd.Examples == nil || cmd.Related == nil ||
			cmd.OutputModes == nil || cmd.LocalFlags == nil || cmd.RequiredLocalFlags == nil {
			t.Fatalf("command has nil slice: %+v", cmd)
		}
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal(document) = %v", err)
	}
	if strings.Contains(string(b), "null") {
		t.Fatalf("document JSON contains null: %s", b)
	}
}
