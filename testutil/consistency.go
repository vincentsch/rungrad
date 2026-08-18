package testutil

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/docsgen"
	"github.com/vincentsch/rungrad/manifest"
)

// CheckConsistency cross-checks already-captured artifacts and returns a sorted
// list of human-readable inconsistencies (empty means consistent). The manifest
// is the source of truth; docs and help must reflect what it declares. docs is
// keyed by generated doc path (for example, "tool_item_list.md"); help is keyed
// by the space-joined command path ("" for the root), matching CaptureAllHelp.
func CheckConsistency(m manifest.Manifest, docs, help map[string]string) []string {
	var issues []string
	for _, c := range m.Commands {
		helpKey := strings.Join(c.Path, " ")
		docKey := strings.Join(append([]string{m.ToolName}, c.Path...), "_") + ".md"
		label := consistencyPathLabel(m.ToolName, c.Path)

		helpText, hasHelp := help[helpKey]
		if !hasHelp {
			issues = append(issues, fmt.Sprintf("%s: no captured help", label))
		}
		docText, hasDoc := docs[docKey]
		if !hasDoc {
			issues = append(issues, fmt.Sprintf("%s: no generated docs page %s", label, docKey))
		}
		if !hasHelp || !hasDoc {
			continue
		}

		if examples := strings.Join(c.Examples, "\n"); examples != "" {
			// Usage lines often repeat the command path, so examples must appear
			// inside the rendered Examples section as the same block docsgen and
			// the manifest use.
			if !helpContainsExamples(helpText, examples) {
				issues = append(issues, fmt.Sprintf("%s: examples missing from help", label))
			}
			if !strings.Contains(docText, examples) {
				issues = append(issues, fmt.Sprintf("%s: examples missing from docs", label))
			}
		}
		if modes := strings.Join(c.OutputModes, ", "); modes != "" {
			if !strings.Contains(helpText, modes) {
				issues = append(issues, fmt.Sprintf("%s: output modes missing from help", label))
			}
			if !strings.Contains(docText, modes) {
				issues = append(issues, fmt.Sprintf("%s: output modes missing from docs", label))
			}
		}
		for _, related := range c.Related {
			if !strings.Contains(helpText, related) {
				issues = append(issues, fmt.Sprintf("%s: related command %q missing from help", label, related))
			}
			if !strings.Contains(docText, "- "+related) {
				issues = append(issues, fmt.Sprintf("%s: related command %q missing from docs", label, related))
			}
		}
		if c.RequiresAuth && !strings.Contains(docText, "## Authentication") {
			issues = append(issues, fmt.Sprintf("%s: authentication section missing from docs", label))
		}
		if c.Mutates && !strings.Contains(docText, "## Changes state") {
			issues = append(issues, fmt.Sprintf("%s: changes-state section missing from docs", label))
		}
		if c.Destructive && !strings.Contains(docText, "## Destructive") {
			issues = append(issues, fmt.Sprintf("%s: destructive section missing from docs", label))
		}
		if c.SupportsMeta && !strings.Contains(docText, "## Metadata") {
			issues = append(issues, fmt.Sprintf("%s: metadata section missing from docs", label))
		}
	}
	sort.Strings(issues)
	return issues
}

// AssertConsistent captures the manifest, generated docs, and all help from
// fresh apps, runs CheckConsistency, and when the app declares a command catalog
// also runs App.ValidateCatalog. It needs no committed files.
func AssertConsistent(t *testing.T, newApp func() *rungrad.App) {
	t.Helper()
	m := CaptureManifest(t, newApp())
	docs := docsgen.Generate(newApp())
	help := CaptureAllHelp(newApp())
	issues := CheckConsistency(m, docs, help)
	if app := newApp(); len(app.Catalog()) > 0 {
		if err := app.ValidateCatalog(); err != nil {
			issues = append(issues, "catalog: "+err.Error())
		}
	}
	if len(issues) > 0 {
		sort.Strings(issues)
		t.Fatalf("help/docs/manifest/catalog inconsistency:\n%s", strings.Join(issues, "\n"))
	}
}

func consistencyPathLabel(tool string, path []string) string {
	if len(path) == 0 {
		return tool
	}
	return strings.Join(path, " ")
}

// helpContainsExamples scopes the example check to Cobra's Examples section so
// a usage line or a longer example cannot mask a dropped declared example.
func helpContainsExamples(helpText, examples string) bool {
	section, ok := helpSection(helpText, "Examples:")
	return ok && strings.Contains(section, examples)
}

// helpSection returns the text after a help section header up to the next blank
// line. rungrad's help sections are separated by blank lines in Cobra output.
func helpSection(helpText, header string) (string, bool) {
	start := strings.Index(helpText, header)
	if start < 0 {
		return "", false
	}
	section := strings.TrimPrefix(helpText[start+len(header):], "\n")
	if end := strings.Index(section, "\n\n"); end >= 0 {
		section = section[:end]
	}
	return section, true
}
