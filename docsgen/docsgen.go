// Package docsgen emits manual reference documentation from a built command
// tree, so a tool's help and its docs cannot drift. Generate returns one markdown
// page per command plus an index; Check and Write compare or regenerate
// committed files so a tool's CI can fail when docs fall behind the command tree.
package docsgen

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/internal/cmdtree"
	"github.com/vincentsch/rungrad/internal/outputhelp"
)

// Generate walks the command tree and returns a map of relative doc path to
// markdown content: one page per command plus index.md.
func Generate(app *rungrad.App) map[string]string {
	root := app.Root()
	globals := cmdtree.GlobalFlagNames(root)
	docs := map[string]string{}
	for _, cmd := range cmdtree.VisibleCommands(root) {
		docs[docPath(cmd)] = renderCommand(cmd, globals)
	}
	docs["index.md"] = renderIndex(root)
	return docs
}

// CheckResult groups the ways committed generated docs can drift from the live
// generator output.
type CheckResult struct {
	Missing  []string // expected generated page is absent on disk
	Stale    []string // expected generated page exists but bytes differ
	Orphaned []string // committed file under dir is no longer generated
}

// OK reports whether committed generated docs are in sync.
func (r CheckResult) OK() bool {
	return len(r.Missing) == 0 && len(r.Stale) == 0 && len(r.Orphaned) == 0
}

// Check compares committed docs under dir with Generate(app), separating
// missing, stale, and orphaned paths so test failures can state the exact drift
// class. An OK result means docs are in sync.
func Check(app *rungrad.App, dir string) (CheckResult, error) {
	want := Generate(app)
	var result CheckResult
	for path, content := range want {
		got, err := os.ReadFile(filepath.Join(dir, path))
		switch {
		case errors.Is(err, os.ErrNotExist):
			result.Missing = append(result.Missing, path)
		case err != nil:
			return CheckResult{}, err
		case string(got) != content:
			result.Stale = append(result.Stale, path)
		}
	}
	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if _, ok := want[rel]; !ok {
			result.Orphaned = append(result.Orphaned, rel)
		}
		return nil
	}); err != nil && !errors.Is(err, os.ErrNotExist) {
		return CheckResult{}, err
	}
	sort.Strings(result.Missing)
	sort.Strings(result.Stale)
	sort.Strings(result.Orphaned)
	return result, nil
}

// Write regenerates the committed docs under dir from app: it writes every page
// returned by Generate (creating dir and any parents) and removes orphaned
// files -- any committed file under dir that Generate no longer owns -- so that
// a subsequent Check returns clean. dir is expected to contain only generated
// docs; pruning follows the same orphan rule as Check.
func Write(app *rungrad.App, dir string) error {
	want := Generate(app)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Write expected files first, then prune. This makes regeneration safe for
	// both missing directories and partially stale directories.
	paths := make([]string, 0, len(want))
	for path := range want {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(want[path]), 0o644); err != nil {
			return err
		}
	}

	// Prune only regular files. Empty directories left behind by old generated
	// pages are harmless and avoiding directory removal keeps the operation
	// narrow.
	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if _, ok := want[filepath.ToSlash(rel)]; !ok {
			return os.Remove(path)
		}
		return nil
	}); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func docPath(cmd *cobra.Command) string {
	return strings.ReplaceAll(cmd.CommandPath(), " ", "_") + ".md"
}

func renderCommand(cmd *cobra.Command, globals map[string]bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", cmd.CommandPath())
	if cmd.Short != "" {
		fmt.Fprintf(&b, "%s\n\n", cmd.Short)
	}
	fmt.Fprintf(&b, "## Usage\n\n```\n%s\n```\n\n", usageLine(cmd))

	// Read Cobra's Example field directly so docs match the manifest, including
	// root examples that are set outside rungrad.Command metadata.
	if ex := cmd.Example; ex != "" {
		fmt.Fprintf(&b, "## Examples\n\n```\n%s\n```\n\n", ex)
	}

	var locals []*pflag.Flag
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if !globals[f.Name] {
			locals = append(locals, f)
		}
	})
	if len(locals) > 0 {
		sort.Slice(locals, func(i, j int) bool { return locals[i].Name < locals[j].Name })
		b.WriteString("## Flags\n\n")
		for _, f := range locals {
			fmt.Fprintf(&b, "- `--%s` %s\n", f.Name, f.Usage)
		}
		b.WriteString("\n")
	}

	if out := cmd.Annotations[rungrad.AnnotationOutputs]; out != "" {
		fmt.Fprintf(&b, "## Output\n\n%s\n\n", strings.Join(outputhelp.Descriptions(strings.Split(out, ",")), "\n\n"))
	}
	if cmd.Annotations[rungrad.AnnotationAuth] == "required" {
		b.WriteString("## Authentication\n\nThis command requires a credential.\n\n")
	}
	if cmd.Annotations[rungrad.AnnotationMutates] == "true" {
		b.WriteString("## Preview changes\n\nThis command changes things. Add `--dry-run` to see what it would change; nothing changes then.\n\n")
	}
	if cmd.Annotations[rungrad.AnnotationDestructive] == "true" {
		b.WriteString("## Confirmation\n\nThis command deletes or overwrites something, so it asks before it acts. Preview it with `--dry-run` first. It doesn't ask when there is no terminal or when you pass `--json` or `--no-prompt`; then it needs its confirmation flag.\n\n")
	}
	if flag := metaFlag(cmd); flag != "" && cmd.Annotations[rungrad.AnnotationMeta] == "true" {
		// The section only appears when the tool has the flag, which it
		// registers with its advanced output modes.
		fmt.Fprintf(&b, "## Request details\n\nAdd `--%s` to the JSON output to get the result as `{data, meta}`, where `meta` holds details about the request.\n\n", flag)
	}
	if rel := cmd.Annotations[rungrad.AnnotationRelated]; rel != "" {
		b.WriteString("## Related commands\n\n")
		for _, r := range strings.Split(rel, ",") {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(r))
		}
		b.WriteString("\n")
	}
	// Section renderers naturally leave a blank line after the last section.
	// Commit generated pages with one final newline, not an extra blank line.
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// metaFlag returns the name of the flag that wraps output as {data, meta},
// or "" when the tool has none.
func metaFlag(cmd *cobra.Command) string {
	name := ""
	cmd.Root().PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if need := f.Annotations[rungrad.FlagCapabilityAnnotation]; len(need) > 0 && need[0] == "meta" && !f.Hidden {
			name = f.Name
		}
	})
	return name
}

func usageLine(cmd *cobra.Command) string {
	fields := strings.Fields(cmd.Use)
	if len(fields) <= 1 {
		return cmd.CommandPath()
	}
	return cmd.CommandPath() + " " + strings.Join(fields[1:], " ")
}

func renderIndex(root *cobra.Command) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s command reference\n\n", root.Name())
	if root.Short != "" {
		fmt.Fprintf(&b, "%s\n\n", root.Short)
	}

	b.WriteString("## Commands\n\n")
	for _, cmd := range cmdtree.VisibleCommands(root) {
		if cmd == root {
			continue
		}
		fmt.Fprintf(&b, "- [%s](%s) %s\n", cmd.CommandPath(), docPath(cmd), dash(cmd.Short))
	}
	b.WriteString("\n## Global flags\n\n")

	var globals []*pflag.Flag
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		globals = append(globals, f)
	})
	sort.Slice(globals, func(i, j int) bool { return globals[i].Name < globals[j].Name })
	for _, f := range globals {
		fmt.Fprintf(&b, "- `--%s` %s\n", f.Name, f.Usage)
	}
	return b.String()
}

func dash(s string) string {
	if s == "" {
		return ""
	}
	return "- " + s
}
