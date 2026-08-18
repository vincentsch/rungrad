// Package cmdtree contains shared Cobra command-tree projection helpers used by
// docs generation and manifest emission.
package cmdtree

import (
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// AnnotationFrameworkCompletion marks Cobra's framework-synthesized completion
// command so VisibleCommands can filter it without filtering a host-owned
// command that happens to be named "completion".
const AnnotationFrameworkCompletion = "rungrad.frameworkCompletion"

// VisibleCommands returns the visible command tree rooted at root in a stable,
// depth-first order: root first, then each command's subcommands sorted by name.
// Hidden commands, Cobra help, and annotated framework completion commands (and
// their subtrees) are skipped.
func VisibleCommands(root *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden || c.Name() == "help" ||
			(c.Name() == "completion" && c.Annotations[AnnotationFrameworkCompletion] == "true") {
			return
		}
		out = append(out, c)
		subs := append([]*cobra.Command(nil), c.Commands()...)
		sort.Slice(subs, func(i, j int) bool { return subs[i].Name() < subs[j].Name() })
		for _, s := range subs {
			walk(s)
		}
	}
	walk(root)
	return out
}

// GlobalFlagNames returns the names treated as global to root: every flag on
// root.PersistentFlags() plus the synthetic help/version flags. Docs generation
// and manifest emission filter these out of a command's local flags by name.
func GlobalFlagNames(root *cobra.Command) map[string]bool {
	names := map[string]bool{"help": true}
	if root.Version != "" {
		names["version"] = true
	}
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) { names[f.Name] = true })
	return names
}
