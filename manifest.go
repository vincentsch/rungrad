package rungrad

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vincentsch/rungrad/internal/cmdtree"
	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/output"
	"github.com/vincentsch/rungrad/spec"
)

const manifestCommandName = "__rungrad_manifest"

// annotationSkipPreRun marks a framework-internal command that must bypass the
// root validate-then-auth pre-run.
const annotationSkipPreRun = "rungrad.skipPreRun"

// manifestEndpointCommand builds a hidden machine endpoint. It uses two bypasses:
// the skip annotation avoids framework validation/auth work, while
// DisableFlagParsing makes Cobra's own required-flag checks no-op for this
// command.
func (a *App) manifestEndpointCommand(name string, render func(ManifestEndpointContext) error) *cobra.Command {
	return &cobra.Command{
		Use:                name,
		Short:              "Emit the rungrad machine manifest (internal)",
		Hidden:             true,
		DisableFlagParsing: true,
		SilenceUsage:       true,
		SilenceErrors:      true,
		Annotations:        map[string]string{annotationSkipPreRun: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			doc, err := a.ManifestDocumentChecked()
			if err != nil {
				return err
			}
			if render == nil {
				b, err := output.StableJSON(doc)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(b)
				return err
			}
			// Host-rendered endpoints own the bytes, but only successful renders
			// are flushed so partial manifest output cannot leak on errors.
			var staged bytes.Buffer
			if err := render(ManifestEndpointContext{Command: cmd, Manifest: doc, Stdout: &staged}); err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(staged.Bytes())
			return err
		},
	}
}

// ManifestDocument returns the typed rungrad-manifest/1 document for the built
// command tree. The hidden endpoint renders this same document; host-rendered
// endpoints receive it via ManifestEndpointContext. Every array field is non-nil
// so StableJSON emits [] rather than null.
func (a *App) ManifestDocument() manifest.Manifest {
	// The hidden endpoint runs after Cobra may have created its default
	// completion command, so mark it before walking visible commands.
	a.markFrameworkCompletion()
	root := a.root
	globals := cmdtree.GlobalFlagNames(root)
	doc := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		SpecVersion:   spec.Version,
		ToolName:      a.cfg.Name,
		ToolVersion:   a.cfg.Version,
		GlobalFlags:   flagEntries(root.PersistentFlags(), nil),
		Commands:      []manifest.Command{},
	}
	for _, cmd := range cmdtree.VisibleCommands(root) {
		doc.Commands = append(doc.Commands, commandEntry(cmd, globals))
	}
	return doc
}

// ManifestDocumentChecked is ManifestDocument with error reporting for malformed
// raw rungrad.extensions annotations introduced by manual Cobra mutation.
func (a *App) ManifestDocumentChecked() (manifest.Manifest, error) {
	// The hidden endpoint runs after Cobra may have created its default
	// completion command, so mark it before walking visible commands.
	a.markFrameworkCompletion()
	root := a.root
	globals := cmdtree.GlobalFlagNames(root)
	doc := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		SpecVersion:   spec.Version,
		ToolName:      a.cfg.Name,
		ToolVersion:   a.cfg.Version,
		GlobalFlags:   flagEntries(root.PersistentFlags(), nil),
		Commands:      []manifest.Command{},
	}
	for _, cmd := range cmdtree.VisibleCommands(root) {
		entry, err := commandEntryChecked(cmd, globals)
		if err != nil {
			return manifest.Manifest{}, err
		}
		doc.Commands = append(doc.Commands, entry)
	}
	return doc, nil
}

// commandEntryChecked converts one Cobra command into the manifest schema. It
// reads the same annotations that help and docs generation read, keeping those
// views tied to one source of truth.
func commandEntryChecked(cmd *cobra.Command, globals map[string]bool) (manifest.Command, error) {
	mutates := cmd.Annotations[AnnotationMutates] == "true"
	destructive := cmd.Annotations[AnnotationDestructive] == "true"
	entry := manifest.Command{
		Path:                 commandPath(cmd),
		Use:                  cmd.Use,
		Short:                cmd.Short,
		Examples:             splitNonEmpty(cmd.Example, "\n"),
		Related:              splitNonEmpty(cmd.Annotations[AnnotationRelated], ","),
		OutputModes:          splitNonEmpty(cmd.Annotations[AnnotationOutputs], ","),
		RequiresAuth:         cmd.Annotations[AnnotationAuth] == "required",
		Mutates:              mutates,
		SupportsDryRun:       mutates,
		Destructive:          destructive,
		RequiresConfirmation: destructive,
		SupportsMeta:         cmd.Annotations[AnnotationMeta] == "true",
		LocalFlags:           flagEntries(cmd.LocalFlags(), globals),
	}
	if raw := cmd.Annotations[AnnotationExtensions]; raw != "" {
		ext, err := manifest.DecodeExtensions(raw)
		if err != nil {
			return manifest.Command{}, fmt.Errorf("command %q: invalid %s annotation: %w",
				strings.Join(commandPath(cmd), " "), AnnotationExtensions, err)
		}
		entry.Extensions = ext
	}
	return entry, nil
}

// commandEntry is the panic-on-error projection for valid framework-built trees,
// used by App.ManifestDocument().
func commandEntry(cmd *cobra.Command, globals map[string]bool) manifest.Command {
	entry, err := commandEntryChecked(cmd, globals)
	if err != nil {
		panic("rungrad: " + err.Error())
	}
	return entry
}

// commandPath returns the command path without the executable name. The root
// command is represented by a non-nil empty slice so JSON emits [].
func commandPath(cmd *cobra.Command) []string {
	fields := strings.Fields(cmd.CommandPath())
	out := []string{}
	if len(fields) > 1 {
		out = append(out, fields[1:]...)
	}
	return out
}

// splitNonEmpty preserves the manifest's array shape for absent metadata.
func splitNonEmpty(s, sep string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, sep)
}

// flagEntries returns visible flags sorted by name. The skip set is used for
// local_flags so root persistent globals do not get reported twice.
func flagEntries(fs *pflag.FlagSet, skip map[string]bool) []manifest.Flag {
	out := []manifest.Flag{}
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || skip[f.Name] {
			return
		}
		out = append(out, manifest.Flag{
			Name:      f.Name,
			Shorthand: f.Shorthand,
			Usage:     f.Usage,
			Default:   f.DefValue,
			Type:      f.Value.Type(),
			Required:  flagRequired(f),
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// flagRequired reads Cobra's required-flag annotation, which is how
// MarkFlagRequired records the state on a pflag flag.
func flagRequired(f *pflag.Flag) bool {
	v, ok := f.Annotations[cobra.BashCompOneRequiredFlag]
	return ok && len(v) > 0 && v[0] == "true"
}
