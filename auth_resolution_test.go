package rungrad_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/config"
	"github.com/vincentsch/rungrad/testutil"
)

type resolverFunc func(*rungrad.AuthContext) (rungrad.Credential, error)

func (fn resolverFunc) ResolveCredential(ac *rungrad.AuthContext) (rungrad.Credential, error) {
	return fn(ac)
}

type resolutionPayload struct {
	ID string
}

func resolutionTestApp(auth rungrad.CredentialResolver, mutate func(*rungrad.AppConfig)) *rungrad.App {
	cfg := rungrad.AppConfig{
		Name:   "rgres",
		Short:  "resolution test",
		EnvVar: "RGRES_TOKEN",
		Resolution: &rungrad.ResolutionConfig{
			Profile:  true,
			AuthFile: true,
			Services: []rungrad.Service{
				{Name: "api", Flag: "base-url", EnvVar: "RGRES_BASE_URL", ConfigKey: "base_url", Default: "https://api.default", Usage: "API base URL"},
				{Name: "region", EnvVar: "RGRES_REGION", ConfigKey: "region", Default: "us"},
			},
		},
		Auth: auth,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	app := rungrad.New(cfg)
	app.AddCommand(
		&rungrad.Command{
			Use:   "show",
			Short: "show resolution",
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				api, _ := f.Service("api")
				region, _ := f.Service("region")
				_, resolved := f.Resolved()
				return f.WriteResult(map[string]any{
					"resolved":         resolved,
					"profile":          f.Profile(),
					"config_path":      f.ConfigPath(),
					"auth_file_path":   f.AuthFilePath(),
					"api":              api.Value,
					"api_source":       api.Source.String(),
					"region":           region.Value,
					"region_source":    region.Source.String(),
					"credential_empty": f.Credential() == (rungrad.Credential{}),
				}, func(w io.Writer) {})
			},
		},
		&rungrad.Command{
			Use:          "whoami",
			Short:        "show auth",
			RequiresAuth: true,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				cred := f.Credential()
				payload, extraOK := cred.Extra.(resolutionPayload)
				return f.WriteResult(map[string]any{
					"token_present":    f.Token != "",
					"token":            f.Token,
					"profile":          f.Profile(),
					"source":           cred.Source,
					"display":          cred.Display,
					"auth_file_path":   f.AuthFilePath(),
					"extra_typed":      extraOK,
					"extra_id":         payload.ID,
					"credential_token": cred.Token,
				}, func(w io.Writer) {})
			},
		},
		&rungrad.Command{
			Use:   "browser",
			Short: "open browser",
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				if err := f.OpenBrowser(cmd.Context(), "https://login.example.test/start"); err != nil {
					return err
				}
				return f.WriteResult(map[string]string{"opened": "yes"}, func(w io.Writer) {})
			},
		},
	)
	return app
}

func writeResolutionConfig(t *testing.T, path string, cfg config.Config) {
	t.Helper()
	if err := (config.Store{Tool: "rgres", Override: path}).SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
}

func resolutionEnv(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := values[name]
		return v, ok
	}
}

func decodeMap(t *testing.T, r testutil.Result) map[string]any {
	t.Helper()
	if r.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit = %d, stderr=%q stdout=%q", r.Exit, r.Stderr, r.Stdout)
	}
	var out map[string]any
	if err := r.JSON(&out); err != nil {
		t.Fatalf("JSON: %v\n%s", err, r.Stdout)
	}
	return out
}

func TestResolutionServiceAndProfilePrecedence(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeResolutionConfig(t, cfgPath, config.Config{
		Version:        1,
		CurrentProfile: "work",
		Profiles: map[string]config.Profile{
			"work": {
				BaseURL:  "https://profile.api",
				Services: map[string]string{"region": "eu"},
			},
		},
		Services: map[string]string{"region": "global"},
	})

	app := resolutionTestApp(nil, nil)
	res := testutil.RunWith(app, testutil.Options{
		LookupEnv: resolutionEnv(map[string]string{
			"RGRES_PROFILE":  "envprof",
			"RGRES_BASE_URL": "https://env.api",
			"RGRES_REGION":   "ap",
		}),
	}, "show", "--config", cfgPath, "--profile", "flagprof", "--base-url", "https://flag.api", "--json")
	out := decodeMap(t, res)
	if out["profile"] != "flagprof" || out["api"] != "https://flag.api" || out["api_source"] != "flag" {
		t.Fatalf("flag precedence output = %#v", out)
	}
	if out["region"] != "ap" || out["region_source"] != "env" {
		t.Fatalf("env region output = %#v", out)
	}

	app = resolutionTestApp(nil, nil)
	res = testutil.RunWith(app, testutil.Options{
		LookupEnv: resolutionEnv(map[string]string{
			"RGRES_PROFILE":  "envprof",
			"RGRES_BASE_URL": "https://env.api",
		}),
	}, "show", "--config", cfgPath, "--json")
	out = decodeMap(t, res)
	if out["profile"] != "envprof" || out["api"] != "https://env.api" || out["api_source"] != "env" {
		t.Fatalf("env precedence output = %#v", out)
	}
	if out["region"] != "global" || out["region_source"] != "defaults" {
		t.Fatalf("global region output = %#v", out)
	}

	app = resolutionTestApp(nil, nil)
	res = testutil.Run(app, "show", "--config", cfgPath, "--json")
	out = decodeMap(t, res)
	if out["profile"] != "work" || out["api"] != "https://profile.api" || out["api_source"] != "profile" {
		t.Fatalf("profile config output = %#v", out)
	}
	if out["region"] != "eu" || out["region_source"] != "profile" {
		t.Fatalf("profile region output = %#v", out)
	}

	emptyPath := filepath.Join(dir, "empty.yaml")
	writeResolutionConfig(t, emptyPath, config.Config{Version: 1})
	app = resolutionTestApp(nil, nil)
	res = testutil.Run(app, "show", "--config", emptyPath, "--json")
	out = decodeMap(t, res)
	if out["profile"] != "default" || out["api"] != "https://api.default" || out["api_source"] != "builtin" {
		t.Fatalf("builtin output = %#v", out)
	}
	if out["region"] != "us" || out["region_source"] != "builtin" {
		t.Fatalf("builtin region output = %#v", out)
	}
}

func TestFlaglessServiceResolvesEnvConfigAndBuiltin(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeResolutionConfig(t, cfgPath, config.Config{
		Version:        1,
		CurrentProfile: "work",
		Profiles: map[string]config.Profile{
			"work": {Services: map[string]string{"region": "profile-region"}},
		},
		Services: map[string]string{"region": "global-region"},
	})

	app := resolutionTestApp(nil, nil)
	out := decodeMap(t, testutil.RunWith(app, testutil.Options{
		LookupEnv: resolutionEnv(map[string]string{"RGRES_REGION": "env-region"}),
	}, "show", "--config", cfgPath, "--json"))
	if out["region"] != "env-region" || out["region_source"] != "env" {
		t.Fatalf("env region = %#v", out)
	}

	app = resolutionTestApp(nil, nil)
	out = decodeMap(t, testutil.Run(app, "show", "--config", cfgPath, "--json"))
	if out["region"] != "profile-region" || out["region_source"] != "profile" {
		t.Fatalf("profile region = %#v", out)
	}

	globalPath := filepath.Join(dir, "global.yaml")
	writeResolutionConfig(t, globalPath, config.Config{Version: 1, Services: map[string]string{"region": "global-region"}})
	app = resolutionTestApp(nil, nil)
	out = decodeMap(t, testutil.Run(app, "show", "--config", globalPath, "--json"))
	if out["region"] != "global-region" || out["region_source"] != "defaults" {
		t.Fatalf("global region = %#v", out)
	}

	emptyPath := filepath.Join(dir, "empty.yaml")
	writeResolutionConfig(t, emptyPath, config.Config{Version: 1})
	app = resolutionTestApp(nil, nil)
	out = decodeMap(t, testutil.Run(app, "show", "--config", emptyPath, "--json"))
	if out["region"] != "us" || out["region_source"] != "builtin" {
		t.Fatalf("builtin region = %#v", out)
	}
}

func TestResolutionConfigLoaderHook(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "product.json")
	var calledPath string
	app := resolutionTestApp(nil, func(cfg *rungrad.AppConfig) {
		cfg.Resolution.LoadConfig = func(path string) (config.Config, error) {
			calledPath = path
			return config.Config{
				Version:        1,
				CurrentProfile: "work",
				Profiles: map[string]config.Profile{
					"work": {BaseURL: "https://normalized.api"},
				},
			}, nil
		}
	})
	out := decodeMap(t, testutil.Run(app, "show", "--config", cfgPath, "--json"))
	if calledPath != cfgPath {
		t.Fatalf("LoadConfig path = %q, want %q", calledPath, cfgPath)
	}
	if out["api"] != "https://normalized.api" || out["api_source"] != "profile" {
		t.Fatalf("loader output = %#v", out)
	}
}

func TestProfileValidationBeforeAuth(t *testing.T) {
	called := false
	app := resolutionTestApp(resolverFunc(func(*rungrad.AuthContext) (rungrad.Credential, error) {
		called = true
		return rungrad.Credential{}, nil
	}), nil)
	res := testutil.Run(app, "whoami", "--profile", "bad/name")
	if res.Exit != rungrad.ExitUsage {
		t.Fatalf("exit = %d, stderr=%q", res.Exit, res.Stderr)
	}
	if called {
		t.Fatal("credential resolver was called after invalid profile")
	}
}

func TestDefaultResolverEnvFileAndMissingCredential(t *testing.T) {
	app := resolutionTestApp(nil, nil)
	out := decodeMap(t, testutil.RunWith(app, testutil.Options{
		LookupEnv: resolutionEnv(map[string]string{"RGRES_TOKEN": "env-secret-token"}),
	}, "whoami", "--json"))
	if out["token_present"] != true || out["source"] != "env" || out["profile"] != "default" {
		t.Fatalf("env auth output = %#v", out)
	}
	testutil.AssertRedacted(t, testutil.Result{Stdout: fmt.Sprint(out), Stderr: ""}, "env-secret-token")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeResolutionConfig(t, cfgPath, config.Config{Version: 1})
	if err := (config.Store{Tool: "rgres", Override: cfgPath}).SaveCredential("default", config.Credential{Token: "file-secret-token", DisplayName: "stored"}); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	app = resolutionTestApp(nil, nil)
	out = decodeMap(t, testutil.Run(app, "whoami", "--config", cfgPath, "--json"))
	if out["source"] != "file" || out["display"] != "stored" || out["token_present"] != true {
		t.Fatalf("file auth output = %#v", out)
	}

	app = resolutionTestApp(nil, nil)
	res := testutil.Run(app, "whoami", "--config", filepath.Join(t.TempDir(), "missing.yaml"), "--json")
	if res.Exit != rungrad.ExitAuth {
		t.Fatalf("missing credential exit = %d, stderr=%q", res.Exit, res.Stderr)
	}
}

func TestDefaultResolverMalformedCredentialsExitUsage(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "corrupt-auth.json")
	if err := os.WriteFile(authPath, []byte(`{"version":1,"entries":`), 0o600); err != nil {
		t.Fatal(err)
	}
	app := resolutionTestApp(nil, nil)
	res := testutil.RunWith(app, testutil.Options{
		UserConfigDir: func() (string, error) { return dir, nil },
	}, "whoami", "--auth-file", authPath, "--json")
	if res.Exit != rungrad.ExitUsage {
		t.Fatalf("exit = %d, want usage; stderr=%q", res.Exit, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "malformed credentials") {
		t.Fatalf("stderr missing malformed credential context: %q", res.Stderr)
	}
}

func TestAuthFileOverrideAndDefaultPathInjection(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "custom-auth.json")
	if err := (config.Store{Tool: "rgres", Override: filepath.Join(dir, "unused.yaml"), Credentials: authPath}).SaveCredential("default", config.Credential{Token: "custom-auth-token"}); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	app := resolutionTestApp(nil, nil)
	out := decodeMap(t, testutil.Run(app, "whoami", "--auth-file", authPath, "--json"))
	if out["source"] != "file" || out["auth_file_path"] != authPath {
		t.Fatalf("auth-file output = %#v", out)
	}

	userConfig := t.TempDir()
	defaultCfg := filepath.Join(userConfig, "rgres", "config.yaml")
	writeResolutionConfig(t, defaultCfg, config.Config{Version: 1})
	defaultAuth := filepath.Join(userConfig, "rgres", "credentials.json")
	if err := (config.Store{Tool: "rgres", Override: defaultCfg}).SaveCredential("default", config.Credential{Token: "default-path-token"}); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	app = resolutionTestApp(nil, nil)
	out = decodeMap(t, testutil.RunWith(app, testutil.Options{
		UserConfigDir: func() (string, error) { return userConfig, nil },
	}, "whoami", "--json"))
	if out["auth_file_path"] != defaultAuth || out["source"] != "file" {
		t.Fatalf("default path output = %#v", out)
	}
}

func TestBlankPathOverridesFailBeforeAuth(t *testing.T) {
	for _, args := range [][]string{
		{"whoami", "--config", ""},
		{"whoami", "--auth-file", ""},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			called := false
			app := resolutionTestApp(resolverFunc(func(*rungrad.AuthContext) (rungrad.Credential, error) {
				called = true
				return rungrad.Credential{Token: "secret"}, nil
			}), nil)
			res := testutil.Run(app, args...)
			if res.Exit != rungrad.ExitUsage {
				t.Fatalf("exit = %d, stderr=%q", res.Exit, res.Stderr)
			}
			if called {
				t.Fatal("auth resolver called after blank path")
			}
		})
	}
}

func TestCustomCredentialResolverAndRedaction(t *testing.T) {
	primary := "primary-secret-token"
	extra := "extra-secret-token"
	app := resolutionTestApp(resolverFunc(func(ac *rungrad.AuthContext) (rungrad.Credential, error) {
		if ac.Profile != "custom" {
			return rungrad.Credential{}, fmt.Errorf("profile = %q", ac.Profile)
		}
		if _, ok := ac.Service("api"); !ok {
			return rungrad.Credential{}, errors.New("missing api service")
		}
		ac.RegisterSecret(extra)
		return rungrad.Credential{
			Token:   primary,
			Profile: ac.Profile,
			Source:  "custom",
			Extra:   resolutionPayload{ID: extra},
		}, nil
	}), nil)
	res := testutil.Run(app, "whoami", "--profile", "custom", "--json")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit = %d, stderr=%q", res.Exit, res.Stderr)
	}
	testutil.AssertRedacted(t, res, primary, extra)
	if !strings.Contains(res.Stdout, `"source": "custom"`) || !strings.Contains(res.Stdout, `"extra_typed": true`) {
		t.Fatalf("custom auth stdout = %s", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "[REDACTED]") {
		t.Fatalf("stdout did not show redacted token: %s", res.Stdout)
	}
}

func TestResolverErrorExitMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"missing credential", config.ErrMissingCredential, rungrad.ExitAuth},
		{"config error", config.Error{Kind: config.ErrKindMalformedConfig, Detail: "bad config"}, rungrad.ExitUsage},
		{"rungrad error", rungrad.NewError(rungrad.ExitForbidden, "forbidden"), rungrad.ExitForbidden},
		{"plain error", errors.New("plain failure"), rungrad.ExitAPI},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := resolutionTestApp(resolverFunc(func(*rungrad.AuthContext) (rungrad.Credential, error) {
				return rungrad.Credential{}, tt.err
			}), nil)
			res := testutil.Run(app, "whoami")
			if res.Exit != tt.want {
				t.Fatalf("exit = %d, want %d stderr=%q", res.Exit, tt.want, res.Stderr)
			}
		})
	}
}

func TestMissingRequiredFlagStillWinsOverAuth(t *testing.T) {
	called := false
	app := rungrad.New(rungrad.AppConfig{
		Name:   "rgres",
		Short:  "resolution test",
		EnvVar: "RGRES_TOKEN",
		Resolution: &rungrad.ResolutionConfig{
			Profile: true,
		},
		Auth: resolverFunc(func(*rungrad.AuthContext) (rungrad.Credential, error) {
			called = true
			return rungrad.Credential{Token: "secret"}, nil
		}),
	})
	app.AddCommand(&rungrad.Command{
		Use:          "needs",
		RequiresAuth: true,
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().String("name", "", "name")
			if err := cmd.MarkFlagRequired("name"); err != nil {
				t.Fatal(err)
			}
		},
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil },
	})
	res := testutil.Run(app, "needs")
	if res.Exit != rungrad.ExitUsage {
		t.Fatalf("exit = %d, stderr=%q", res.Exit, res.Stderr)
	}
	if called {
		t.Fatal("auth resolver called before required flag validation")
	}
}

func TestResolutionFlagsResetBetweenRuns(t *testing.T) {
	app := resolutionTestApp(nil, nil)
	first := decodeMap(t, testutil.Run(app, "show", "--profile", "work", "--json"))
	second := decodeMap(t, testutil.Run(app, "show", "--json"))
	if first["profile"] != "work" {
		t.Fatalf("first profile = %#v", first)
	}
	if second["profile"] != "default" {
		t.Fatalf("second profile = %#v", second)
	}
}

func TestBrowserOpenerInjection(t *testing.T) {
	var opened []string
	app := resolutionTestApp(nil, nil)
	res := testutil.RunWith(app, testutil.Options{
		BrowserOpener: func(ctx context.Context, url string) error {
			opened = append(opened, url)
			return nil
		},
	}, "browser", "--json")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit = %d, stderr=%q", res.Exit, res.Stderr)
	}
	if len(opened) != 1 || opened[0] != "https://login.example.test/start" {
		t.Fatalf("opened = %v", opened)
	}
}

func TestResolutionConfigPanics(t *testing.T) {
	tests := []struct {
		name string
		rc   *rungrad.ResolutionConfig
		want string
	}{
		{
			name: "duplicate service name",
			rc: &rungrad.ResolutionConfig{Services: []rungrad.Service{
				{Name: "api"},
				{Name: "api"},
			}},
			want: "api",
		},
		{
			name: "service flag collides",
			rc: &rungrad.ResolutionConfig{Services: []rungrad.Service{
				{Name: "api", Flag: "config"},
			}},
			want: "config",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("New did not panic")
				}
				if !strings.Contains(fmt.Sprint(r), tt.want) {
					t.Fatalf("panic = %v, want mentioning %q", r, tt.want)
				}
			}()
			rungrad.New(rungrad.AppConfig{Name: "rgres", Short: "panic", Resolution: tt.rc})
		})
	}
}
