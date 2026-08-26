package scaffold

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vincentsch/rungrad/agentmeta"
	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/spec"
)

func productManifest(d productTemplateData) (manifest.Manifest, error) {
	m := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		SpecVersion:   spec.Version,
		ToolName:      d.Name,
		ToolVersion:   "v0.1.0",
		GlobalFlags:   productGlobalFlags(d.Services),
		Commands: []manifest.Command{
			{
				Path:        []string{},
				Use:         d.Name,
				Short:       d.ProductName,
				Examples:    cloneStringSlice(d.RootExamples),
				Related:     []string{},
				OutputModes: []string{},
				LocalFlags:  []manifest.Flag{},
			},
			{
				Path:           []string{"update"},
				Use:            "update",
				Short:          "Check for and install the latest version",
				Examples:       productUpdateExamples(d),
				Related:        []string{},
				OutputModes:    []string{"human", "json"},
				Mutates:        true,
				SupportsDryRun: true,
				LocalFlags: []manifest.Flag{
					{
						Name:     "check",
						Usage:    "Check for an update without installing it",
						Default:  "false",
						Type:     "bool",
						Required: false,
					},
				},
			},
			{
				Path:        []string{"widget"},
				Use:         "widget",
				Short:       "Work with widgets",
				Examples:    []string{},
				Related:     []string{},
				OutputModes: []string{},
				LocalFlags:  []manifest.Flag{},
			},
			{
				Path:           []string{"widget", "create"},
				Use:            "create <name>",
				Short:          "Create a widget",
				Examples:       cloneStringSlice(d.WidgetCreateExamples),
				Related:        []string{d.Name + " widget list"},
				OutputModes:    []string{"table", "json"},
				Mutates:        true,
				SupportsDryRun: true,
				LocalFlags:     []manifest.Flag{},
			},
			{
				Path:                 []string{"widget", "delete"},
				Use:                  "delete <name>",
				Short:                "Delete a widget",
				Examples:             cloneStringSlice(d.WidgetDeleteExamples),
				Related:              []string{d.Name + " widget list"},
				OutputModes:          []string{"table", "json"},
				Mutates:              true,
				SupportsDryRun:       true,
				Destructive:          true,
				RequiresConfirmation: true,
				LocalFlags: []manifest.Flag{
					{
						Name:     "confirm",
						Usage:    "Confirm the destructive action without a prompt",
						Default:  "false",
						Type:     "bool",
						Required: false,
					},
				},
			},
			{
				Path:        []string{"widget", "list"},
				Use:         "list",
				Short:       "List widgets",
				Examples:    cloneStringSlice(d.WidgetListExamples),
				Related:     []string{d.Name + " widget create"},
				OutputModes: []string{"table", "json"},
				LocalFlags:  []manifest.Flag{},
				Extensions: manifest.ExtensionSet{
					d.MetadataNamespace: {
						"owner":  "platform",
						"status": "stable",
					},
				},
			},
		},
	}
	if err := manifest.Validate(&m); err != nil {
		return manifest.Manifest{}, err
	}
	return m, nil
}

func productGlobalFlags(services []serviceData) []manifest.Flag {
	flags := []manifest.Flag{
		{Name: "json", Usage: "Output stable JSON instead of the human view", Default: "false", Type: "bool"},
		{Name: "dry-run", Usage: "Preview changes without performing them", Default: "false", Type: "bool"},
		{Name: "no-prompt", Usage: "Never block on an interactive prompt", Default: "false", Type: "bool"},
		{Name: "quiet", Usage: "Suppress non-essential output", Default: "false", Type: "bool"},
		{Name: "config", Usage: "Path to the config file", Default: "", Type: "string"},
		{Name: "profile", Usage: "Profile to use for config and credentials", Default: "", Type: "string"},
		{Name: "auth-file", Usage: "Path to the credentials file", Default: "", Type: "string"},
	}
	for _, svc := range services {
		flags = append(flags, manifest.Flag{
			Name:    svc.Flag,
			Usage:   svc.Usage,
			Default: svc.Default,
			Type:    "string",
		})
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })
	return flags
}

func productUpdateExamples(d productTemplateData) []string {
	examples := []string{
		d.Name + " update --check",
		d.Name + " update --check --json",
		d.Name + " update",
	}
	return append(examples, cloneStringSlice(d.UpdateExamples)...)
}

func renderProductSkillFiles(d productTemplateData) (map[string]string, error) {
	m, err := productManifest(d)
	if err != nil {
		return nil, err
	}
	doc, err := agentmeta.FromManifest(m)
	if err != nil {
		return nil, err
	}
	data, err := productSkillTemplateDataFromAgentMeta(doc)
	if err != nil {
		return nil, err
	}

	files, err := render(productSkillReadmeTemplateMap, data)
	if err != nil {
		return nil, err
	}
	skillPath := ".agents/skills/" + doc.ToolName + "/SKILL.md"
	skillFiles, err := render(map[string]string{"templates/agent_skill.md.tmpl": skillPath}, data)
	if err != nil {
		return nil, err
	}
	for path, content := range skillFiles {
		files[path] = normalizeAgentFile(content)
	}
	for path, content := range files {
		files[path] = normalizeAgentFile(content)
	}
	return files, nil
}

type productSkillTemplateData struct {
	Tool        string
	SkillPath   string
	CommandRows []productSkillCommandRow
}

type productSkillCommandRow struct {
	Command string
	Policy  string
}

func productSkillTemplateDataFromAgentMeta(doc agentmeta.Document) (productSkillTemplateData, error) {
	list, err := requireAgentCommand(doc, "widget", "list")
	if err != nil {
		return productSkillTemplateData{}, err
	}
	create, err := requireAgentCommand(doc, "widget", "create")
	if err != nil {
		return productSkillTemplateData{}, err
	}
	deleteCmd, err := requireAgentCommand(doc, "widget", "delete")
	if err != nil {
		return productSkillTemplateData{}, err
	}
	updateCmd, err := requireAgentCommand(doc, "update")
	if err != nil {
		return productSkillTemplateData{}, err
	}
	if list.MachineUsageTemplate == "" {
		return productSkillTemplateData{}, fmt.Errorf("scaffold: generated widget list command is missing machine usage metadata")
	}
	if create.DryRunUsageTemplate == "" {
		return productSkillTemplateData{}, fmt.Errorf("scaffold: generated widget create command is missing dry-run metadata")
	}
	if deleteCmd.DryRunUsageTemplate == "" || !deleteCmd.RequiresConfirmation || !hasAgentFlag(deleteCmd.LocalFlags, "confirm", "bool", "false") {
		return productSkillTemplateData{}, fmt.Errorf("scaffold: generated widget delete command is missing destructive confirmation metadata")
	}
	if !hasAgentFlag(updateCmd.LocalFlags, "check", "bool", "false") || !commandDeclaresAgentOutput(updateCmd, "json") {
		return productSkillTemplateData{}, fmt.Errorf("scaffold: generated update command is missing check/json metadata")
	}
	if updateCmd.DryRunUsageTemplate == "" {
		return productSkillTemplateData{}, fmt.Errorf("scaffold: generated update command is missing dry-run metadata")
	}

	updateCheck := updateCmd.CommandTemplate + " --check"
	updateCheckMachine := updateCheck + " --json --no-prompt"
	return productSkillTemplateData{
		Tool:      doc.ToolName,
		SkillPath: ".agents/skills/" + doc.ToolName + "/SKILL.md",
		CommandRows: []productSkillCommandRow{
			{
				Command: list.CommandTemplate,
				Policy:  "Read-only. Prefer `" + list.MachineUsageTemplate + "` for parseable output.",
			},
			{
				Command: updateCheck,
				Policy:  "Read-only scaffold update check. Prefer `" + updateCheckMachine + "`.",
			},
			{
				Command: create.UsageTemplate,
				Policy:  "Mutating. Run `" + create.DryRunUsageTemplate + "` first and review the preview before real execution.",
			},
			{
				Command: updateCmd.CommandTemplate,
				Policy:  "Mutating install path. Prefer `" + updateCheckMachine + "` unless the user asks to install; then run `" + updateCmd.DryRunUsageTemplate + "` first.",
			},
			{
				Command: deleteCmd.UsageTemplate,
				Policy:  "Destructive. Run `" + deleteCmd.DryRunUsageTemplate + "` first, require explicit user confirmation before real execution, and use `--confirm` only after that confirmation.",
			},
		},
	}, nil
}

func requireAgentCommand(doc agentmeta.Document, path ...string) (agentmeta.Command, error) {
	for _, cmd := range doc.Commands {
		if equalStrings(cmd.Path, path) {
			return cmd, nil
		}
	}
	label := strings.Join(path, " ")
	if label == "" {
		label = "root"
	}
	return agentmeta.Command{}, fmt.Errorf("scaffold: generated skill metadata is missing %s", label)
}

func hasAgentFlag(flags []agentmeta.Flag, name, typ, def string) bool {
	for _, flag := range flags {
		if flag.Name == name && flag.Type == typ && flag.Default == def {
			return true
		}
	}
	return false
}

func commandDeclaresAgentOutput(cmd agentmeta.Command, mode string) bool {
	for _, got := range cmd.OutputModes {
		if got == mode {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func cloneStringSlice(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func normalizeAgentFile(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimRight(s, "\n") + "\n"
	return s
}
