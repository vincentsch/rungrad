package main

import (
	"reflect"
	"testing"

	"github.com/vincentsch/rungrad/agentmeta"
)

func TestAgentMetaReferenceDocument(t *testing.T) {
	m, err := newApp().ManifestDocumentChecked()
	if err != nil {
		t.Fatalf("ManifestDocumentChecked() = %v", err)
	}
	got, err := agentmeta.FromManifest(m)
	if err != nil {
		t.Fatalf("agentmeta.FromManifest() = %v", err)
	}

	emptyFlags := []agentmeta.Flag{}
	confirmationNote := "Require explicit user confirmation before executing this command and inspect product documentation for the confirmation mechanism."
	want := agentmeta.Document{
		SchemaVersion: "rungrad-agentmeta/1",
		SpecVersion:   "rungrad-spec/1",
		ToolName:      "rgref",
		ToolVersion:   version,
		GlobalFlags: []agentmeta.Flag{
			{Name: "api-url", Usage: "Base URL for the reference API service", Default: "https://api.rgref.invalid", Type: "string"},
			{Name: "auth-file", Usage: "Path to the credentials file", Default: "", Type: "string"},
			{Name: "config", Usage: "Path to the config file", Default: "", Type: "string"},
			{Name: "dry-run", Usage: "Preview changes without performing them", Default: "false", Type: "bool"},
			{Name: "include-meta", Usage: "Wrap machine output as {data, meta} (commands that expose request metadata)", Default: "false", Type: "bool"},
			{Name: "jq", Usage: "Transform stable JSON output with a jq expression (commands with machine output)", Default: "", Type: "string"},
			{Name: "json", Usage: "Output stable JSON instead of the human view", Default: "false", Type: "bool"},
			{Name: "no-ansi", Usage: "Disable all ANSI/control sequences in human output", Default: "false", Type: "bool"},
			{Name: "no-color", Usage: "Disable color in human output", Default: "false", Type: "bool"},
			{Name: "no-pager", Usage: "Never use a pager for long human output", Default: "false", Type: "bool"},
			{Name: "no-prompt", Usage: "Never block on an interactive prompt", Default: "false", Type: "bool"},
			{Name: "plain", Usage: "Print unstyled, copy-safe text (commands with human output)", Default: "false", Type: "bool"},
			{Name: "profile", Usage: "Profile to use for config and credentials", Default: "", Type: "string"},
			{Name: "quiet", Usage: "Suppress non-essential output", Default: "false", Type: "bool"},
			{Name: "template", Usage: "Render stable JSON output through a Go text/template (commands with machine output)", Default: "", Type: "string"},
		},
		Commands: []agentmeta.Command{
			{
				Path:               []string{},
				Use:                "rgref",
				Short:              "rungrad reference CLI",
				Examples:           []string{"rgref item list", "rgref item list --json", "rgref item create gamma --dry-run"},
				Related:            []string{},
				OutputModes:        []string{},
				LocalFlags:         emptyFlags,
				RequiredLocalFlags: emptyFlags,
				IsRoot:             true,
				HasChildren:        true,
				CommandTemplate:    "rgref",
				UsageTemplate:      "rgref",
			},
			{
				Path:               []string{"item"},
				Use:                "item",
				Short:              "Work with items",
				Examples:           []string{"rgref item list", "rgref item get alpha", "rgref item create gamma --dry-run"},
				Related:            []string{"rgref whoami", "rgref update"},
				OutputModes:        []string{},
				LocalFlags:         emptyFlags,
				RequiredLocalFlags: emptyFlags,
				HasChildren:        true,
				CommandTemplate:    "rgref item",
				UsageTemplate:      "rgref item",
			},
			{
				Path:                []string{"item", "create"},
				Use:                 "create <name>",
				Short:               "Create an item",
				Examples:            []string{"rgref item create gamma", "rgref item create gamma --dry-run", "rgref item create gamma --quiet"},
				Related:             []string{"rgref item list"},
				OutputModes:         []string{"human", "json"},
				LocalFlags:          emptyFlags,
				RequiredLocalFlags:  emptyFlags,
				Mutates:             true,
				SupportsDryRun:      true,
				CommandTemplate:     "rgref item create",
				UsageTemplate:       "rgref item create <name>",
				DryRunUsageTemplate: "rgref item create <name> --dry-run --json --no-prompt",
			},
			{
				Path:                 []string{"item", "delete"},
				Use:                  "delete <name>",
				Short:                "Delete an item",
				Examples:             []string{"rgref item delete alpha --dry-run", "rgref item delete alpha --confirm"},
				Related:              []string{"rgref item list"},
				OutputModes:          []string{"human", "json"},
				LocalFlags:           []agentmeta.Flag{{Name: "confirm", Usage: "Confirm the destructive action without a prompt", Default: "false", Type: "bool"}},
				RequiredLocalFlags:   emptyFlags,
				Mutates:              true,
				SupportsDryRun:       true,
				Destructive:          true,
				RequiresConfirmation: true,
				CommandTemplate:      "rgref item delete",
				UsageTemplate:        "rgref item delete <name>",
				DryRunUsageTemplate:  "rgref item delete <name> --dry-run --json --no-prompt",
				ConfirmationNote:     confirmationNote,
			},
			{
				Path:                 []string{"item", "get"},
				Use:                  "get <name>",
				Short:                "Resolve an item by name or id",
				Examples:             []string{"rgref item get alpha", "rgref item get 1", "rgref item get alpha --jq .id", "rgref item get alpha --template '{{.id}}'"},
				Related:              []string{"rgref item list"},
				OutputModes:          []string{"human", "json", "jq", "template"},
				LocalFlags:           emptyFlags,
				RequiredLocalFlags:   emptyFlags,
				CommandTemplate:      "rgref item get",
				UsageTemplate:        "rgref item get <name>",
				MachineUsageTemplate: "rgref item get <name> --json --no-prompt",
			},
			{
				Path:                 []string{"item", "list"},
				Use:                  "list",
				Short:                "List items",
				Examples:             []string{"rgref item list", "rgref item list --json", "rgref item list --plain", "rgref item list --jq '.[].name'", `rgref item list --template '{{range .}}{{.id}} {{.name}}{{"\n"}}{{end}}'`},
				Related:              []string{"rgref item get", "rgref item create"},
				OutputModes:          []string{"human", "json", "plain", "jq", "template"},
				LocalFlags:           emptyFlags,
				RequiredLocalFlags:   emptyFlags,
				SupportsMeta:         true,
				CommandTemplate:      "rgref item list",
				UsageTemplate:        "rgref item list",
				MachineUsageTemplate: "rgref item list --json --no-prompt",
			},
			{
				Path:                []string{"update"},
				Use:                 "update",
				Short:               "Check for and install the latest version",
				Examples:            []string{"rgref update --check", "rgref update --check --json", "rgref update"},
				Related:             []string{},
				OutputModes:         []string{"human", "json"},
				LocalFlags:          []agentmeta.Flag{{Name: "check", Usage: "Check for an update without installing it", Default: "false", Type: "bool"}},
				RequiredLocalFlags:  emptyFlags,
				Mutates:             true,
				SupportsDryRun:      true,
				CommandTemplate:     "rgref update",
				UsageTemplate:       "rgref update",
				DryRunUsageTemplate: "rgref update --dry-run --json --no-prompt",
			},
			{
				Path:                 []string{"whoami"},
				Use:                  "whoami",
				Short:                "Show the authenticated identity",
				Examples:             []string{"rgref whoami"},
				Related:              []string{"rgref item list"},
				OutputModes:          []string{"human", "json"},
				LocalFlags:           emptyFlags,
				RequiredLocalFlags:   emptyFlags,
				RequiresAuth:         true,
				CommandTemplate:      "rgref whoami",
				UsageTemplate:        "rgref whoami",
				MachineUsageTemplate: "rgref whoami --json --no-prompt",
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agent metadata document mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}
