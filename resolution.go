package rungrad

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vincentsch/rungrad/config"
)

// ResolutionConfig opts an app into profile/auth-file/service resolution and
// registers the corresponding opt-in global flags.
type ResolutionConfig struct {
	Profile        bool   // register --profile and enable the profile env tier
	ProfileEnvVar  string // default <TOOL>_PROFILE
	AuthFile       bool   // register --auth-file and enable the auth-file env tier
	AuthFileEnvVar string // default <TOOL>_AUTH_FILE
	ConfigEnvVar   string // default <TOOL>_CONFIG
	Services       []Service
	LoadConfig     func(path string) (config.Config, error) // nil uses rungrad config.yaml
}

// Service declares one named endpoint resolved with flag > env >
// profile-config > global-config-default > built-in precedence.
type Service struct {
	Name      string             // stable key used with Factory.Service
	Flag      string             // optional global flag name
	EnvVar    string             // optional environment variable
	ConfigKey string             // optional profile/global config key
	Default   string             // built-in fallback
	Usage     string             // flag help text
	Validate  func(string) error // optional validation of the resolved value
}

func serviceSpecs(rc *ResolutionConfig) []config.ServiceSpec {
	if rc == nil || len(rc.Services) == 0 {
		return nil
	}
	out := make([]config.ServiceSpec, 0, len(rc.Services))
	for _, svc := range rc.Services {
		out = append(out, config.ServiceSpec{
			Name:      svc.Name,
			EnvVar:    svc.EnvVar,
			ConfigKey: svc.ConfigKey,
			Default:   svc.Default,
			Validate:  svc.Validate,
		})
	}
	return out
}

func (a *App) registerServiceFlags(fs *pflag.FlagSet) {
	if a.cfg.Resolution == nil {
		return
	}
	for _, svc := range a.cfg.Resolution.Services {
		if svc.Flag == "" {
			continue
		}
		// Service flags are keyed by Service.Name, not by visible flag name, so
		// renamed host bindings and rungrad-owned flags resolve through one path.
		o := &serviceOverride{def: svc.Default, value: svc.Default}
		a.serviceOverrides[svc.Name] = o
		fs.Var(serviceFlagValue{o}, svc.Flag, svc.Usage)
	}
}

// validateResolution catches mistakes that would otherwise produce confusing
// runtime behavior or low-level pflag panics during app construction.
func validateResolution(rc *ResolutionConfig) {
	if rc == nil {
		return
	}
	names := map[string]bool{}
	global := frameworkGlobalFlagNames()
	for _, svc := range rc.Services {
		if svc.Name == "" {
			panic("rungrad service name cannot be empty")
		}
		if names[svc.Name] {
			panic(fmt.Sprintf("rungrad service %q registered more than once", svc.Name))
		}
		names[svc.Name] = true
		if svc.Flag != "" && global[svc.Flag] {
			panic(fmt.Sprintf("rungrad service flag %q collides with a framework global flag", svc.Flag))
		}
	}
}

func frameworkGlobalFlagNames() map[string]bool {
	return map[string]bool{
		"json":         true,
		"dry-run":      true,
		"no-prompt":    true,
		"quiet":        true,
		"config":       true,
		"profile":      true,
		"auth-file":    true,
		"plain":        true,
		"jq":           true,
		"template":     true,
		"include-meta": true,
		"no-color":     true,
		"no-ansi":      true,
		"no-pager":     true,
	}
}

// buildResolution converts already-parsed Cobra flags and app hooks into the
// pure config resolver input. Service flags are considered only when Cobra says
// the user changed them; their registered defaults must not mask env/config
// values in the precedence chain.
func (a *App) buildResolution(_ *cobra.Command) (config.Resolved, error) {
	rc := a.cfg.Resolution
	overrides := config.FlagOverrides{
		Config: config.OverrideString{
			Value: a.flags.Config,
			Set:   a.globalChanged("config"),
		},
		Services: map[string]config.OverrideString{},
	}
	if rc.Profile {
		overrides.Profile = config.OverrideString{Value: a.flags.Profile, Set: a.globalChanged("profile")}
	}
	if rc.AuthFile {
		overrides.AuthFile = config.OverrideString{Value: a.flags.AuthFile, Set: a.globalChanged("auth-file")}
	}
	for _, svc := range rc.Services {
		if svc.Flag == "" {
			continue
		}
		if o := a.serviceOverrides[svc.Name]; o != nil {
			overrides.Services[svc.Name] = config.OverrideString{
				Value: o.value,
				Set:   o.set,
			}
		}
	}

	profileEnv := ""
	if rc.Profile {
		profileEnv = rc.ProfileEnvVar
		if profileEnv == "" {
			profileEnv = toolEnvVar(a.cfg.Name, "PROFILE")
		}
	}
	authFileEnv := ""
	if rc.AuthFile {
		authFileEnv = rc.AuthFileEnvVar
		if authFileEnv == "" {
			authFileEnv = toolEnvVar(a.cfg.Name, "AUTH_FILE")
		}
	}
	configEnv := rc.ConfigEnvVar
	if configEnv == "" {
		configEnv = toolEnvVar(a.cfg.Name, "CONFIG")
	}

	return config.Resolve(config.Resolution{
		Tool:           a.cfg.Name,
		DefaultProfile: a.cfg.Profile,
		Flags:          overrides,
		LookupEnv:      a.factory.lookupEnv,
		UserConfigDir:  a.factory.userConfigDir,
		LoadConfig:     rc.LoadConfig,
		ProfileEnvVar:  profileEnv,
		ConfigEnvVar:   configEnv,
		AuthFileEnvVar: authFileEnv,
	}, serviceSpecs(rc))
}
