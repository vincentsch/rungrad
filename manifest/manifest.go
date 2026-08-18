// Package manifest defines the rungrad machine manifest wire schema.
package manifest

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// SchemaVersion is the wire schema version emitted by rungrad-built tools.
const SchemaVersion = "rungrad-manifest/1"

// Manifest is the top-level self-description document for a CLI.
type Manifest struct {
	SchemaVersion string    `json:"schema_version"`
	SpecVersion   string    `json:"spec_version"`
	ToolName      string    `json:"tool_name"`
	ToolVersion   string    `json:"tool_version"`
	GlobalFlags   []Flag    `json:"global_flags"`
	Commands      []Command `json:"commands"`
}

// Command describes one visible non-synthetic command in the CLI tree.
type Command struct {
	Path                 []string `json:"path"`
	Use                  string   `json:"use"`
	Short                string   `json:"short"`
	Examples             []string `json:"examples"`
	Related              []string `json:"related"`
	OutputModes          []string `json:"output_modes"`
	RequiresAuth         bool     `json:"requires_auth"`
	Mutates              bool     `json:"mutates"`
	SupportsDryRun       bool     `json:"supports_dry_run"`
	Destructive          bool     `json:"destructive"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
	SupportsMeta         bool     `json:"supports_meta"`
	LocalFlags           []Flag   `json:"local_flags"`
	// Extensions carries product-owned namespaced metadata and is omitted when
	// a command has none.
	Extensions ExtensionSet `json:"extensions,omitempty"`
}

// Flag describes a visible Cobra/pflag flag as exposed by a command.
type Flag struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand"`
	Usage     string `json:"usage"`
	Default   string `json:"default"`
	Type      string `json:"type"`
	Required  bool   `json:"required"`
}

// UnsupportedVersionError reports a manifest whose schema_version is present
// but not supported by this package.
type UnsupportedVersionError struct{ Version string }

func (e *UnsupportedVersionError) Error() string {
	return fmt.Sprintf("unsupported manifest schema version %q", e.Version)
}

// Validate checks the typed manifest structure after JSON decoding. It
// distinguishes present-but-unsupported schema versions from invalid manifests.
func Validate(m *Manifest) error {
	if m == nil {
		return errors.New("manifest is nil")
	}
	// Version classification is intentionally first: a present but unsupported
	// schema is different from a malformed rungrad-manifest/1 document.
	if m.SchemaVersion == "" {
		return errors.New("manifest is missing schema_version")
	}
	if m.SchemaVersion != SchemaVersion {
		return &UnsupportedVersionError{Version: m.SchemaVersion}
	}
	if m.ToolName == "" {
		return errors.New("manifest is missing tool_name")
	}
	// The wire contract uses arrays for empty collections. A nil slice after JSON
	// decoding means the field was missing or null, not an empty array.
	if m.GlobalFlags == nil {
		return errors.New("manifest global_flags must be an array")
	}
	if m.Commands == nil {
		return errors.New("manifest commands must be an array")
	}
	if len(m.Commands) == 0 {
		return errors.New("manifest has no commands")
	}

	seen := map[string]bool{}
	hasRoot := false
	for i, c := range m.Commands {
		// An empty, non-nil path is the root command. A nil path is malformed.
		if c.Path == nil {
			return fmt.Errorf("manifest command %d has no path array", i)
		}
		if c.Examples == nil {
			return fmt.Errorf("manifest command %s examples must be an array", pathLabel(c.Path))
		}
		if c.Related == nil {
			return fmt.Errorf("manifest command %s related must be an array", pathLabel(c.Path))
		}
		if c.OutputModes == nil {
			return fmt.Errorf("manifest command %s output_modes must be an array", pathLabel(c.Path))
		}
		if c.LocalFlags == nil {
			return fmt.Errorf("manifest command %s local_flags must be an array", pathLabel(c.Path))
		}
		if len(c.Extensions) > 0 {
			if err := ValidateExtensionSet(c.Extensions); err != nil {
				return fmt.Errorf("manifest command %s: %w", pathLabel(c.Path), err)
			}
		}
		for _, seg := range c.Path {
			if seg == "" {
				return fmt.Errorf("manifest command %s has an empty path segment", pathLabel(c.Path))
			}
			if strings.IndexFunc(seg, unicode.IsSpace) != -1 {
				return fmt.Errorf("manifest command %s has a path segment with whitespace", pathLabel(c.Path))
			}
		}
		// Use the planned NUL-joined key format for path identity.
		key := strings.Join(c.Path, "\x00")
		if seen[key] {
			return fmt.Errorf("manifest has duplicate command path %s", pathLabel(c.Path))
		}
		seen[key] = true
		if len(c.Path) == 0 {
			hasRoot = true
		}
	}
	if !hasRoot {
		return errors.New("manifest has no root command (path: [])")
	}
	return nil
}

func pathLabel(path []string) string {
	if len(path) == 0 {
		return "[]"
	}
	return strings.Join(path, " ")
}
