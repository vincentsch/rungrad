package rungrad

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vincentsch/rungrad/manifest"
)

// SurfaceMode declares which side owns a public framework surface.
type SurfaceMode string

const (
	SurfaceRungradOwned SurfaceMode = "rungrad"
	SurfaceHostOwned    SurfaceMode = "host"
	SurfaceDisabled     SurfaceMode = "disabled"
)

// GlobalFlagBinding describes one host-owned rungrad global flag.
type GlobalFlagBinding struct {
	Name      string
	Shorthand string
	Usage     string
	Hidden    bool
}

// GlobalFlagBindings maps every rungrad-recognized global flag identity to the
// host-visible flag that should drive it.
type GlobalFlagBindings struct {
	JSON, DryRun, NoPrompt, Quiet, Config GlobalFlagBinding
	Profile, AuthFile                     GlobalFlagBinding
	Plain, JQ, Template, IncludeMeta      GlobalFlagBinding
	NoColor, NoANSI, NoPager              GlobalFlagBinding
	Services                              map[string]GlobalFlagBinding
}

// GlobalFlagSurface configures ownership of rungrad's global flags.
type GlobalFlagSurface struct {
	Mode     SurfaceMode
	Bindings GlobalFlagBindings
}

// ManifestEndpointMode declares how the hidden machine-manifest endpoint is
// exposed.
type ManifestEndpointMode string

const (
	ManifestEndpointRungradOwned ManifestEndpointMode = "rungrad"
	ManifestEndpointDisabled     ManifestEndpointMode = "disabled"
	ManifestEndpointRenamed      ManifestEndpointMode = "renamed"
	ManifestEndpointHostRendered ManifestEndpointMode = "host-rendered"
)

// ManifestEndpointSurface configures the hidden manifest endpoint.
type ManifestEndpointSurface struct {
	Mode   ManifestEndpointMode
	Name   string
	Render func(ManifestEndpointContext) error
}

// ManifestEndpointContext is passed to a host-rendered manifest endpoint.
type ManifestEndpointContext struct {
	Command  *cobra.Command
	Manifest manifest.Manifest
	Stdout   io.Writer
}

// SurfaceConfig configures ownership of rungrad's public framework surfaces.
type SurfaceConfig struct {
	GlobalFlags GlobalFlagSurface
	Version     SurfaceMode
	Completion  SurfaceMode
	Manifest    ManifestEndpointSurface
}

// serviceOverride is the per-service flag state used by resolution. Service
// flags do not have fields on GlobalFlags, so both rungrad-owned and host-owned
// registrations write into this small carrier instead.
type serviceOverride struct {
	def   string
	value string
	set   bool
}

// serviceFlagValue is the pflag.Value for service flags. It reports Type()
// "string" so help and manifest output render the same way as pflag.String.
type serviceFlagValue struct{ o *serviceOverride }

func (v serviceFlagValue) String() string {
	if v.o == nil {
		return ""
	}
	return v.o.value
}

func (v serviceFlagValue) Set(s string) error {
	if v.o != nil {
		v.o.value = s
		v.o.set = true
	}
	return nil
}

func (v serviceFlagValue) Type() string { return "string" }

// normalizeSurface fills zero-value ownership modes with rungrad's default
// behavior and rejects unknown mode strings during app construction.
func normalizeSurface(s SurfaceConfig) SurfaceConfig {
	if s.GlobalFlags.Mode == "" {
		s.GlobalFlags.Mode = SurfaceRungradOwned
	}
	if s.Version == "" {
		s.Version = SurfaceRungradOwned
	}
	if s.Completion == "" {
		s.Completion = SurfaceRungradOwned
	}
	if s.Manifest.Mode == "" {
		s.Manifest.Mode = ManifestEndpointRungradOwned
	}
	for _, mode := range []SurfaceMode{s.GlobalFlags.Mode, s.Version, s.Completion} {
		if !validSurfaceMode(mode) {
			panic(fmt.Sprintf("rungrad: invalid surface mode %q", mode))
		}
	}
	if !validManifestEndpointMode(s.Manifest.Mode) {
		panic(fmt.Sprintf("rungrad: invalid manifest endpoint mode %q", s.Manifest.Mode))
	}
	return s
}

func validSurfaceMode(mode SurfaceMode) bool {
	switch mode {
	case SurfaceRungradOwned, SurfaceHostOwned, SurfaceDisabled:
		return true
	default:
		return false
	}
}

func validManifestEndpointMode(mode ManifestEndpointMode) bool {
	switch mode {
	case ManifestEndpointRungradOwned, ManifestEndpointDisabled, ManifestEndpointRenamed, ManifestEndpointHostRendered:
		return true
	default:
		return false
	}
}

type globalFlagKind int

const (
	globalFlagBool globalFlagKind = iota
	globalFlagString
	globalFlagService
)

// globalFlagSpec is the internal recipe for one rungrad-recognized global flag.
// It keeps the stable identity separate from the host-visible flag name.
type globalFlagSpec struct {
	id           string
	binding      GlobalFlagBinding
	kind         globalFlagKind
	boolTarget   *bool
	stringTarget *string
	service      *Service
	machine      bool
	noShorthand  bool
	usage        string
}

// BindGlobalFlags registers host-owned global flags on fs, binding each to the
// same canonical rungrad state used by the default framework-owned flags.
func (a *App) BindGlobalFlags(fs *pflag.FlagSet, bindings GlobalFlagBindings) error {
	if a == nil || a.flags == nil {
		return fmt.Errorf("rungrad: cannot bind global flags before app initialization")
	}
	if fs == nil {
		return fmt.Errorf("rungrad: cannot bind global flags on a nil flag set")
	}
	specs := a.applicableGlobalFlagSpecs(bindings)
	if err := a.validateGlobalFlagBindings(fs, specs, bindings); err != nil {
		return err
	}
	for _, spec := range specs {
		// Registration mutates the supplied FlagSet, so every collision and
		// malformed binding check must have completed before this loop starts.
		binding := spec.binding
		usage := binding.Usage
		if usage == "" {
			usage = spec.usage
		}
		switch spec.kind {
		case globalFlagBool:
			if binding.Shorthand == "" {
				fs.BoolVar(spec.boolTarget, binding.Name, false, usage)
			} else {
				fs.BoolVarP(spec.boolTarget, binding.Name, binding.Shorthand, false, usage)
			}
		case globalFlagString:
			if binding.Shorthand == "" {
				fs.StringVar(spec.stringTarget, binding.Name, "", usage)
			} else {
				fs.StringVarP(spec.stringTarget, binding.Name, binding.Shorthand, "", usage)
			}
			a.stringGlobalRefs[spec.id] = fs.Lookup(binding.Name)
		case globalFlagService:
			o := &serviceOverride{def: spec.service.Default, value: spec.service.Default}
			a.serviceOverrides[spec.service.Name] = o
			if binding.Shorthand == "" {
				fs.Var(serviceFlagValue{o}, binding.Name, usage)
			} else {
				fs.VarP(serviceFlagValue{o}, binding.Name, binding.Shorthand, usage)
			}
		}
		if f := fs.Lookup(binding.Name); f != nil && binding.Hidden {
			f.Hidden = true
		}
		if spec.machine {
			// Early command-resolution errors are detected from raw argv, before
			// parsed flag state may exist. Record the host-visible long names for
			// that detector.
			switch spec.id {
			case "json":
				a.machineFlags.JSON = binding.Name
			case "jq":
				a.machineFlags.JQ = binding.Name
			case "template":
				a.machineFlags.Template = binding.Name
			}
		}
	}
	a.machineFlagsActive = true
	return nil
}

// applicableGlobalFlagSpecs returns the rungrad flag identities that are active
// for this app. Host-owned bindings are all-or-nothing across this list.
func (a *App) applicableGlobalFlagSpecs(bindings GlobalFlagBindings) []globalFlagSpec {
	specs := []globalFlagSpec{
		{id: "json", binding: bindings.JSON, kind: globalFlagBool, boolTarget: &a.flags.JSON, machine: true, noShorthand: true, usage: "Output stable JSON instead of the human view"},
		{id: "dry-run", binding: bindings.DryRun, kind: globalFlagBool, boolTarget: &a.flags.DryRun, usage: "Preview changes without performing them"},
		{id: "no-prompt", binding: bindings.NoPrompt, kind: globalFlagBool, boolTarget: &a.flags.NoPrompt, usage: "Never block on an interactive prompt"},
		{id: "quiet", binding: bindings.Quiet, kind: globalFlagBool, boolTarget: &a.flags.Quiet, usage: "Suppress non-essential output"},
		{id: "config", binding: bindings.Config, kind: globalFlagString, stringTarget: &a.flags.Config, usage: "Path to the config file"},
	}
	if a.advancedOutput {
		specs = append(specs,
			globalFlagSpec{id: "plain", binding: bindings.Plain, kind: globalFlagBool, boolTarget: &a.flags.Plain, usage: "Print unstyled, copy-safe text (commands with human output)"},
			globalFlagSpec{id: "jq", binding: bindings.JQ, kind: globalFlagString, stringTarget: &a.flags.JQ, machine: true, noShorthand: true, usage: "Transform stable JSON output with a jq expression (commands with machine output)"},
			globalFlagSpec{id: "template", binding: bindings.Template, kind: globalFlagString, stringTarget: &a.flags.Template, machine: true, noShorthand: true, usage: "Render stable JSON output through a Go text/template (commands with machine output)"},
			globalFlagSpec{id: "include-meta", binding: bindings.IncludeMeta, kind: globalFlagBool, boolTarget: &a.flags.IncludeMeta, usage: "Wrap machine output as {data, meta} (commands that expose request metadata)"},
			globalFlagSpec{id: "no-color", binding: bindings.NoColor, kind: globalFlagBool, boolTarget: &a.flags.NoColor, usage: "Disable color in human output"},
			globalFlagSpec{id: "no-ansi", binding: bindings.NoANSI, kind: globalFlagBool, boolTarget: &a.flags.NoANSI, usage: "Disable all ANSI/control sequences in human output"},
			globalFlagSpec{id: "no-pager", binding: bindings.NoPager, kind: globalFlagBool, boolTarget: &a.flags.NoPager, usage: "Never use a pager for long human output"},
		)
	}
	if a.cfg.Resolution != nil {
		if a.cfg.Resolution.Profile {
			specs = append(specs, globalFlagSpec{id: "profile", binding: bindings.Profile, kind: globalFlagString, stringTarget: &a.flags.Profile, usage: "Profile to use for config and credentials"})
		}
		if a.cfg.Resolution.AuthFile {
			specs = append(specs, globalFlagSpec{id: "auth-file", binding: bindings.AuthFile, kind: globalFlagString, stringTarget: &a.flags.AuthFile, usage: "Path to the credentials file"})
		}
		for i := range a.cfg.Resolution.Services {
			svc := &a.cfg.Resolution.Services[i]
			if svc.Flag == "" {
				continue
			}
			b := GlobalFlagBinding{}
			if bindings.Services != nil {
				b = bindings.Services[svc.Name]
			}
			specs = append(specs, globalFlagSpec{
				id:      "service:" + svc.Name,
				binding: b,
				kind:    globalFlagService,
				service: svc,
				usage:   svc.Usage,
			})
		}
	}
	return specs
}

// validateGlobalFlagBindings checks the whole binding set before registration.
// It compares pflag-normalized long names because a FlagSet may treat names such
// as "foo-bar" and "foo_bar" as the same flag.
func (a *App) validateGlobalFlagBindings(fs *pflag.FlagSet, specs []globalFlagSpec, bindings GlobalFlagBindings) error {
	applicable := map[string]globalFlagSpec{}
	longNames := map[string]string{}
	shorthands := map[string]string{}
	normalizeName := fs.GetNormalizeFunc()
	for _, spec := range specs {
		applicable[spec.id] = spec
		b := spec.binding
		if b.Hidden && b.Name == "" {
			return fmt.Errorf("rungrad: hidden host binding for %q requires a name", spec.id)
		}
		if b.Name == "" {
			return fmt.Errorf("rungrad: missing host binding for global flag %q", spec.id)
		}
		if len(b.Shorthand) > 1 {
			return fmt.Errorf("rungrad: host binding shorthand %q must be one ASCII character", b.Shorthand)
		}
		if spec.noShorthand && b.Shorthand != "" {
			return fmt.Errorf("rungrad: global flag %q cannot define a shorthand", spec.id)
		}
		normalizedName := string(normalizeName(fs, b.Name))
		if prev, ok := longNames[normalizedName]; ok {
			return fmt.Errorf("rungrad: host binding %q duplicates global flag %q", b.Name, prev)
		}
		longNames[normalizedName] = spec.id
		if b.Shorthand != "" {
			if prev, ok := shorthands[b.Shorthand]; ok {
				return fmt.Errorf("rungrad: host binding shorthand %q duplicates global flag %q", b.Shorthand, prev)
			}
			shorthands[b.Shorthand] = spec.id
		}
		if fs.Lookup(b.Name) != nil {
			return fmt.Errorf("rungrad: host binding %q collides with an existing flag on the flag set", b.Name)
		}
		if b.Shorthand != "" && fs.ShorthandLookup(b.Shorthand) != nil {
			return fmt.Errorf("rungrad: host binding %q collides with an existing flag on the flag set", b.Name)
		}
	}
	for short := range shorthands {
		if longNames[short] != "" {
			return fmt.Errorf("rungrad: host binding shorthand %q collides with a long flag name", short)
		}
	}
	for _, disabled := range disabledFixedGlobalBindings(a, bindings) {
		if disabled.binding.Name != "" {
			return fmt.Errorf("rungrad: host binding %q provided for a feature that is not enabled", disabled.id)
		}
	}
	if bindings.Services != nil {
		for key := range bindings.Services {
			if _, ok := applicable["service:"+key]; !ok {
				return fmt.Errorf("rungrad: unknown service binding key %q", key)
			}
		}
	}
	return nil
}

// disabledFixedGlobalBindings returns fixed rungrad identities that are not
// active for this app, so host bindings for disabled features can fail clearly.
func disabledFixedGlobalBindings(a *App, bindings GlobalFlagBindings) []globalFlagSpec {
	all := []globalFlagSpec{
		{id: "json", binding: bindings.JSON},
		{id: "dry-run", binding: bindings.DryRun},
		{id: "no-prompt", binding: bindings.NoPrompt},
		{id: "quiet", binding: bindings.Quiet},
		{id: "config", binding: bindings.Config},
		{id: "profile", binding: bindings.Profile},
		{id: "auth-file", binding: bindings.AuthFile},
		{id: "plain", binding: bindings.Plain},
		{id: "jq", binding: bindings.JQ},
		{id: "template", binding: bindings.Template},
		{id: "include-meta", binding: bindings.IncludeMeta},
		{id: "no-color", binding: bindings.NoColor},
		{id: "no-ansi", binding: bindings.NoANSI},
		{id: "no-pager", binding: bindings.NoPager},
	}
	applicable := map[string]bool{
		"json":      true,
		"dry-run":   true,
		"no-prompt": true,
		"quiet":     true,
		"config":    true,
	}
	if a.advancedOutput {
		for _, id := range []string{"plain", "jq", "template", "include-meta", "no-color", "no-ansi", "no-pager"} {
			applicable[id] = true
		}
	}
	if a.cfg.Resolution != nil {
		if a.cfg.Resolution.Profile {
			applicable["profile"] = true
		}
		if a.cfg.Resolution.AuthFile {
			applicable["auth-file"] = true
		}
	}
	var out []globalFlagSpec
	for _, spec := range all {
		if !applicable[spec.id] {
			out = append(out, spec)
		}
	}
	return out
}

// globalChanged reports set-ness by rungrad's stable internal identity rather
// than by the flag's visible name, which may be host-renamed.
func (a *App) globalChanged(id string) bool {
	if a == nil || a.stringGlobalRefs == nil {
		return false
	}
	ref := a.stringGlobalRefs[id]
	return ref != nil && ref.Changed
}

// resetGlobalFlagState clears state that lives outside pflag's own Changed bit.
// resetFlagSet restores service values by calling Set(DefValue), which marks the
// custom value as set; this method turns those defaults back into "not set".
func (a *App) resetGlobalFlagState() {
	for _, o := range a.serviceOverrides {
		if o == nil {
			continue
		}
		o.value = o.def
		o.set = false
	}
}

// captureRungradGlobalRefs records the default framework-owned flag refs under
// the same identities that host-owned binding uses. Resolution and transforms
// then read one path regardless of which side owns the visible flag names.
func (a *App) captureRungradGlobalRefs() {
	pf := a.root.PersistentFlags()
	a.stringGlobalRefs["config"] = pf.Lookup("config")
	if a.cfg.Resolution != nil {
		if a.cfg.Resolution.Profile {
			a.stringGlobalRefs["profile"] = pf.Lookup("profile")
		}
		if a.cfg.Resolution.AuthFile {
			a.stringGlobalRefs["auth-file"] = pf.Lookup("auth-file")
		}
	}
	if a.advancedOutput {
		a.stringGlobalRefs["jq"] = pf.Lookup("jq")
		a.stringGlobalRefs["template"] = pf.Lookup("template")
	}
	a.machineFlags = machineFlagNames{JSON: "json", JQ: "jq", Template: "template"}
	a.machineFlagsActive = true
}

// validateManifestEndpoint enforces the endpoint ownership matrix and returns
// the concrete command name plus an optional host renderer.
func (a *App) validateManifestEndpoint(surface SurfaceConfig) (string, func(ManifestEndpointContext) error) {
	m := surface.Manifest
	switch m.Mode {
	case ManifestEndpointRungradOwned:
		if m.Name != "" || m.Render != nil {
			panic("rungrad: rungrad-owned manifest endpoint does not accept Name or Render")
		}
		return manifestCommandName, nil
	case ManifestEndpointDisabled:
		if m.Name != "" || m.Render != nil {
			panic("rungrad: disabled manifest endpoint does not accept Name or Render")
		}
		return "", nil
	case ManifestEndpointRenamed:
		if m.Name == "" {
			panic("rungrad: renamed manifest endpoint requires Name")
		}
		if m.Render != nil {
			panic("rungrad: renamed manifest endpoint does not accept Render")
		}
		if strings.ContainsAny(m.Name, " \t\r\n") {
			panic(fmt.Sprintf("rungrad: renamed manifest endpoint %q must be a single command token", m.Name))
		}
		if m.Name == manifestCommandName {
			panic(fmt.Sprintf("rungrad: renamed manifest endpoint cannot use default name %q", manifestCommandName))
		}
		if a.reservedCommandName(m.Name) {
			panic(fmt.Sprintf("rungrad: renamed manifest endpoint %q collides with a reserved command name", m.Name))
		}
		if a.topLevelCommandExists(m.Name) {
			panic(fmt.Sprintf("rungrad: renamed manifest endpoint %q collides with an existing top-level command", m.Name))
		}
		return m.Name, nil
	case ManifestEndpointHostRendered:
		if m.Name != "" {
			panic("rungrad: host-rendered manifest endpoint does not accept Name")
		}
		if m.Render == nil {
			panic("rungrad: host-rendered manifest endpoint requires Render")
		}
		return manifestCommandName, m.Render
	default:
		panic(fmt.Sprintf("rungrad: invalid manifest endpoint mode %q", m.Mode))
	}
}

// topLevelCommandExists is defensive for callers that add root commands before
// endpoint validation in the future; normal AddCommand collisions are also
// caught later by reservedCommandName.
func (a *App) topLevelCommandExists(name string) bool {
	for _, cmd := range a.root.Commands() {
		if cmd.Name() == name {
			return true
		}
	}
	return false
}
