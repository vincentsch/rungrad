package rungrad

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vincentsch/rungrad/manifest"
)

// Annotation keys used to record rungrad command metadata on a cobra command so
// help, docs generation, and conformance can all read it from one place. They
// are exported so docsgen and other readers can inspect a built command tree.
const (
	AnnotationAuth        = "rungrad.auth"
	AnnotationMutates     = "rungrad.mutates"
	AnnotationDestructive = "rungrad.destructive"
	AnnotationExamples    = "rungrad.examples"
	AnnotationRelated     = "rungrad.related"
	AnnotationOutputs     = "rungrad.outputs"
	AnnotationMeta        = "rungrad.meta"
	AnnotationExtensions  = "rungrad.extensions"
)

// Canonical output-mode tokens recognized by the framework. Advanced-output
// apps use these tokens in Command.OutputModes to enable per-command --plain,
// --jq, and --template support.
const (
	OutputModeHuman    = "human"
	OutputModeJSON     = "json"
	OutputModePlain    = "plain"
	OutputModeJQ       = "jq"
	OutputModeTemplate = "template"
)

// Command is a thin builder over a cobra command that records the metadata
// rungrad needs: examples, related commands, output modes, whether the command
// mutates or destroys state, and whether it requires authentication. The
// framework uses that metadata for self-describing help, docs generation, and
// conformance scoring.
type Command struct {
	Use   string
	Short string
	Long  string

	// Examples are concrete invocations shown under "Examples:" in --help.
	Examples []string
	// Related lists related command paths shown under "Related commands:".
	Related []string
	// OutputModes documents the supported output forms, e.g. "human", "json".
	// Advanced-output apps recognize OutputModePlain, OutputModeJQ, and
	// OutputModeTemplate here for runtime capability checks. These tokens flow to
	// --help, generated docs, and the manifest.
	OutputModes []string
	// Mutates marks a command that changes state, so it is expected to honor
	// --dry-run.
	Mutates bool
	// Destructive marks a command whose action is destructive. It implies Mutates
	// (so the command honors --dry-run) and is expected to gate the real action
	// behind Factory.ConfirmDestructive.
	Destructive bool
	// RequiresAuth marks a command that needs a credential; the auth pre-run hook
	// loads it before the command runs.
	RequiresAuth bool
	// SupportsMeta marks a command that can attach request metadata, making
	// --include-meta valid in an advanced-output app and advertised in the
	// manifest and generated docs.
	SupportsMeta bool
	// Extensions attaches product-owned, namespaced metadata to the command. It
	// is serialized into the rungrad.extensions manifest annotation and mirrored
	// on CommandSpec.Extensions for catalog validation. Keys are
	// example.com/product style namespaces; values must not reuse core manifest
	// field names.
	Extensions manifest.ExtensionSet
	// GroupID sorts the command under a named help group registered on the App.
	GroupID string
	// Args validates positional arguments (optional).
	Args cobra.PositionalArgs
	// Configure registers local flags and other cobra options (optional).
	Configure func(*cobra.Command)
	// Run executes the command. It receives the shared Factory.
	Run func(f *Factory, cmd *cobra.Command, args []string) error

	subcommands []*Command
}

// AddCommand attaches subcommands.
func (c *Command) AddCommand(subs ...*Command) {
	c.subcommands = append(c.subcommands, subs...)
}

// build constructs the cobra command tree for this command, binding it to f.
func (c *Command) build(f *Factory) *cobra.Command {
	long := c.Long
	if long == "" {
		long = c.Short
	}
	if len(c.Related) > 0 {
		long = strings.TrimRight(long, "\n") + "\n\nRelated commands:\n  " + strings.Join(c.Related, "\n  ")
	}
	long = appendOutputModesHelp(long, c.OutputModes)

	cmd := &cobra.Command{
		Use:           c.Use,
		Short:         c.Short,
		Long:          long,
		GroupID:       c.GroupID,
		SilenceUsage:  true,
		SilenceErrors: true,
		Annotations:   map[string]string{},
	}
	cmd.SetFlagErrorFunc(usageFlagErrorFunc)
	if c.Args != nil {
		validate := c.Args
		cmd.Args = func(cmd *cobra.Command, args []string) error {
			return newUsageError(validate(cmd, args))
		}
	}
	if len(c.Examples) > 0 {
		cmd.Example = strings.Join(c.Examples, "\n")
		cmd.Annotations[AnnotationExamples] = strings.Join(c.Examples, "\n")
	}
	if len(c.Related) > 0 {
		cmd.Annotations[AnnotationRelated] = strings.Join(c.Related, ",")
	}
	if len(c.OutputModes) > 0 {
		cmd.Annotations[AnnotationOutputs] = strings.Join(c.OutputModes, ",")
	}
	if c.Mutates || c.Destructive {
		cmd.Annotations[AnnotationMutates] = "true"
	}
	if c.Destructive {
		cmd.Annotations[AnnotationDestructive] = "true"
	}
	if c.RequiresAuth {
		cmd.Annotations[AnnotationAuth] = "required"
	}
	if c.SupportsMeta {
		cmd.Annotations[AnnotationMeta] = "true"
	}
	if len(c.Extensions) > 0 {
		// Keep extensions on the same annotation path that manifest projection
		// reads; empty namespace objects validate but encode to no annotation.
		encoded, err := manifest.EncodeExtensions(c.Extensions)
		if err != nil {
			panic(fmt.Sprintf("rungrad: command %q has invalid extensions: %v", c.Use, err))
		}
		if encoded != "" {
			cmd.Annotations[AnnotationExtensions] = encoded
		}
	}
	if c.Configure != nil {
		c.Configure(cmd)
	}
	if c.Run != nil {
		run := c.Run
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			return run(f, cmd, args)
		}
	}
	for _, sub := range c.subcommands {
		cmd.AddCommand(sub.build(f))
	}
	return cmd
}

// appendOutputModesHelp mirrors Command.OutputModes into Cobra help. Docs and
// manifest readers use the annotation, but interactive help comes from Long.
func appendOutputModesHelp(long string, modes []string) string {
	if len(modes) == 0 {
		return long
	}
	return strings.TrimRight(long, "\n") + "\n\nOutput modes:\n  " + strings.Join(modes, ", ")
}
