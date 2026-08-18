package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// OverrideString carries a flag value and whether the user set it explicitly.
// A flag set to the empty string still has Set=true, so an explicit blank wins
// over lower-precedence sources.
type OverrideString struct {
	Value string
	Set   bool
}

// FlagOverrides are resolution inputs sourced from command-line flags.
type FlagOverrides struct {
	Profile  OverrideString
	Config   OverrideString
	AuthFile OverrideString
	Services map[string]OverrideString
}

// ProfileSource reports which precedence tier selected the active profile.
type ProfileSource int

const (
	ProfileDefault ProfileSource = iota
	ProfileConfig
	ProfileEnv
	ProfileFlag
)

// SourceKind reports which precedence tier produced a resolved service value.
type SourceKind int

const (
	SourceBuiltin SourceKind = iota
	SourceDefaults
	SourceProfile
	SourceEnv
	SourceFlag
)

// ServiceSpec declares one named endpoint to resolve. Empty EnvVar/ConfigKey
// disables that tier; Validate runs on the resolved value when present.
type ServiceSpec struct {
	Name      string
	EnvVar    string
	ConfigKey string
	Default   string
	Validate  func(string) error
}

// Resolution carries injectable resolution inputs. Zero-value hooks fall back
// to process environment and os.UserConfigDir.
type Resolution struct {
	Tool           string
	DefaultProfile string
	Flags          FlagOverrides
	LookupEnv      func(string) (string, bool)
	UserConfigDir  func() (string, error)
	LoadConfig     func(path string) (Config, error)
	ProfileEnvVar  string
	ConfigEnvVar   string
	AuthFileEnvVar string
}

// ResolvedService is one endpoint's resolved value and origin tier.
type ResolvedService struct {
	Value  string
	Source SourceKind
}

// Resolved is the fully resolved runtime state.
type Resolved struct {
	Profile       string
	ProfileSource ProfileSource
	ConfigPath    string
	AuthFilePath  string
	Services      map[string]ResolvedService
	Config        Config
}

var profileNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Resolve computes paths, the active profile, and service values with
// flag > env > profile-config > global-config-default > built-in precedence.
func Resolve(r Resolution, specs []ServiceSpec) (Resolved, error) {
	configPath, err := resolveConfigPath(r)
	if err != nil {
		return Resolved{}, err
	}
	// Load config before selecting the profile because current_profile and
	// profile-scoped services are part of the resolution inputs.
	cfg, err := loadResolvedConfig(r, configPath)
	if err != nil {
		return Resolved{}, err
	}
	profile, profileSource, err := resolveProfile(r, cfg)
	if err != nil {
		return Resolved{}, err
	}
	authFilePath, err := resolveAuthFilePath(r, configPath)
	if err != nil {
		return Resolved{}, err
	}
	services, err := resolveServices(r, cfg, profile, specs)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{
		Profile:       profile,
		ProfileSource: profileSource,
		ConfigPath:    configPath,
		AuthFilePath:  authFilePath,
		Services:      services,
		Config:        cfg,
	}, nil
}

func resolveConfigPath(r Resolution) (string, error) {
	if r.Flags.Config.Set {
		if r.Flags.Config.Value == "" {
			return "", &Error{Kind: ErrKindMalformedConfig, Detail: "--config cannot be blank"}
		}
		return r.Flags.Config.Value, nil
	}
	if v, ok := lookupNonEmpty(r, r.ConfigEnvVar); ok {
		return v, nil
	}
	dir, err := defaultDir(r)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func defaultDir(r Resolution) (string, error) {
	userConfigDir := r.UserConfigDir
	if userConfigDir == nil {
		userConfigDir = os.UserConfigDir
	}
	base, err := userConfigDir()
	if err != nil {
		return "", &Error{Kind: ErrKindMalformedConfig, Detail: err.Error(), Err: err}
	}
	if base == "" {
		return "", &Error{Kind: ErrKindMalformedConfig, Detail: "user config directory is empty"}
	}
	return filepath.Join(base, r.Tool), nil
}

func loadResolvedConfig(r Resolution, path string) (Config, error) {
	load := r.LoadConfig
	if load == nil {
		load = func(path string) (Config, error) {
			return Store{Tool: r.Tool, Override: path}.LoadConfig()
		}
	}
	cfg, err := load(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{Version: 1}, nil
	}
	if err != nil {
		return Config{}, &Error{Kind: ErrKindMalformedConfig, Path: path, Err: err}
	}
	if cfg.Version == 0 {
		// Custom loaders may normalize product-owned config files that do not
		// carry rungrad's version field. Treat that as the current empty schema.
		cfg.Version = 1
	}
	return cfg, nil
}

func resolveProfile(r Resolution, cfg Config) (string, ProfileSource, error) {
	profile, source := "", ProfileDefault
	if r.Flags.Profile.Set {
		profile, source = r.Flags.Profile.Value, ProfileFlag
	} else if v, ok := lookupNonEmpty(r, r.ProfileEnvVar); ok {
		profile, source = v, ProfileEnv
	} else if cfg.CurrentProfile != "" {
		profile, source = cfg.CurrentProfile, ProfileConfig
	} else if r.DefaultProfile != "" {
		profile, source = r.DefaultProfile, ProfileDefault
	} else {
		profile, source = "default", ProfileDefault
	}
	if !validateProfileName(profile) {
		return "", source, &Error{Kind: ErrKindInvalidProfile, Profile: profile}
	}
	return profile, source, nil
}

func resolveAuthFilePath(r Resolution, configPath string) (string, error) {
	if r.Flags.AuthFile.Set {
		if r.Flags.AuthFile.Value == "" {
			return "", &Error{Kind: ErrKindMalformedConfig, Detail: "--auth-file cannot be blank"}
		}
		return r.Flags.AuthFile.Value, nil
	}
	if v, ok := lookupNonEmpty(r, r.AuthFileEnvVar); ok {
		return v, nil
	}
	return filepath.Join(filepath.Dir(configPath), "credentials.json"), nil
}

func resolveServices(r Resolution, cfg Config, profile string, specs []ServiceSpec) (map[string]ResolvedService, error) {
	out := make(map[string]ResolvedService, len(specs))
	for _, spec := range specs {
		value, source := resolveService(r, cfg, profile, spec)
		if spec.Validate != nil {
			if err := spec.Validate(value); err != nil {
				return nil, &Error{
					Kind:    ErrKindInvalidService,
					Service: spec.Name,
					Detail:  err.Error(),
					Err:     err,
				}
			}
		}
		out[spec.Name] = ResolvedService{Value: value, Source: source}
	}
	return out, nil
}

func resolveService(r Resolution, cfg Config, profile string, spec ServiceSpec) (string, SourceKind) {
	if override, ok := r.Flags.Services[spec.Name]; ok && override.Set {
		return override.Value, SourceFlag
	}
	if v, ok := lookupNonEmpty(r, spec.EnvVar); ok {
		return v, SourceEnv
	}
	if v, ok := profileConfigValue(cfg, profile, spec.ConfigKey); ok {
		return v, SourceProfile
	}
	if v, ok := globalConfigValue(cfg, spec.ConfigKey); ok {
		return v, SourceDefaults
	}
	return spec.Default, SourceBuiltin
}

func profileConfigValue(cfg Config, profile, key string) (string, bool) {
	if key == "" || cfg.Profiles == nil {
		return "", false
	}
	p, ok := cfg.Profiles[profile]
	if !ok {
		return "", false
	}
	if key == "base_url" && p.BaseURL != "" {
		return p.BaseURL, true
	}
	// Services are the first-class endpoint map. Defaults remains a
	// compatibility fallback so existing config files can still participate.
	if p.Services != nil {
		if v := p.Services[key]; v != "" {
			return v, true
		}
	}
	if p.Defaults != nil {
		if v := p.Defaults[key]; v != "" {
			return v, true
		}
	}
	return "", false
}

func globalConfigValue(cfg Config, key string) (string, bool) {
	if key == "" {
		return "", false
	}
	if cfg.Services != nil {
		if v := cfg.Services[key]; v != "" {
			return v, true
		}
	}
	if cfg.Defaults != nil {
		if v := cfg.Defaults[key]; v != "" {
			return v, true
		}
	}
	return "", false
}

func lookupNonEmpty(r Resolution, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	lookup := r.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	v, ok := lookup(name)
	return v, ok && v != ""
}

func validateProfileName(name string) bool {
	return profileNameRE.MatchString(name)
}

func (ps ProfileSource) String() string {
	switch ps {
	case ProfileDefault:
		return "default"
	case ProfileConfig:
		return "config"
	case ProfileEnv:
		return "env"
	case ProfileFlag:
		return "flag"
	default:
		return fmt.Sprintf("ProfileSource(%d)", ps)
	}
}

func (sk SourceKind) String() string {
	switch sk {
	case SourceBuiltin:
		return "builtin"
	case SourceDefaults:
		return "defaults"
	case SourceProfile:
		return "profile"
	case SourceEnv:
		return "env"
	case SourceFlag:
		return "flag"
	default:
		return fmt.Sprintf("SourceKind(%d)", sk)
	}
}
