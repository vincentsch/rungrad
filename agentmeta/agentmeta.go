package agentmeta

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/spec"
)

const schemaVersion = "rungrad-agentmeta/1"

// Document is an agent metadata document derived from a validated rungrad
// manifest.
type Document struct {
	// SchemaVersion identifies the agent metadata document JSON shape.
	SchemaVersion string `json:"schema_version"`
	// SpecVersion is the rungrad spec version copied from the source manifest.
	SpecVersion string `json:"spec_version"`
	// ToolName is the executable name copied from the source manifest.
	ToolName string `json:"tool_name"`
	// ToolVersion is the product version copied from the source manifest.
	ToolVersion string `json:"tool_version"`
	// GlobalFlags are visible framework or host-owned surface global flags.
	GlobalFlags []Flag `json:"global_flags"`
	// Commands are visible commands in manifest order with derived agent
	// metadata.
	Commands []Command `json:"commands"`
}

// Command describes one manifest command for repository-scoped skill text and
// manual adapter curation.
type Command struct {
	// Path is the executable-relative command path copied from the manifest.
	Path []string `json:"path"`
	// Use is the Cobra use line copied from the manifest.
	Use string `json:"use"`
	// Short is the one-line command description copied from the manifest.
	Short string `json:"short"`
	// Examples are command examples copied from the manifest.
	Examples []string `json:"examples"`
	// Related are related command paths copied from the manifest.
	Related []string `json:"related"`
	// OutputModes are declared output-mode tokens copied from the manifest.
	OutputModes []string `json:"output_modes"`
	// LocalFlags are visible local flags copied from the manifest.
	LocalFlags []Flag `json:"local_flags"`
	// RequiredLocalFlags are required local flags kept as structured data.
	RequiredLocalFlags []Flag `json:"required_local_flags"`
	// RequiresAuth reports the manifest's authentication metadata.
	RequiresAuth bool `json:"requires_auth"`
	// Mutates reports the manifest's mutation metadata.
	Mutates bool `json:"mutates"`
	// SupportsDryRun reports whether the manifest says --dry-run is supported.
	SupportsDryRun bool `json:"supports_dry_run"`
	// Destructive reports the manifest's destructive metadata.
	Destructive bool `json:"destructive"`
	// RequiresConfirmation reports the manifest's destructive-confirmation
	// metadata.
	RequiresConfirmation bool `json:"requires_confirmation"`
	// SupportsMeta reports whether the command supports request metadata output.
	SupportsMeta bool `json:"supports_meta"`
	// Extensions preserves product-owned manifest extension namespaces opaquely.
	Extensions manifest.ExtensionSet `json:"extensions,omitempty"`
	// IsRoot is computed from len(Path) == 0 and does not imply handler presence.
	IsRoot bool `json:"is_root"`
	// HasChildren is computed mechanically from strict path prefixes.
	HasChildren bool `json:"has_children"`
	// CommandTemplate is the tool name plus command path with no positional
	// operands.
	CommandTemplate string `json:"command_template"`
	// UsageTemplate is CommandTemplate plus operand tokens parsed from Use.
	UsageTemplate string `json:"usage_template"`
	// MachineUsageTemplate is a machine usage template with canonical visible
	// machine flags when the manifest supports them.
	MachineUsageTemplate string `json:"machine_usage_template"`
	// DryRunUsageTemplate is a non-executing dry-run usage template when the
	// manifest and host-owned surface expose canonical --dry-run.
	DryRunUsageTemplate string `json:"dry_run_usage_template"`
	// ConfirmationNote tells consumers how to handle commands that require
	// destructive confirmation.
	ConfirmationNote string `json:"confirmation_note"`
}

// Flag describes one visible flag copied from the manifest.
type Flag struct {
	// Name is the long flag name without --.
	Name string `json:"name"`
	// Shorthand is the short flag name without -, or empty when absent.
	Shorthand string `json:"shorthand"`
	// Usage is the flag help text copied from the manifest.
	Usage string `json:"usage"`
	// Default is the manifest default value.
	Default string `json:"default"`
	// Type is the pflag value type string.
	Type string `json:"type"`
	// Required reports whether the manifest marks the flag required.
	Required bool `json:"required"`
}

// FromManifest derives an agent metadata document from a rungrad-manifest/1
// value without executing adopter commands or generating MCP tool definitions.
func FromManifest(m manifest.Manifest) (Document, error) {
	if err := manifest.Validate(&m); err != nil {
		return Document{}, err
	}
	if m.SpecVersion != spec.Version {
		return Document{}, fmt.Errorf("unsupported manifest spec version %q", m.SpecVersion)
	}
	if len(m.Commands[0].Path) != 0 {
		return Document{}, fmt.Errorf("manifest command at index 0 is not root: %s", pathLabel(m.Commands[0].Path))
	}

	canonical := canonicalGlobals(m.GlobalFlags)
	doc := Document{
		SchemaVersion: schemaVersion,
		SpecVersion:   m.SpecVersion,
		ToolName:      m.ToolName,
		ToolVersion:   m.ToolVersion,
		GlobalFlags:   cloneFlags(m.GlobalFlags),
		Commands:      make([]Command, len(m.Commands)),
	}
	for i, c := range m.Commands {
		commandTemplate, usageTemplate, err := templates(m.ToolName, c)
		if err != nil {
			return Document{}, err
		}
		cmd := Command{
			Path:                 cloneStrings(c.Path),
			Use:                  c.Use,
			Short:                c.Short,
			Examples:             cloneStrings(c.Examples),
			Related:              cloneStrings(c.Related),
			OutputModes:          cloneStrings(c.OutputModes),
			LocalFlags:           cloneFlags(c.LocalFlags),
			RequiredLocalFlags:   requiredFlags(c.LocalFlags),
			RequiresAuth:         c.RequiresAuth,
			Mutates:              c.Mutates,
			SupportsDryRun:       c.SupportsDryRun,
			Destructive:          c.Destructive,
			RequiresConfirmation: c.RequiresConfirmation,
			SupportsMeta:         c.SupportsMeta,
			Extensions:           cloneExtensionSet(c.Extensions),
			IsRoot:               len(c.Path) == 0,
			HasChildren:          hasChildren(m.Commands, c.Path),
			CommandTemplate:      commandTemplate,
			UsageTemplate:        usageTemplate,
		}
		if commandMachineCapable(c, canonical) {
			cmd.MachineUsageTemplate = usageTemplate + " --json --no-prompt"
		}
		if c.SupportsDryRun && canonical["dry-run"] {
			cmd.DryRunUsageTemplate = usageTemplate + " --dry-run"
			if commandDeclaresOutput(c, "json") && canonical["json"] && canonical["no-prompt"] {
				cmd.DryRunUsageTemplate += " --json --no-prompt"
			}
		}
		if c.RequiresConfirmation {
			cmd.ConfirmationNote = "Require explicit user confirmation before executing this command and inspect product documentation for the confirmation mechanism."
		}
		doc.Commands[i] = cmd
	}
	return doc, nil
}

func templates(toolName string, c manifest.Command) (string, string, error) {
	fields := strings.Fields(c.Use)
	if len(fields) == 0 {
		return "", "", fmt.Errorf("manifest command %s has empty use", pathLabel(c.Path))
	}
	parts := append([]string{toolName}, c.Path...)
	expected := toolName
	if len(c.Path) > 0 {
		expected = c.Path[len(c.Path)-1]
	}
	if fields[0] != expected {
		return "", "", fmt.Errorf("manifest command %s use first field %q does not match %q", pathLabel(c.Path), fields[0], expected)
	}
	commandTemplate := strings.Join(parts, " ")
	usageParts := append(cloneStrings(parts), fields[1:]...)
	return commandTemplate, strings.Join(usageParts, " "), nil
}

func canonicalGlobals(flags []manifest.Flag) map[string]bool {
	out := map[string]bool{}
	for _, f := range flags {
		if f.Type != "bool" || f.Default != "false" {
			continue
		}
		switch f.Name {
		case "json", "no-prompt", "dry-run":
			out[f.Name] = true
		}
	}
	return out
}

func commandMachineCapable(c manifest.Command, canonical map[string]bool) bool {
	return commandDeclaresOutput(c, "json") &&
		canonical["json"] &&
		canonical["no-prompt"] &&
		!c.Mutates &&
		!c.Destructive
}

func commandDeclaresOutput(c manifest.Command, mode string) bool {
	for _, got := range c.OutputModes {
		if got == mode {
			return true
		}
	}
	return false
}

func hasChildren(commands []manifest.Command, path []string) bool {
	for _, other := range commands {
		if len(other.Path) <= len(path) {
			continue
		}
		if pathPrefix(path, other.Path) {
			return true
		}
	}
	return false
}

func pathPrefix(prefix, path []string) bool {
	for i := range prefix {
		if prefix[i] != path[i] {
			return false
		}
	}
	return true
}

func requiredFlags(flags []manifest.Flag) []Flag {
	out := make([]Flag, 0)
	for _, f := range flags {
		if f.Required {
			out = append(out, cloneFlag(f))
		}
	}
	return out
}

func cloneStrings(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneFlags(in []manifest.Flag) []Flag {
	out := make([]Flag, len(in))
	for i, f := range in {
		out[i] = cloneFlag(f)
	}
	return out
}

func cloneFlag(f manifest.Flag) Flag {
	return Flag{
		Name:      f.Name,
		Shorthand: f.Shorthand,
		Usage:     f.Usage,
		Default:   f.Default,
		Type:      f.Type,
		Required:  f.Required,
	}
}

func cloneExtensionSet(in manifest.ExtensionSet) manifest.ExtensionSet {
	if in == nil {
		return nil
	}
	out := make(manifest.ExtensionSet, len(in))
	for namespace, object := range in {
		out[namespace] = cloneExtensionObject(object)
	}
	return out
}

func cloneExtensionObject(in manifest.ExtensionObject) manifest.ExtensionObject {
	out := make(manifest.ExtensionObject, len(in))
	for key, value := range in {
		out[key] = cloneExtensionValue(value)
	}
	return out
}

func cloneExtensionValue(value any) any {
	if value == nil {
		return nil
	}
	out := cloneReflect(reflect.ValueOf(value))
	if !out.IsValid() {
		return nil
	}
	return out.Interface()
}

func cloneReflect(v reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		elem := cloneReflect(v.Elem())
		out := reflect.New(v.Type()).Elem()
		if elem.IsValid() && elem.Type().AssignableTo(v.Type()) {
			out.Set(elem)
			return out
		}
		out.Set(v)
		return out
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		elem := cloneReflect(v.Elem())
		out := reflect.New(v.Type().Elem())
		if elem.IsValid() && elem.Type().AssignableTo(v.Type().Elem()) {
			out.Elem().Set(elem)
			return out
		}
		out.Elem().Set(v.Elem())
		return out
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			elem := cloneReflect(v.Index(i))
			if elem.IsValid() && elem.Type().AssignableTo(v.Type().Elem()) {
				out.Index(i).Set(elem)
			} else {
				out.Index(i).Set(v.Index(i))
			}
		}
		return out
	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			elem := cloneReflect(v.Index(i))
			if elem.IsValid() && elem.Type().AssignableTo(v.Type().Elem()) {
				out.Index(i).Set(elem)
			} else {
				out.Index(i).Set(v.Index(i))
			}
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			key := cloneReflect(iter.Key())
			if !key.IsValid() || !key.Type().AssignableTo(v.Type().Key()) {
				key = iter.Key()
			}
			value := cloneReflect(iter.Value())
			if !value.IsValid() || !value.Type().AssignableTo(v.Type().Elem()) {
				value = iter.Value()
			}
			out.SetMapIndex(key, value)
		}
		return out
	default:
		return v
	}
}

func pathLabel(path []string) string {
	if len(path) == 0 {
		return "[]"
	}
	return strings.Join(path, " ")
}
