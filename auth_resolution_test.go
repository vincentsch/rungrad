package rungrad_test

import (
	"context"
	"encoding/json"
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
	"github.com/vincentsch/rungrad/docsgen"
	"github.com/vincentsch/rungrad/testutil"
)

const privateAuthResolutionAnnotation = "rungrad.authResolution"

var _ = rungrad.Command{
	Use:          "legacy",
	RequiresAuth: true,
}

var _ = rungrad.Command{
	Use:            "private",
	RequiresAuth:   true,
	AuthResolution: rungrad.AuthResolutionHandler,
}

var _ rungrad.AuthResolution = rungrad.AuthResolutionFramework

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
			Use:            "framework",
			Short:          "show explicit framework auth",
			RequiresAuth:   true,
			AuthResolution: rungrad.AuthResolutionFramework,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				cred := f.Credential()
				return f.WriteResult(map[string]any{
					"token":            f.Token,
					"credential_token": cred.Token,
					"source":           cred.Source,
				}, func(w io.Writer) {})
			},
		},
		&rungrad.Command{
			Use:            "public",
			Short:          "show public data",
			AuthResolution: rungrad.AuthResolutionFramework,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return f.WriteResult(map[string]any{
					"token_empty":      f.Token == "",
					"credential_empty": f.Credential() == (rungrad.Credential{}),
				}, func(w io.Writer) {})
			},
		},
		&rungrad.Command{
			Use:            "handler",
			Short:          "show handler-owned auth",
			RequiresAuth:   true,
			AuthResolution: rungrad.AuthResolutionHandler,
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
					"token_empty":      f.Token == "",
					"credential_empty": f.Credential() == (rungrad.Credential{}),
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

func findBuiltCommand(t *testing.T, app *rungrad.App, args ...string) *cobra.Command {
	t.Helper()
	cmd, _, err := app.Root().Find(args)
	if err != nil {
		t.Fatalf("Find(%v): %v", args, err)
	}
	return cmd
}

func TestAuthResolutionConstantsAndDefaultFramework(t *testing.T) {
	if string(rungrad.AuthResolutionFramework) != "framework" {
		t.Fatalf("AuthResolutionFramework = %q", rungrad.AuthResolutionFramework)
	}
	if string(rungrad.AuthResolutionHandler) != "handler" {
		t.Fatalf("AuthResolutionHandler = %q", rungrad.AuthResolutionHandler)
	}

	app := resolutionTestApp(nil, nil)
	for _, tt := range []struct {
		path []string
		want string
	}{
		{[]string{"whoami"}, "framework"},
		{[]string{"framework"}, "framework"},
		{[]string{"handler"}, "handler"},
	} {
		cmd := findBuiltCommand(t, app, tt.path...)
		if cmd.Annotations[rungrad.AnnotationAuth] != "required" {
			t.Fatalf("%v auth annotation = %q", tt.path, cmd.Annotations[rungrad.AnnotationAuth])
		}
		if got := cmd.Annotations[privateAuthResolutionAnnotation]; got != tt.want {
			t.Fatalf("%v auth resolution annotation = %q, want %q", tt.path, got, tt.want)
		}
	}
	public := findBuiltCommand(t, app, "public")
	if public.Annotations[rungrad.AnnotationAuth] != "" || public.Annotations[privateAuthResolutionAnnotation] != "" {
		t.Fatalf("public annotations = %+v, want no auth annotations", public.Annotations)
	}
}

func TestAuthResolutionInvalidDeclarationsPreflightBeforeConfigure(t *testing.T) {
	t.Run("invalid top-level", func(t *testing.T) {
		app := rungrad.New(rungrad.AppConfig{Name: "rginvalid", Short: "invalid"})
		configured := false
		defer expectPanicContaining(t, `rungrad: command "bad" has invalid AuthResolution "bogus"`)()
		defer func() {
			if configured {
				t.Fatal("Configure ran for invalid top-level declaration")
			}
			if cmd, _, _ := app.Root().Find([]string{"bad"}); cmd != app.Root() {
				t.Fatalf("invalid top-level command was attached: %v", cmd.CommandPath())
			}
		}()
		app.AddCommand(&rungrad.Command{
			Use:            "bad",
			AuthResolution: rungrad.AuthResolution("bogus"),
			Configure:      func(*cobra.Command) { configured = true },
		})
	})

	t.Run("handler without requires auth", func(t *testing.T) {
		app := rungrad.New(rungrad.AppConfig{Name: "rginvalid", Short: "invalid"})
		defer expectPanicContaining(t, `rungrad: command "bad" uses handler auth resolution without RequiresAuth`)()
		app.AddCommand(&rungrad.Command{
			Use:            "bad",
			AuthResolution: rungrad.AuthResolutionHandler,
		})
	})

	t.Run("invalid descendant", func(t *testing.T) {
		app := rungrad.New(rungrad.AppConfig{Name: "rginvalid", Short: "invalid"})
		var configured []string
		parent := &rungrad.Command{
			Use:       "parent",
			Configure: func(*cobra.Command) { configured = append(configured, "parent") },
		}
		parent.AddCommand(
			&rungrad.Command{
				Use:       "good",
				Configure: func(*cobra.Command) { configured = append(configured, "good") },
			},
			&rungrad.Command{
				Use:            "bad",
				RequiresAuth:   true,
				AuthResolution: rungrad.AuthResolution("bogus"),
				Configure:      func(*cobra.Command) { configured = append(configured, "bad") },
			},
		)
		defer expectPanicContaining(t, `rungrad: command "bad" has invalid AuthResolution "bogus"`)()
		defer func() {
			if len(configured) != 0 {
				t.Fatalf("Configure callbacks ran before preflight finished: %v", configured)
			}
			if cmd, _, _ := app.Root().Find([]string{"parent"}); cmd != app.Root() {
				t.Fatalf("partial subtree was attached: %v", cmd.CommandPath())
			}
		}()
		app.AddCommand(parent)
	})
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

func TestHandlerAuthResolutionBypassesResolverWithResolvedState(t *testing.T) {
	called := 0
	app := resolutionTestApp(resolverFunc(func(*rungrad.AuthContext) (rungrad.Credential, error) {
		called++
		return rungrad.Credential{}, errors.New("framework resolver must not run")
	}), nil)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeResolutionConfig(t, cfgPath, config.Config{Version: 1})
	res := testutil.Run(app,
		"handler",
		"--config", cfgPath,
		"--profile", "selected",
		"--base-url", "https://handler.api",
		"--json",
	)
	out := decodeMap(t, res)
	if called != 0 {
		t.Fatalf("resolver calls = %d, want 0", called)
	}
	if out["resolved"] != true || out["profile"] != "selected" ||
		out["config_path"] != cfgPath || out["api"] != "https://handler.api" ||
		out["api_source"] != "flag" || out["token_empty"] != true ||
		out["credential_empty"] != true {
		t.Fatalf("handler output = %#v", out)
	}
	if app.Factory().Token != "" || app.Factory().Credential() != (rungrad.Credential{}) {
		t.Fatalf("factory auth state after handler = token %q credential %+v",
			app.Factory().Token, app.Factory().Credential())
	}
}

func TestExplicitFrameworkAuthResolutionInvokesResolver(t *testing.T) {
	called := 0
	app := resolutionTestApp(resolverFunc(func(*rungrad.AuthContext) (rungrad.Credential, error) {
		called++
		return rungrad.Credential{Token: "explicit-framework-secret", Source: "custom"}, nil
	}), nil)
	out := decodeMap(t, testutil.Run(app, "framework", "--json"))
	if called != 1 {
		t.Fatalf("resolver calls = %d, want 1", called)
	}
	if out["source"] != "custom" || out["token"] != "[REDACTED]" || out["credential_token"] != "[REDACTED]" {
		t.Fatalf("framework auth output = %#v", out)
	}
}

func TestExplicitFrameworkResolutionWithoutAuthDoesNotInvokeResolver(t *testing.T) {
	called := 0
	app := resolutionTestApp(resolverFunc(func(*rungrad.AuthContext) (rungrad.Credential, error) {
		called++
		return rungrad.Credential{}, errors.New("resolver must not run")
	}), nil)
	out := decodeMap(t, testutil.Run(app, "public", "--json"))
	if called != 0 {
		t.Fatalf("resolver calls = %d, want 0", called)
	}
	if out["token_empty"] != true || out["credential_empty"] != true {
		t.Fatalf("public output = %#v", out)
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

func TestManualCobraAuthResolutionCompatibility(t *testing.T) {
	for _, tt := range []struct {
		name           string
		ownerValue     string
		wantResolver   int
		wantTokenEmpty bool
	}{
		{name: "legacy missing owner", wantResolver: 1},
		{name: "unexpected owner", ownerValue: "bogus", wantResolver: 1},
		{name: "exact handler", ownerValue: "handler", wantTokenEmpty: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			app := rungrad.New(rungrad.AppConfig{
				Name:  "rgmanual",
				Short: "manual cobra",
				Auth: resolverFunc(func(*rungrad.AuthContext) (rungrad.Credential, error) {
					calls++
					return rungrad.Credential{Token: "manual-secret-token", Source: "manual"}, nil
				}),
			})
			annotations := map[string]string{rungrad.AnnotationAuth: "required"}
			if tt.ownerValue != "" {
				annotations[privateAuthResolutionAnnotation] = tt.ownerValue
			}
			app.Root().AddCommand(&cobra.Command{
				Use:           "raw",
				Short:         "raw cobra command",
				SilenceUsage:  true,
				SilenceErrors: true,
				Annotations:   annotations,
				RunE: func(cmd *cobra.Command, args []string) error {
					return app.Factory().WriteResult(map[string]any{
						"token_empty":      app.Factory().Token == "",
						"credential_empty": app.Factory().Credential() == (rungrad.Credential{}),
					}, func(w io.Writer) {})
				},
			})
			out := decodeMap(t, testutil.Run(app, "raw", "--json"))
			if calls != tt.wantResolver {
				t.Fatalf("resolver calls = %d, want %d", calls, tt.wantResolver)
			}
			if out["token_empty"] != tt.wantTokenEmpty {
				t.Fatalf("token_empty = %#v, want %t; out=%#v", out["token_empty"], tt.wantTokenEmpty, out)
			}
		})
	}
}

func TestHandlerAuthResolutionValidationFailuresPrecedeOwnerWork(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		advanced  bool
		outputs   []string
		configure func(*cobra.Command)
		want      string
	}{
		{
			name: "required flag",
			args: []string{"owned"},
			configure: func(cmd *cobra.Command) {
				cmd.Flags().String("name", "", "name")
				if err := cmd.MarkFlagRequired("name"); err != nil {
					t.Fatal(err)
				}
			},
			want: `required flag(s) "name" not set`,
		},
		{
			name: "flag group",
			args: []string{"owned", "--left", "--right"},
			configure: func(cmd *cobra.Command) {
				cmd.Flags().Bool("left", false, "left")
				cmd.Flags().Bool("right", false, "right")
				cmd.MarkFlagsMutuallyExclusive("left", "right")
			},
			want: `if any flags in the group [left right] are set none of the others can be; [left right] were all set`,
		},
		{
			name:     "output mode",
			args:     []string{"owned", "--plain"},
			advanced: true,
			outputs:  []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			want:     `"rgowned owned" does not support --plain`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolverCalls := 0
			handlerCalls := 0
			app := rungrad.New(rungrad.AppConfig{
				Name:           "rgowned",
				Short:          "owned auth",
				AdvancedOutput: tt.advanced,
				Auth: resolverFunc(func(*rungrad.AuthContext) (rungrad.Credential, error) {
					resolverCalls++
					return rungrad.Credential{Token: "secret"}, nil
				}),
			})
			app.AddCommand(&rungrad.Command{
				Use:            "owned",
				Short:          "owned auth",
				OutputModes:    tt.outputs,
				RequiresAuth:   true,
				AuthResolution: rungrad.AuthResolutionHandler,
				Configure:      tt.configure,
				Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
					handlerCalls++
					return nil
				},
			})
			res := testutil.Run(app, tt.args...)
			if res.Exit != rungrad.ExitUsage || res.Stdout != "" {
				t.Fatalf("result = %#v, want usage with empty stdout", res)
			}
			if !strings.Contains(res.Stderr, tt.want) {
				t.Fatalf("stderr = %q, want containing %q", res.Stderr, tt.want)
			}
			if resolverCalls != 0 || handlerCalls != 0 {
				t.Fatalf("owner work ran: resolver=%d handler=%d", resolverCalls, handlerCalls)
			}
		})
	}
}

func TestHandlerAuthResolutionErrorUsesExistingAuthExitPolicy(t *testing.T) {
	app := rungrad.New(rungrad.AppConfig{Name: "rgowned", Short: "owned auth", AdvancedOutput: true})
	app.AddCommand(&rungrad.Command{
		Use:            "owned",
		Short:          "owned auth",
		RequiresAuth:   true,
		AuthResolution: rungrad.AuthResolutionHandler,
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			return config.ErrMissingCredential
		},
	})
	res := testutil.Run(app, "owned", "--json")
	if res.Exit != rungrad.ExitAuth || res.Stdout != "" {
		t.Fatalf("result = %#v, want auth failure with empty stdout", res)
	}
	if !json.Valid([]byte(res.Stderr)) || !strings.Contains(res.Stderr, `"exit_code": 3`) {
		t.Fatalf("stderr is not normal machine auth error JSON: %q", res.Stderr)
	}
}

func TestHandlerAuthResolutionPublicProjectionsHideOwner(t *testing.T) {
	resolverCalls := 0
	handlerCalls := 0
	app := rungrad.New(rungrad.AppConfig{
		Name:    "rgowned",
		Short:   "owned auth",
		Version: "1.0.0",
		Auth: resolverFunc(func(*rungrad.AuthContext) (rungrad.Credential, error) {
			resolverCalls++
			return rungrad.Credential{Token: "secret"}, nil
		}),
	})
	app.AddModule(stubModule{
		commands: []*rungrad.Command{{
			Use:            "owned",
			Short:          "owned auth",
			RequiresAuth:   true,
			AuthResolution: rungrad.AuthResolutionHandler,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				handlerCalls++
				return nil
			},
		}},
		specs: []rungrad.CommandSpec{{
			Path:         "owned",
			Summary:      "owned auth",
			RequiresAuth: true,
		}},
	})
	if err := app.ValidateCatalog(); err != nil {
		t.Fatalf("ValidateCatalog() = %v", err)
	}

	m, manifestResult := readManifest(t, app)
	if resolverCalls != 0 || handlerCalls != 0 {
		t.Fatalf("manifest touched owner work: resolver=%d handler=%d", resolverCalls, handlerCalls)
	}
	owned := findManifestCommand(&m, "owned")
	if owned == nil || !owned.RequiresAuth {
		t.Fatalf("owned manifest entry = %+v", owned)
	}
	if app.Factory().Store != (config.Store{}) || app.Factory().Token != "" ||
		app.Factory().Credential() != (rungrad.Credential{}) {
		t.Fatalf("manifest left factory auth/resolution state: store=%+v token=%q credential=%+v",
			app.Factory().Store, app.Factory().Token, app.Factory().Credential())
	}
	docs := docsgen.Generate(app)
	if page := docs["rgowned_owned.md"]; !strings.Contains(page, "## Authentication") {
		t.Fatalf("owned docs missing authentication section:\n%s", page)
	}
	help := testutil.Run(app, "owned", "--help")
	if help.Exit != rungrad.ExitSuccess {
		t.Fatalf("help exit = %d stderr=%q", help.Exit, help.Stderr)
	}
	completion := testutil.Run(app, "__complete", "")
	if completion.Exit != rungrad.ExitSuccess {
		t.Fatalf("completion exit = %d stderr=%q", completion.Exit, completion.Stderr)
	}
	for label, text := range map[string]string{
		"manifest":   manifestResult.Stdout,
		"docs":       strings.Join(mapValues(docs), "\n"),
		"help":       help.Stdout,
		"completion": completion.Stdout + completion.Stderr,
	} {
		if strings.Contains(text, privateAuthResolutionAnnotation) {
			t.Fatalf("%s leaked private auth annotation:\n%s", label, text)
		}
	}
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func TestAuthResolutionResetIsolationAcrossReusableApp(t *testing.T) {
	const frameworkSecret = "framework-secret-token"
	const handlerSecret = "handler-secret-token"
	newApp := func() *rungrad.App {
		app := rungrad.New(rungrad.AppConfig{
			Name:  "rgreset",
			Short: "reset auth",
			Auth: resolverFunc(func(*rungrad.AuthContext) (rungrad.Credential, error) {
				return rungrad.Credential{
					Token: frameworkSecret,
					Extra: handlerSecret,
				}, nil
			}),
		})
		app.AddCommand(
			&rungrad.Command{
				Use:          "framework",
				Short:        "framework auth",
				RequiresAuth: true,
				Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
					return f.WriteResult(map[string]any{
						"token":                  f.Token,
						"credential_empty":       f.Credential() == (rungrad.Credential{}),
						"handler_secret_literal": f.Credential().Extra,
					}, func(w io.Writer) {})
				},
			},
			&rungrad.Command{
				Use:            "handler",
				Short:          "handler auth",
				RequiresAuth:   true,
				AuthResolution: rungrad.AuthResolutionHandler,
				Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
					f.RegisterSecret(handlerSecret)
					return f.WriteResult(map[string]any{
						"token_empty":              f.Token == "",
						"credential_empty":         f.Credential() == (rungrad.Credential{}),
						"framework_secret_literal": frameworkSecret,
						"handler_secret":           handlerSecret,
					}, func(w io.Writer) {})
				},
			},
		)
		return app
	}

	app := newApp()
	firstFramework := testutil.Run(app, "framework", "--json")
	if firstFramework.Exit != rungrad.ExitSuccess ||
		!strings.Contains(firstFramework.Stdout, `"token": "[REDACTED]"`) ||
		!strings.Contains(firstFramework.Stdout, handlerSecret) {
		t.Fatalf("first framework result = %#v", firstFramework)
	}
	handler := testutil.Run(app, "handler", "--json")
	if handler.Exit != rungrad.ExitSuccess ||
		!strings.Contains(handler.Stdout, frameworkSecret) ||
		!strings.Contains(handler.Stdout, `"handler_secret": "[REDACTED]"`) ||
		!strings.Contains(handler.Stdout, `"token_empty": true`) ||
		!strings.Contains(handler.Stdout, `"credential_empty": true`) {
		t.Fatalf("handler after framework result = %#v", handler)
	}
	secondFramework := testutil.Run(app, "framework", "--json")
	if secondFramework.Exit != rungrad.ExitSuccess ||
		!strings.Contains(secondFramework.Stdout, `"token": "[REDACTED]"`) ||
		!strings.Contains(secondFramework.Stdout, handlerSecret) {
		t.Fatalf("framework after handler result = %#v", secondFramework)
	}

	app = newApp()
	firstHandler := testutil.Run(app, "handler", "--json")
	if firstHandler.Exit != rungrad.ExitSuccess ||
		!strings.Contains(firstHandler.Stdout, frameworkSecret) ||
		!strings.Contains(firstHandler.Stdout, `"token_empty": true`) {
		t.Fatalf("first handler result = %#v", firstHandler)
	}
	frameworkAfterHandler := testutil.Run(app, "framework", "--json")
	if frameworkAfterHandler.Exit != rungrad.ExitSuccess ||
		!strings.Contains(frameworkAfterHandler.Stdout, `"token": "[REDACTED]"`) ||
		!strings.Contains(frameworkAfterHandler.Stdout, handlerSecret) {
		t.Fatalf("framework after first handler result = %#v", frameworkAfterHandler)
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
