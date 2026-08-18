package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func testLookup(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
}

func baseResolution(t *testing.T, cfg Config, env map[string]string) Resolution {
	t.Helper()
	dir := t.TempDir()
	return Resolution{
		Tool:           "rgres",
		DefaultProfile: "fallback",
		LookupEnv:      testLookup(env),
		UserConfigDir:  func() (string, error) { return dir, nil },
		LoadConfig:     func(string) (Config, error) { return cfg, nil },
		ProfileEnvVar:  "RGRES_PROFILE",
		ConfigEnvVar:   "RGRES_CONFIG",
		AuthFileEnvVar: "RGRES_AUTH_FILE",
	}
}

func TestResolveProfilePrecedence(t *testing.T) {
	cfg := Config{Version: 1, CurrentProfile: "from-config"}
	tests := []struct {
		name    string
		mutate  func(*Resolution)
		want    string
		source  ProfileSource
		wantErr bool
	}{
		{
			name: "flag wins",
			mutate: func(r *Resolution) {
				r.Flags.Profile = OverrideString{Value: "from-flag", Set: true}
			},
			want:   "from-flag",
			source: ProfileFlag,
		},
		{
			name: "env wins over config",
			mutate: func(r *Resolution) {
				r.LookupEnv = testLookup(map[string]string{"RGRES_PROFILE": "from-env"})
			},
			want:   "from-env",
			source: ProfileEnv,
		},
		{name: "config wins over default", want: "from-config", source: ProfileConfig},
		{
			name: "default profile",
			mutate: func(r *Resolution) {
				r.LoadConfig = func(string) (Config, error) { return Config{Version: 1}, nil }
			},
			want:   "fallback",
			source: ProfileDefault,
		},
		{
			name: "builtin default",
			mutate: func(r *Resolution) {
				r.DefaultProfile = ""
				r.LoadConfig = func(string) (Config, error) { return Config{Version: 1}, nil }
			},
			want:   "default",
			source: ProfileDefault,
		},
		{
			name: "blank flag is invalid",
			mutate: func(r *Resolution) {
				r.Flags.Profile = OverrideString{Set: true}
				r.LookupEnv = testLookup(map[string]string{"RGRES_PROFILE": "from-env"})
			},
			source:  ProfileFlag,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := baseResolution(t, cfg, nil)
			if tt.mutate != nil {
				tt.mutate(&r)
			}
			got, err := Resolve(r, nil)
			if tt.wantErr {
				var cfgErr *Error
				if !errors.As(err, &cfgErr) || cfgErr.Kind != ErrKindInvalidProfile || cfgErr.ExitCode() != 1 {
					t.Fatalf("error = %#v, want invalid profile Error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got.Profile != tt.want || got.ProfileSource != tt.source {
				t.Fatalf("profile/source = %q/%v, want %q/%v", got.Profile, got.ProfileSource, tt.want, tt.source)
			}
		})
	}
}

func TestResolveRejectsInvalidProfile(t *testing.T) {
	r := baseResolution(t, Config{Version: 1}, nil)
	r.Flags.Profile = OverrideString{Value: "bad/name", Set: true}
	_, err := Resolve(r, nil)
	var cfgErr *Error
	if !errors.As(err, &cfgErr) || cfgErr.Kind != ErrKindInvalidProfile || cfgErr.Profile != "bad/name" {
		t.Fatalf("error = %#v, want invalid profile", err)
	}
	if cfgErr.ExitCode() != 1 {
		t.Fatalf("ExitCode = %d, want 1", cfgErr.ExitCode())
	}
}

func TestResolveConfigPathPrecedenceAndErrors(t *testing.T) {
	tmp := t.TempDir()
	tests := []struct {
		name    string
		mutate  func(*Resolution)
		want    string
		wantErr bool
	}{
		{
			name: "flag wins",
			mutate: func(r *Resolution) {
				r.Flags.Config = OverrideString{Value: filepath.Join(tmp, "flag.yaml"), Set: true}
			},
			want: filepath.Join(tmp, "flag.yaml"),
		},
		{
			name: "env wins",
			mutate: func(r *Resolution) {
				r.LookupEnv = testLookup(map[string]string{"RGRES_CONFIG": filepath.Join(tmp, "env.yaml")})
			},
			want: filepath.Join(tmp, "env.yaml"),
		},
		{
			name: "default dir",
			want: filepath.Join(tmp, "rgres", "config.yaml"),
		},
		{
			name: "blank flag fails",
			mutate: func(r *Resolution) {
				r.Flags.Config = OverrideString{Set: true}
			},
			wantErr: true,
		},
		{
			name: "user config dir error",
			mutate: func(r *Resolution) {
				r.UserConfigDir = func() (string, error) { return "", errors.New("home failed") }
			},
			wantErr: true,
		},
		{
			name: "empty user config dir",
			mutate: func(r *Resolution) {
				r.UserConfigDir = func() (string, error) { return "", nil }
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resolution{
				Tool:          "rgres",
				LookupEnv:     testLookup(nil),
				UserConfigDir: func() (string, error) { return tmp, nil },
				LoadConfig:    func(string) (Config, error) { return Config{Version: 1}, nil },
				ConfigEnvVar:  "RGRES_CONFIG",
			}
			if tt.mutate != nil {
				tt.mutate(&r)
			}
			got, err := Resolve(r, nil)
			if tt.wantErr {
				var cfgErr *Error
				if !errors.As(err, &cfgErr) || cfgErr.Kind != ErrKindMalformedConfig || cfgErr.ExitCode() != 1 {
					t.Fatalf("error = %#v, want malformed config", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got.ConfigPath != tt.want {
				t.Fatalf("ConfigPath = %q, want %q", got.ConfigPath, tt.want)
			}
		})
	}
}

func TestResolveAuthFilePathPrecedenceAndBlank(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "cfg", "config.yaml")
	tests := []struct {
		name    string
		mutate  func(*Resolution)
		want    string
		wantErr bool
	}{
		{
			name: "flag wins",
			mutate: func(r *Resolution) {
				r.Flags.AuthFile = OverrideString{Value: filepath.Join(tmp, "flag.json"), Set: true}
			},
			want: filepath.Join(tmp, "flag.json"),
		},
		{
			name: "env wins",
			mutate: func(r *Resolution) {
				r.LookupEnv = testLookup(map[string]string{"RGRES_AUTH_FILE": filepath.Join(tmp, "env.json")})
			},
			want: filepath.Join(tmp, "env.json"),
		},
		{
			name: "beside config",
			want: filepath.Join(tmp, "cfg", "credentials.json"),
		},
		{
			name: "blank flag fails",
			mutate: func(r *Resolution) {
				r.Flags.AuthFile = OverrideString{Set: true}
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := baseResolution(t, Config{Version: 1}, nil)
			r.Flags.Config = OverrideString{Value: cfgPath, Set: true}
			if tt.mutate != nil {
				tt.mutate(&r)
			}
			got, err := Resolve(r, nil)
			if tt.wantErr {
				var cfgErr *Error
				if !errors.As(err, &cfgErr) || cfgErr.Kind != ErrKindMalformedConfig {
					t.Fatalf("error = %#v, want malformed config", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got.AuthFilePath != tt.want {
				t.Fatalf("AuthFilePath = %q, want %q", got.AuthFilePath, tt.want)
			}
		})
	}
}

func TestResolveLoadConfigInjectionAndMissingFile(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	called := false
	r := Resolution{
		Tool: "rgres",
		Flags: FlagOverrides{
			Config: OverrideString{Value: cfgPath, Set: true},
		},
		LoadConfig: func(path string) (Config, error) {
			called = true
			if path != cfgPath {
				t.Fatalf("LoadConfig path = %q, want %q", path, cfgPath)
			}
			return Config{}, os.ErrNotExist
		},
	}
	got, err := Resolve(r, nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !called {
		t.Fatal("LoadConfig hook was not called")
	}
	if got.Config.Version != 1 {
		t.Fatalf("Config.Version = %d, want 1", got.Config.Version)
	}

	r.LoadConfig = nil
	got, err = Resolve(r, nil)
	if err != nil {
		t.Fatalf("default loader missing file error = %v", err)
	}
	if got.Config.Version != 1 {
		t.Fatalf("default loader Version = %d, want 1", got.Config.Version)
	}
}

func TestResolveMalformedConfigFile(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("profiles:\n  bad: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Resolution{
		Tool:  "rgres",
		Flags: FlagOverrides{Config: OverrideString{Value: cfgPath, Set: true}},
	}
	_, err := Resolve(r, nil)
	var cfgErr *Error
	if !errors.As(err, &cfgErr) || cfgErr.Kind != ErrKindMalformedConfig || cfgErr.Path != cfgPath || cfgErr.ExitCode() != 1 {
		t.Fatalf("error = %#v, want malformed config for path", err)
	}
}

func TestResolveServicePrecedence(t *testing.T) {
	cfg := Config{
		Version: 1,
		Profiles: map[string]Profile{
			"fallback": {
				BaseURL:  "https://profile-base",
				Services: map[string]string{"region": "eu"},
				Defaults: map[string]string{"legacy": "profile-legacy"},
			},
		},
		Services: map[string]string{"region": "global-eu", "global": "global-value"},
		Defaults: map[string]string{"legacy": "global-legacy"},
	}
	specs := []ServiceSpec{
		{Name: "api", EnvVar: "RGRES_BASE_URL", ConfigKey: "base_url", Default: "https://builtin"},
		{Name: "region", EnvVar: "RGRES_REGION", ConfigKey: "region", Default: "us"},
		{Name: "global", ConfigKey: "global", Default: "builtin-global"},
		{Name: "legacy", ConfigKey: "legacy", Default: "builtin-legacy"},
		{Name: "plain", Default: "builtin-plain"},
	}
	tests := []struct {
		name   string
		mutate func(*Resolution)
		want   map[string]ResolvedService
	}{
		{
			name: "flag wins",
			mutate: func(r *Resolution) {
				r.Flags.Services = map[string]OverrideString{"api": {Value: "https://flag", Set: true}}
			},
			want: map[string]ResolvedService{
				"api": {Value: "https://flag", Source: SourceFlag},
			},
		},
		{
			name: "env wins",
			mutate: func(r *Resolution) {
				r.LookupEnv = testLookup(map[string]string{"RGRES_BASE_URL": "https://env"})
			},
			want: map[string]ResolvedService{
				"api": {Value: "https://env", Source: SourceEnv},
			},
		},
		{
			name: "profile and global and builtin",
			want: map[string]ResolvedService{
				"api":    {Value: "https://profile-base", Source: SourceProfile},
				"region": {Value: "eu", Source: SourceProfile},
				"global": {Value: "global-value", Source: SourceDefaults},
				"legacy": {Value: "profile-legacy", Source: SourceProfile},
				"plain":  {Value: "builtin-plain", Source: SourceBuiltin},
			},
		},
		{
			name: "global defaults compatibility",
			mutate: func(r *Resolution) {
				r.DefaultProfile = "missing"
			},
			want: map[string]ResolvedService{
				"api":    {Value: "https://builtin", Source: SourceBuiltin},
				"region": {Value: "global-eu", Source: SourceDefaults},
				"global": {Value: "global-value", Source: SourceDefaults},
				"legacy": {Value: "global-legacy", Source: SourceDefaults},
				"plain":  {Value: "builtin-plain", Source: SourceBuiltin},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := baseResolution(t, cfg, nil)
			if tt.mutate != nil {
				tt.mutate(&r)
			}
			got, err := Resolve(r, specs)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			for name, want := range tt.want {
				if got.Services[name] != want {
					t.Fatalf("service %q = %+v, want %+v", name, got.Services[name], want)
				}
			}
		})
	}
}

func TestResolveServiceValidationError(t *testing.T) {
	r := baseResolution(t, Config{Version: 1}, nil)
	_, err := Resolve(r, []ServiceSpec{{
		Name:    "api",
		Default: "not-a-url",
		Validate: func(string) error {
			return errors.New("invalid url")
		},
	}})
	var cfgErr *Error
	if !errors.As(err, &cfgErr) || cfgErr.Kind != ErrKindInvalidService || cfgErr.Service != "api" || cfgErr.ExitCode() != 1 {
		t.Fatalf("error = %#v, want invalid service", err)
	}
}

func TestStoreCredentialsOverrideRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	s := Store{Tool: "rgtest", Override: filepath.Join(t.TempDir(), "config.yaml"), Credentials: path}
	if got, err := s.CredentialsPath(); err != nil || got != path {
		t.Fatalf("CredentialsPath = %q, %v; want %q", got, err, path)
	}
	if err := s.SaveCredential("work", Credential{Token: "secret"}); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != fs.FileMode(0o600) {
		t.Fatalf("credentials perm = %o, want 600", perm)
	}
	cred, err := s.LoadCredential("work")
	if err != nil {
		t.Fatalf("LoadCredential: %v", err)
	}
	if cred.Token != "secret" {
		t.Fatalf("token = %q, want secret", cred.Token)
	}
}

func TestLoadTokenWithSource(t *testing.T) {
	s := Store{Tool: "rgtest", Override: filepath.Join(t.TempDir(), "config.yaml")}
	if err := s.SaveCredential("default", Credential{Token: "from-file"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RGTEST_TOKEN", "from-env")
	token, source, err := s.LoadTokenWithSource("RGTEST_TOKEN", "default")
	if err != nil || token != "from-env" || source != "env" {
		t.Fatalf("env token/source/err = %q/%q/%v", token, source, err)
	}
	t.Setenv("RGTEST_TOKEN", "")
	token, source, err = s.LoadTokenWithSource("RGTEST_TOKEN", "default")
	if err != nil || token != "from-file" || source != "file" {
		t.Fatalf("file token/source/err = %q/%q/%v", token, source, err)
	}
	_, source, err = s.LoadTokenWithSource("UNSET_TOKEN", "missing")
	if !errors.Is(err, ErrMissingCredential) || source != "" {
		t.Fatalf("missing source/err = %q/%v, want empty ErrMissingCredential", source, err)
	}
}
