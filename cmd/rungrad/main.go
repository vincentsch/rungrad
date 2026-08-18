// Command rungrad is the framework's own tool. It scaffolds new rungrad CLIs and
// scores any CLI against the agent-ready spec. Being a rungrad command itself, it
// inherits the global flags and dual output it asks of other tools.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/conformance"
	"github.com/vincentsch/rungrad/scaffold"
)

const version = "v0.2.1"

func fields(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

func newApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:    "rungrad",
		Short:   "Build and score agent-ready CLIs",
		Long:    "rungrad scaffolds new agent-ready CLIs and scores any CLI against the rungrad spec.",
		Version: version,
	})
	app.AddCommand(scoreCommand(), newCommand())
	return app
}

func scoreCommand() *rungrad.Command {
	return &rungrad.Command{
		Use:   "score <target>",
		Short: "Score a CLI against the rungrad spec",
		Examples: []string{
			`rungrad score ./mytool --read "item list"`,
			`rungrad score ./mytool --read "item list" --json`,
		},
		Related:     []string{"rungrad new"},
		OutputModes: []string{"table", "json"},
		Args:        cobra.ExactArgs(1),
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().String("read", "", `A read command, e.g. "item list"`)
			cmd.Flags().String("mutate", "", `A mutating command, e.g. "item create demo"`)
			cmd.Flags().String("auth", "", "A command that requires a credential")
			cmd.Flags().String("ambiguous", "", "A resolution command with an ambiguous name")
			cmd.Flags().String("not-found", "", "A command naming a missing resource")
			cmd.Flags().String("api-error", "", "A command that hits an upstream or runtime error")
			cmd.Flags().String("forbidden", "", "A command refused for lacking permission")
			cmd.Flags().String("rate-limited", "", "A command throttled by an upstream service")
			cmd.Flags().String("destructive", "", "A safe/stub destructive command, exercised only through its dry-run and refused-confirmation paths (the scorer never passes --confirm)")
			cmd.Flags().String("secret", "", "A credential-handling command")
			cmd.Flags().String("secret-env", "", "Environment variable carrying the credential")
			cmd.Flags().String("manifest", "auto", "Manifest discovery mode: auto, off, or required")
			cmd.Flags().Bool("update", false, "The target has an update command")
			cmd.Flags().Bool("strict", false, "Exit non-zero if a required rule fails")
		},
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			rs, err := conformance.DefaultRuleset()
			if err != nil {
				return err
			}
			get := func(name string) string { v, _ := cmd.Flags().GetString(name); return v }
			hasUpdate, _ := cmd.Flags().GetBool("update")
			strict, _ := cmd.Flags().GetBool("strict")

			runner, err := conformance.NewRunner(conformance.Target{
				Path:         args[0],
				Read:         fields(get("read")),
				Mutate:       fields(get("mutate")),
				Auth:         fields(get("auth")),
				Ambiguous:    fields(get("ambiguous")),
				NotFound:     fields(get("not-found")),
				APIError:     fields(get("api-error")),
				Forbidden:    fields(get("forbidden")),
				RateLimited:  fields(get("rate-limited")),
				Destructive:  fields(get("destructive")),
				HasUpdate:    hasUpdate,
				Secret:       fields(get("secret")),
				SecretEnv:    get("secret-env"),
				ManifestMode: get("manifest"),
			})
			if err != nil {
				return err
			}
			defer runner.Close()

			if err := runner.DiscoverManifest(); err != nil {
				return err
			}

			result := runner.Score(rs)
			if err := f.WriteResult(result, func(w io.Writer) { fmt.Fprint(w, result.Report()) }); err != nil {
				return err
			}
			if strict {
				if failed := result.RequiredFailures(); len(failed) > 0 {
					return rungrad.NewError(rungrad.ExitUsage, "required rules failed: "+strings.Join(failed, ", "))
				}
			}
			return nil
		},
	}
}

func newCommand() *rungrad.Command {
	return &rungrad.Command{
		Use:         "new <name>",
		Short:       "Scaffold a new rungrad CLI",
		Examples:    []string{"rungrad new mytool", "rungrad new mytool --module github.com/me/mytool", "rungrad new acmectl --product-profile --env-prefix ACME --product-name \"Acme Control\""},
		Related:     []string{"rungrad score"},
		OutputModes: []string{"table", "json"},
		Mutates:     true,
		Args:        cobra.ExactArgs(1),
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().String("module", "", "Go module path (default example.com/<name>)")
			cmd.Flags().String("dir", ".", "Parent directory to create the project in")
			cmd.Flags().Bool("product-profile", false, "Generate the expanded product CLI scaffold")
			cmd.Flags().String("env-prefix", "", "Environment-variable prefix for the product profile (default derived from the name)")
			cmd.Flags().String("product-name", "", "Human product name (AppConfig.Short)")
			cmd.Flags().String("description", "", "Product description (AppConfig.Long)")
			cmd.Flags().StringArray("service", nil, "Service endpoint as name=url (repeatable; product profile)")
			cmd.Flags().String("metadata-namespace", "", "Manifest extension namespace, e.g. example.com/acme")
			cmd.Flags().String("surface", "", "Global-flag surface ownership: rungrad (default) or host")
			cmd.Flags().String("release-owner", "", "Release owner placeholder (commented example only)")
			cmd.Flags().String("release-repo", "", "Release repo placeholder (commented example only)")
			cmd.Flags().String("docs-label", "", "Docs/README title (default: product name)")
			cmd.Flags().StringArray("example", nil, "Extra command example, e.g. \"<tool> widget list\" (repeatable; product profile)")
		},
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			name := args[0]
			module, _ := cmd.Flags().GetString("module")
			dir, _ := cmd.Flags().GetString("dir")
			productProfile, _ := cmd.Flags().GetBool("product-profile")
			if !productProfile {
				// Parsed values cannot distinguish omitted flags from explicitly
				// empty ones like --surface=, so use Changed to enforce the
				// product-profile gate.
				for _, name := range productFlagNames() {
					if cmd.Flags().Changed(name) {
						return &scaffold.ValidationError{Message: fmt.Sprintf("scaffold: --%s requires --product-profile", name)}
					}
				}
			}
			envPrefix, _ := cmd.Flags().GetString("env-prefix")
			productName, _ := cmd.Flags().GetString("product-name")
			description, _ := cmd.Flags().GetString("description")
			services, _ := cmd.Flags().GetStringArray("service")
			metadataNamespace, _ := cmd.Flags().GetString("metadata-namespace")
			surface, _ := cmd.Flags().GetString("surface")
			releaseOwner, _ := cmd.Flags().GetString("release-owner")
			releaseRepo, _ := cmd.Flags().GetString("release-repo")
			docsLabel, _ := cmd.Flags().GetString("docs-label")
			examples, _ := cmd.Flags().GetStringArray("example")
			if productProfile {
				// pflag's StringArray parsing drops an explicit empty value. Treat
				// Changed+empty as invalid input before scaffold.Options loses that
				// distinction.
				if cmd.Flags().Changed("service") && len(services) == 0 {
					return &scaffold.ValidationError{Message: "scaffold: service must be name=url"}
				}
				if cmd.Flags().Changed("example") && len(examples) == 0 {
					return &scaffold.ValidationError{Message: "scaffold: example must not be empty"}
				}
			}
			opts := scaffold.Options{
				Name:              name,
				Module:            module,
				ProductProfile:    productProfile,
				EnvPrefix:         envPrefix,
				ProductName:       productName,
				Description:       description,
				DocsLabel:         docsLabel,
				Services:          services,
				MetadataNamespace: metadataNamespace,
				Surface:           surface,
				ReleaseOwner:      releaseOwner,
				ReleaseRepo:       releaseRepo,
				Examples:          examples,
			}

			if f.DryRun() {
				files, err := scaffold.Generate(opts)
				if err != nil {
					return err
				}
				paths := make([]string, 0, len(files))
				for p := range files {
					paths = append(paths, p)
				}
				sort.Strings(paths)
				return f.WriteResult(
					map[string]any{"dir": filepath.Join(dir, name), "files": paths},
					func(w io.Writer) {
						fmt.Fprintf(w, "DRY RUN: would scaffold %s with %d files\n", name, len(paths))
						for _, p := range paths {
							fmt.Fprintf(w, "  %s\n", p)
						}
					},
				)
			}

			root, err := scaffold.Write(dir, opts)
			if err != nil {
				return err
			}
			return f.WriteResult(map[string]string{"created": root}, func(w io.Writer) {
				fmt.Fprintf(w, "Created %s\n", root)
			})
		},
	}
}

// productFlagNames is the set of rungrad new flags that have meaning only when
// the product scaffold is selected.
func productFlagNames() []string {
	return []string{
		"env-prefix",
		"product-name",
		"description",
		"service",
		"metadata-namespace",
		"surface",
		"release-owner",
		"release-repo",
		"docs-label",
		"example",
	}
}

func main() {
	os.Exit(newApp().Run(os.Args[1:], os.Stdout, os.Stderr))
}
