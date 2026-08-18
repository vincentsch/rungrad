// Package rungrad is a framework for building command-line tools that humans and
// AI agents can both drive well. It wraps cobra so an adopting tool gets the
// agent-ready behaviors of the rungrad spec by default: stable dual output, safe
// dry-run previews, deterministic behavior, name resolution, self-describing
// help, and a stable exit-code contract.
//
// A tool constructs an App, registers Commands, and calls Run. The same binary
// then serves a human at a terminal and a script or agent reading --json.
package rungrad

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vincentsch/rungrad/config"
	"github.com/vincentsch/rungrad/transform"
)

// AppConfig describes the tool being built.
type AppConfig struct {
	Name           string // program name, also the config directory name
	Short          string // one-line description
	Long           string // longer description for the root --help
	Version        string // version string for --version
	EnvVar         string // environment variable holding the credential, e.g. "RGDEMO_TOKEN"
	Profile        string // active credential profile (empty means the default entry)
	AdvancedOutput bool   // enable --plain, --jq, --template, --include-meta, --no-color, --no-ansi, --no-pager, and their validation
	// ErrorPolicy lets a host CLI own error rendering and exit classification.
	// Nil uses rungrad's default renderer and classifier.
	ErrorPolicy *ErrorPolicy
	// Resolution enables opt-in profile/auth-file/service resolution and the
	// matching global flags.
	Resolution *ResolutionConfig
	// Auth overrides credential resolution for RequiresAuth commands. Nil uses
	// the default env-then-stored-credential resolver.
	Auth CredentialResolver
	// Surface configures ownership of framework public surfaces. The zero value
	// keeps rungrad's default behavior.
	Surface SurfaceConfig
}

// Group is a named help section that commands can sort into.
type Group struct {
	ID    string
	Title string
}

// App is a constructed rungrad tool: a root command, the global flags, and the
// shared Factory passed to every command.
type App struct {
	cfg            AppConfig
	flags          *GlobalFlags
	factory        *Factory
	root           *cobra.Command
	advancedOutput bool
	auth           CredentialResolver
	catalog        []CommandSpec
	// machineFlags backs the raw-argument machine-output detector. It is zero in
	// disabled mode and otherwise records the active visible long names.
	machineFlags         machineFlagNames
	machineFlagsActive   bool
	stringGlobalRefs     map[string]*pflag.Flag
	serviceOverrides     map[string]*serviceOverride
	completionSurface    SurfaceMode
	manifestEndpointMode ManifestEndpointMode
	manifestEndpointName string
	// sliceDefaults holds each pflag SliceValue flag's registered default,
	// captured from the live value on the first reset before argv parsing.
	sliceDefaults map[*pflag.Flag][]string
}

// New builds an App with the global flags registered and the validate-then-auth
// pre-run hook installed. Commands and groups are added before calling Run.
func New(cfg AppConfig) *App {
	// Cobra's traverse-run-hooks switch is process-global; enabling it here keeps
	// rungrad's root validate-then-auth pre-run active even when subcommands add
	// their own pre-run hooks.
	cobra.EnableTraverseRunHooks = true
	surface := normalizeSurface(cfg.Surface)
	flags := &GlobalFlags{}
	factory := &Factory{
		Flags:          flags,
		PagerEnvVar:    pagerEnvVarName(cfg.Name),
		advancedOutput: cfg.AdvancedOutput,
		defaultProfile: cfg.Profile,
	}
	root := &cobra.Command{
		Use:           cfg.Name,
		Short:         cfg.Short,
		Long:          cfg.Long,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	if surface.Version == SurfaceRungradOwned {
		root.Version = cfg.Version
	}
	auth := cfg.Auth
	if auth == nil {
		auth = defaultCredentialResolver{}
	}
	a := &App{
		cfg:                  cfg,
		flags:                flags,
		factory:              factory,
		root:                 root,
		advancedOutput:       cfg.AdvancedOutput,
		auth:                 auth,
		stringGlobalRefs:     map[string]*pflag.Flag{},
		serviceOverrides:     map[string]*serviceOverride{},
		completionSurface:    surface.Completion,
		manifestEndpointMode: surface.Manifest.Mode,
	}
	if cfg.Resolution != nil {
		validateResolution(cfg.Resolution)
	}
	switch surface.GlobalFlags.Mode {
	case SurfaceRungradOwned:
		flags.register(root.PersistentFlags())
		if cfg.AdvancedOutput {
			flags.registerAdvanced(root.PersistentFlags())
		}
		if cfg.Resolution != nil {
			flags.registerResolution(root.PersistentFlags(), cfg.Resolution)
			a.registerServiceFlags(root.PersistentFlags())
		}
		a.captureRungradGlobalRefs()
	case SurfaceHostOwned:
		if err := a.BindGlobalFlags(root.PersistentFlags(), surface.GlobalFlags.Bindings); err != nil {
			panic(err.Error())
		}
	case SurfaceDisabled:
		// No rungrad globals are registered; product-local flags with the same
		// visible names stay outside rungrad's output-mode detector.
	}
	root.SetFlagErrorFunc(usageFlagErrorFunc)
	root.PersistentPreRunE = a.preRunValidateThenAuth
	switch surface.Completion {
	case SurfaceHostOwned, SurfaceDisabled:
		root.CompletionOptions.DisableDefaultCmd = true
	}
	name, render := a.validateManifestEndpoint(surface)
	if surface.Manifest.Mode == ManifestEndpointRenamed {
		a.manifestEndpointName = name
	}
	switch surface.Manifest.Mode {
	case ManifestEndpointRungradOwned:
		root.AddCommand(a.manifestEndpointCommand(manifestCommandName, nil))
	case ManifestEndpointRenamed:
		root.AddCommand(a.manifestEndpointCommand(name, nil))
	case ManifestEndpointHostRendered:
		root.AddCommand(a.manifestEndpointCommand(manifestCommandName, render))
	case ManifestEndpointDisabled:
		// no endpoint
	}
	return a
}

// toolEnvVar derives a shell-friendly tool-specific env var with the supplied
// suffix.
func toolEnvVar(name, suffix string) string {
	var b strings.Builder
	lastUnderscore := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
			b.WriteByte(c - ('a' - 'A'))
			lastUnderscore = false
		case (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
			b.WriteByte(c)
			lastUnderscore = false
		default:
			if b.Len() > 0 && !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	derived := strings.Trim(b.String(), "_")
	if derived == "" {
		return ""
	}
	if suffix == "" {
		return derived
	}
	return derived + "_" + suffix
}

// pagerEnvVarName derives the tool-specific pager env var from the app name.
func pagerEnvVarName(name string) string { return toolEnvVar(name, "PAGER") }

// Root returns the underlying cobra root command for advanced wiring.
func (a *App) Root() *cobra.Command { return a.root }

// Factory returns the shared dependency carrier.
func (a *App) Factory() *Factory { return a.factory }

// Flags returns the resolved global flags.
func (a *App) Flags() *GlobalFlags { return a.flags }

// AddGroup registers named help groups.
func (a *App) AddGroup(groups ...Group) {
	for _, g := range groups {
		a.addGroupChecked(g)
	}
}

// AddCommand registers top-level commands.
func (a *App) AddCommand(cmds ...*Command) {
	for _, c := range cmds {
		// Build first so Cobra applies its normal first-token Name() parsing
		// before we compare against reserved framework command names.
		built := c.build(a.factory)
		a.validateCommandTreeNames(built)
		a.root.AddCommand(built)
	}
}

// preRunValidateThenAuth is the root persistent pre-run. Cobra runs its native
// required-flag and flag-group validation after this hook, so those failures
// would otherwise classify as runtime/API errors and, for auth-required commands,
// be masked by the credential load below. Running the validators here, before
// auth, makes those bad invocations classify as usage errors without depending
// on Cobra's English message wording. The validators are idempotent reads of the
// already-parsed leaf command flags, and they no-op when flag parsing is disabled
// (for example, shell completion).
func (a *App) preRunValidateThenAuth(cmd *cobra.Command, args []string) error {
	// The hidden manifest endpoint emits metadata only. It must not validate
	// adopter flags, assign the config store, or load credentials.
	if cmd.Annotations[annotationSkipPreRun] != "" {
		return nil
	}
	// Bad flag usage should win over auth failures, so callers get the usage
	// exit code even when the command would otherwise need a credential.
	if err := cmd.ValidateRequiredFlags(); err != nil {
		return newUsageError(err)
	}
	if err := cmd.ValidateFlagGroups(); err != nil {
		return newUsageError(err)
	}
	if err := a.guardOutputMode(cmd); err != nil {
		return err
	}
	store := config.Store{Tool: a.cfg.Name, Override: a.flags.Config}
	profile := a.cfg.Profile
	if a.cfg.Resolution != nil {
		resolved, err := a.buildResolution(cmd)
		if err != nil {
			return err
		}
		a.factory.setResolved(resolved)
		store.Override = resolved.ConfigPath
		store.Credentials = resolved.AuthFilePath
		profile = resolved.Profile
	}
	a.factory.Store = store
	// From this point on, error hooks may safely expose resolved profile,
	// config/auth-file, and service fields from the Factory.
	a.factory.storeReady = true
	if cmd.Annotations[AnnotationAuth] != "required" {
		return nil
	}
	services := map[string]config.ResolvedService(nil)
	if a.factory.resolvedSet {
		services = a.factory.resolved.Services
	}
	ac := &AuthContext{
		Context:        cmd.Context(),
		Profile:        profile,
		ConfigPath:     a.factory.ConfigPath(),
		AuthFilePath:   a.factory.AuthFilePath(),
		EnvVar:         a.cfg.EnvVar,
		Store:          store,
		LookupEnv:      a.factory.lookupEnv,
		RegisterSecret: a.factory.RegisterSecret,
		services:       services,
	}
	cred, err := a.auth.ResolveCredential(ac)
	if err != nil {
		return err
	}
	a.factory.Token = cred.Token
	a.factory.setCredential(cred)
	a.factory.RegisterSecret(cred.Token)
	return nil
}

// Run executes the tool with the given args and writers, reading prompts from
// os.Stdin, and returns the process exit code.
func (a *App) Run(args []string, stdout, stderr io.Writer) int {
	return a.RunIO(args, os.Stdin, stdout, stderr)
}

// RunIO is Run with an explicit input reader, so tests and embedders can drive
// interactive prompts. Errors are printed in the active output mode and
// classified into a stable exit code.
func (a *App) RunIO(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a.resetForRun()
	a.factory.Stdin = stdin
	a.factory.Stdout = stdout
	a.factory.Stderr = stderr
	if args == nil {
		args = []string{}
	}
	a.root.SetArgs(args)
	a.root.SetIn(stdin)
	a.root.SetOut(stdout)
	a.root.SetErr(stderr)

	cmd, err := a.root.ExecuteC()
	// Cobra lazily adds its default completion command during ExecuteC. Mark it
	// after every run so later docs/help/manifest walks can filter it.
	a.markFrameworkCompletion()
	if err != nil {
		return a.handleError(args, cmd, err, stderr)
	}
	return ExitSuccess
}

func (a *App) resetForRun() {
	if a.sliceDefaults == nil {
		a.sliceDefaults = make(map[*pflag.Flag][]string)
	}
	a.resetCommandFlags(a.root)
	a.resetGlobalFlagState()
	a.factory.Store = config.Store{}
	a.factory.storeReady = false
	a.factory.Token = ""
	a.factory.resolved = config.Resolved{}
	a.factory.resolvedSet = false
	a.factory.credential = Credential{}
	a.factory.jqActive, a.factory.jqExpr = false, ""
	a.factory.tmplActive, a.factory.tmplText = false, ""
	a.factory.metaActive = false
	a.factory.resetSecrets()
}

func (a *App) resetCommandFlags(cmd *cobra.Command) {
	a.resetFlagSet(cmd.Flags())
	a.resetFlagSet(cmd.PersistentFlags())
	for _, sub := range cmd.Commands() {
		a.resetCommandFlags(sub)
	}
}

func (a *App) resetFlagSet(fs *pflag.FlagSet) {
	fs.VisitAll(func(flag *pflag.Flag) {
		if sv, ok := flag.Value.(pflag.SliceValue); ok {
			// Slice flags cannot be reset by feeding DefValue back through Set:
			// DefValue is pflag's bracketed display form, and Set would also leave
			// the private append-mode bit set. Snapshot the registered default once
			// and restore through the slice-aware helper instead.
			def, ok := a.sliceDefaults[flag]
			if !ok {
				def = cloneStrings(sv.GetSlice())
				a.sliceDefaults[flag] = def
			}
			resetSliceFlag(flag, sv, def)
			return
		}
		_ = flag.Value.Set(flag.DefValue)
		flag.Changed = false
	})
}

type errorDetailer interface {
	ErrorDetails() any
}

func isUsageError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Cobra exposes unknown-command failures as formatted text. Flag and positional
	// argument errors are wrapped at registration time so runtime errors are not
	// classified by incidental English words in their messages.
	return strings.HasPrefix(msg, "unknown command ")
}

func usageFlagErrorFunc(_ *cobra.Command, err error) error {
	return newFlagParseError(err)
}

// guardOutputMode validates advanced output flags before auth and before the
// command handler can do work. Parse errors are usage errors here; failures that
// depend on the command result happen later in Factory.writeResolved.
func (a *App) guardOutputMode(cmd *cobra.Command) error {
	if !a.advancedOutput {
		return nil
	}
	jqSet := a.globalChanged("jq")
	tmplSet := a.globalChanged("template")
	plain := a.flags.Plain
	jsonOn := a.flags.JSON
	includeMeta := a.flags.IncludeMeta
	machineSelected := jsonOn || jqSet || tmplSet

	if jqSet && tmplSet {
		return NewError(ExitUsage, "--jq and --template cannot be combined")
	}
	if plain && (jsonOn || jqSet || tmplSet || includeMeta) {
		return NewError(ExitUsage, "--plain cannot be combined with --json, --jq, --template, or --include-meta")
	}
	if plain && !commandSupportsPlain(cmd) {
		path := cmd.CommandPath()
		return NewError(ExitUsage, fmt.Sprintf(
			"%q does not support --plain. Run %q for supported output modes.",
			path, path+" --help"))
	}
	if (jqSet || tmplSet) && !commandSupportsTransform(cmd) {
		path := cmd.CommandPath()
		return NewError(ExitUsage, fmt.Sprintf(
			"%q does not support --jq or --template because it has no stable JSON output. Run %q for supported output modes.",
			path, path+" --help"))
	}
	if includeMeta {
		// Metadata is a modifier on machine output, not a standalone output mode.
		// Check capability and machine mode before the dry-run conflict so users
		// get the error tied to the primary misuse.
		if !commandSupportsMeta(cmd) {
			path := cmd.CommandPath()
			return NewError(ExitUsage, fmt.Sprintf(
				"%q does not support --include-meta. Run %q for supported output modes.",
				path, path+" --help"))
		}
		if !machineSelected {
			return NewError(ExitUsage, "--include-meta requires --json, --jq, or --template")
		}
		if a.flags.DryRun {
			return NewError(ExitUsage, "--include-meta cannot be combined with --dry-run")
		}
	}

	// Store active transform state on the shared Factory only after the flag
	// combination and command capability checks have passed.
	if jqSet {
		if err := transform.ValidateJQ(a.flags.JQ); err != nil {
			return err
		}
		a.factory.jqActive, a.factory.jqExpr = true, a.flags.JQ
	}
	if tmplSet {
		if err := transform.ValidateTemplate(a.flags.Template); err != nil {
			return err
		}
		a.factory.tmplActive, a.factory.tmplText = true, a.flags.Template
	}
	if includeMeta {
		// Set only after every guard has passed; wrapMeta treats this as the
		// authoritative signal that the envelope is valid for this run.
		a.factory.metaActive = true
	}
	return nil
}

func commandOutputModes(cmd *cobra.Command) []string {
	return splitNonEmpty(cmd.Annotations[AnnotationOutputs], ",")
}

// commandSupportsPlain reports whether the leaf command declared plain output in
// its OutputModes (projected to the rungrad.outputs annotation).
func commandSupportsPlain(cmd *cobra.Command) bool {
	for _, m := range commandOutputModes(cmd) {
		if m == OutputModePlain {
			return true
		}
	}
	return false
}

// commandSupportsTransform reports whether the leaf command declared
// jq/template in its OutputModes (projected to the rungrad.outputs annotation).
func commandSupportsTransform(cmd *cobra.Command) bool {
	for _, m := range commandOutputModes(cmd) {
		if m == OutputModeJQ || m == OutputModeTemplate {
			return true
		}
	}
	return false
}

// commandSupportsMeta reports whether the leaf command declared SupportsMeta
// (projected to the rungrad.meta annotation), so --include-meta is accepted.
func commandSupportsMeta(cmd *cobra.Command) bool {
	return cmd.Annotations[AnnotationMeta] == "true"
}
