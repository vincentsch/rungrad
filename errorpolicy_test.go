package rungrad_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/config"
	"github.com/vincentsch/rungrad/testutil"
)

type errorPolicyResolverFunc func(*rungrad.AuthContext) (rungrad.Credential, error)

func (fn errorPolicyResolverFunc) ResolveCredential(ac *rungrad.AuthContext) (rungrad.Credential, error) {
	return fn(ac)
}

// errorPolicyApp builds a compact fixture with enough command shapes to exercise
// each error stage: resolution, flag parsing, argument validation, auth, handler
// failure, and advanced output guards.
func errorPolicyApp(advanced bool, policy *rungrad.ErrorPolicy, auth rungrad.CredentialResolver) *rungrad.App {
	modes := []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON}
	if advanced {
		modes = append(modes, rungrad.OutputModePlain, rungrad.OutputModeJQ, rungrad.OutputModeTemplate)
	}
	cfg := rungrad.AppConfig{
		Name:           "rgerr",
		Short:          "error policy test",
		EnvVar:         "RGERR_TOKEN",
		Profile:        "work",
		AdvancedOutput: advanced,
		ErrorPolicy:    policy,
		Auth:           auth,
	}
	app := rungrad.New(cfg)
	item := &rungrad.Command{
		Use:   "item",
		Short: "item parent",
		Args:  cobra.NoArgs,
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	item.AddCommand(&rungrad.Command{
		Use:   "get <name>",
		Short: "get item",
		Args:  cobra.ExactArgs(1),
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			return nil
		},
	})
	app.AddCommand(
		&rungrad.Command{
			Use:         "read",
			Short:       "read data",
			OutputModes: modes,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return f.WriteOutput(rungrad.Output{
					Model: map[string]string{"value": "ok"},
					Human: func(w io.Writer) {
						fmt.Fprintln(w, "ok")
					},
					Plain: func(w io.Writer) {
						fmt.Fprintln(w, "ok")
					},
				})
			},
		},
		&rungrad.Command{
			Use:   "boom",
			Short: "fail api",
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return rungrad.NewError(rungrad.ExitAPI, "boom failed")
			},
		},
		// local and parent both use a command-local value flag so tests can prove
		// a parsed value of "--json" does not force machine-mode error rendering.
		&rungrad.Command{
			Use:   "local",
			Short: "local flag failure",
			Configure: func(cmd *cobra.Command) {
				cmd.Flags().String("name", "", "Name")
			},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return rungrad.NewError(rungrad.ExitAPI, "local failed")
			},
		},
		&rungrad.Command{
			Use:   "parent",
			Short: "no-args parent",
			Args:  cobra.NoArgs,
			Configure: func(cmd *cobra.Command) {
				cmd.Flags().String("name", "", "Name")
			},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return nil
			},
		},
		&rungrad.Command{
			Use:          "whoami",
			Short:        "show auth",
			RequiresAuth: true,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return rungrad.NewError(rungrad.ExitForbidden, "nope "+f.Token)
			},
		},
		item,
		&rungrad.Command{
			Use:   "need",
			Short: "need flag",
			Configure: func(cmd *cobra.Command) {
				cmd.Flags().String("name", "", "Name")
				_ = cmd.MarkFlagRequired("name")
			},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return nil
			},
		},
		&rungrad.Command{
			Use:   "pair",
			Short: "exclusive flags",
			Configure: func(cmd *cobra.Command) {
				cmd.Flags().Bool("left", false, "Left")
				cmd.Flags().Bool("right", false, "Right")
				cmd.MarkFlagsMutuallyExclusive("left", "right")
			},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return nil
			},
		},
		&rungrad.Command{
			Use:   "exact <name>",
			Short: "exact args",
			Args:  cobra.ExactArgs(1),
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return nil
			},
		},
	)
	return app
}

func runErrorPolicyApp(app *rungrad.App, args ...string) testutil.Result {
	return testutil.RunWith(app, testutil.Options{
		UserConfigDir: func() (string, error) { return "/tmp/rungrad-errorpolicy-config", nil },
	}, args...)
}

func decodeErrorJSON(t *testing.T, stderr string) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(stderr), &body); err != nil {
		t.Fatalf("stderr is not JSON: %v\n%s", err, stderr)
	}
	return body
}

func expectHumanError(t *testing.T, stderr string) {
	t.Helper()
	if !strings.HasPrefix(stderr, "Error: ") {
		t.Fatalf("stderr = %q, want human Error prefix", stderr)
	}
	if json.Valid([]byte(stderr)) {
		t.Fatalf("stderr = %q, did not want JSON", stderr)
	}
}

func TestDefaultUnknownCommandHumanErrorByteIdentical(t *testing.T) {
	res := runErrorPolicyApp(errorPolicyApp(false, nil, nil), "nope")
	if res.Exit != rungrad.ExitUsage {
		t.Fatalf("exit = %d, stderr=%q", res.Exit, res.Stderr)
	}
	if res.Stdout != "" {
		t.Fatalf("stdout = %q, want empty", res.Stdout)
	}
	want := "Error: unknown command \"nope\" for \"rgerr\"\n"
	if res.Stderr != want {
		t.Fatalf("stderr = %q, want %q", res.Stderr, want)
	}
}

func TestErrorPolicyCustomRendererSuppressesDefault(t *testing.T) {
	policy := &rungrad.ErrorPolicy{
		Render: func(ctx rungrad.ErrorContext) error {
			_, _ = fmt.Fprintf(ctx.Stderr, "custom: %s\n", ctx.CommandPath)
			return nil
		},
	}
	res := runErrorPolicyApp(errorPolicyApp(false, policy, nil), "nope")
	if res.Exit != rungrad.ExitUsage || res.Stdout != "" || res.Stderr != "custom: rgerr\n" {
		t.Fatalf("result = %#v", res)
	}
}

func TestErrorPolicyCustomClassifierFeedsDefaultJSON(t *testing.T) {
	policy := &rungrad.ErrorPolicy{
		Classify: func(ctx rungrad.ErrorContext) int {
			if ctx.DefaultExitCode == rungrad.ExitAPI {
				return 42
			}
			return ctx.DefaultExitCode
		},
	}
	res := runErrorPolicyApp(errorPolicyApp(false, policy, nil), "boom", "--json")
	if res.Exit != 42 || res.Stdout != "" {
		t.Fatalf("result = %#v", res)
	}
	body := decodeErrorJSON(t, res.Stderr)
	if body["exit_code"] != float64(42) || body["error"] != "boom failed" {
		t.Fatalf("body = %#v", body)
	}
}

func TestErrorPolicyBothHooksShareExitCode(t *testing.T) {
	policy := &rungrad.ErrorPolicy{
		Classify: func(ctx rungrad.ErrorContext) int { return 7 },
		Render: func(ctx rungrad.ErrorContext) error {
			_, _ = fmt.Fprintf(ctx.Stderr, "exit=%d default=%d", ctx.ExitCode, ctx.DefaultExitCode)
			return nil
		},
	}
	res := runErrorPolicyApp(errorPolicyApp(false, policy, nil), "boom")
	if res.Exit != 7 || res.Stderr != "exit=7 default=2" || res.Stdout != "" {
		t.Fatalf("result = %#v", res)
	}
}

func TestErrorContextCommandAndStageFields(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		advanced      bool
		auth          rungrad.CredentialResolver
		wantExit      int
		wantPath      string
		wantMachine   bool
		wantResolved  bool
		wantCred      bool
		wantNoCommand bool
	}{
		{name: "unknown command", args: []string{"nope"}, wantExit: rungrad.ExitUsage, wantPath: "rgerr"},
		{name: "nested unknown command", args: []string{"item", "bogus"}, wantExit: rungrad.ExitUsage, wantPath: "rgerr item"},
		{name: "unknown flag", args: []string{"read", "--bogus"}, wantExit: rungrad.ExitUsage, wantPath: "rgerr read"},
		{name: "required flag", args: []string{"need"}, wantExit: rungrad.ExitUsage, wantPath: "rgerr need"},
		{name: "flag group", args: []string{"pair", "--left", "--right"}, wantExit: rungrad.ExitUsage, wantPath: "rgerr pair"},
		{name: "arg validation", args: []string{"exact"}, wantExit: rungrad.ExitUsage, wantPath: "rgerr exact"},
		{name: "advanced guard", args: []string{"read", "--include-meta"}, advanced: true, wantExit: rungrad.ExitUsage, wantPath: "rgerr read"},
		{
			name:     "auth failure",
			args:     []string{"whoami"},
			wantExit: rungrad.ExitAuth,
			wantPath: "rgerr whoami",
			auth: errorPolicyResolverFunc(func(ac *rungrad.AuthContext) (rungrad.Credential, error) {
				return rungrad.Credential{}, config.ErrMissingCredential
			}),
			wantResolved: true,
		},
		{
			name:     "handler failure with credential",
			args:     []string{"whoami"},
			wantExit: rungrad.ExitForbidden,
			wantPath: "rgerr whoami",
			auth: errorPolicyResolverFunc(func(ac *rungrad.AuthContext) (rungrad.Credential, error) {
				return rungrad.Credential{Token: "supersecret", Source: "env"}, nil
			}),
			wantResolved: true,
			wantCred:     true,
		},
		{name: "machine unknown", args: []string{"--json", "nope"}, wantExit: rungrad.ExitUsage, wantPath: "rgerr", wantMachine: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got rungrad.ErrorContext
			policy := &rungrad.ErrorPolicy{
				Render: func(ctx rungrad.ErrorContext) error {
					got = ctx
					return nil
				},
			}
			res := runErrorPolicyApp(errorPolicyApp(tt.advanced, policy, tt.auth), tt.args...)
			if res.Exit != tt.wantExit || res.Stdout != "" || res.Stderr != "" {
				t.Fatalf("result = %#v", res)
			}
			if tt.wantNoCommand {
				if got.Command != nil {
					t.Fatalf("Command = %v, want nil", got.Command)
				}
			} else if got.Command == nil {
				t.Fatal("Command = nil")
			}
			if got.CommandPath != tt.wantPath {
				t.Fatalf("CommandPath = %q, want %q", got.CommandPath, tt.wantPath)
			}
			if got.ExitCode != tt.wantExit || got.DefaultExitCode != tt.wantExit {
				t.Fatalf("codes = %d/%d, want %d", got.ExitCode, got.DefaultExitCode, tt.wantExit)
			}
			if got.MachineOutput != tt.wantMachine {
				t.Fatalf("MachineOutput = %v, want %v", got.MachineOutput, tt.wantMachine)
			}
			if !reflect.DeepEqual(got.Args, tt.args) {
				t.Fatalf("Args = %#v, want %#v", got.Args, tt.args)
			}
			if tt.wantResolved {
				if got.Profile != "work" || got.ConfigPath == "" || got.AuthFilePath == "" {
					t.Fatalf("resolved fields = profile %q config %q auth %q", got.Profile, got.ConfigPath, got.AuthFilePath)
				}
			} else if got.Profile != "" || got.ConfigPath != "" || got.AuthFilePath != "" || got.Services != nil {
				t.Fatalf("resolved fields should be empty: %#v", got)
			}
			if tt.wantCred {
				if got.CredentialSource != "env" || got.CredentialDisplay != config.Mask("supersecret") {
					t.Fatalf("credential fields = %q/%q", got.CredentialSource, got.CredentialDisplay)
				}
				if strings.Contains(got.CredentialDisplay, "supersecret") {
					t.Fatalf("credential display leaked raw token: %q", got.CredentialDisplay)
				}
			} else if got.CredentialSource != "" || got.CredentialDisplay != "" {
				t.Fatalf("credential fields should be empty: %q/%q", got.CredentialSource, got.CredentialDisplay)
			}
		})
	}
}

func errorPolicyResolutionApp(policy *rungrad.ErrorPolicy, auth rungrad.CredentialResolver) *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:        "rgerr",
		Short:       "error policy resolution test",
		EnvVar:      "RGERR_TOKEN",
		Profile:     "work",
		ErrorPolicy: policy,
		Auth:        auth,
		Resolution: &rungrad.ResolutionConfig{
			Profile:  true,
			AuthFile: true,
			Services: []rungrad.Service{{
				Name:    "api",
				Flag:    "api-url",
				Default: "https://api.default",
				Usage:   "API URL",
			}},
		},
	})
	app.AddCommand(&rungrad.Command{
		Use:          "whoami",
		Short:        "show auth",
		RequiresAuth: true,
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			return rungrad.NewError(rungrad.ExitForbidden, "nope "+f.Token)
		},
	})
	return app
}

func TestErrorContextResolutionBuildFailureAndServices(t *testing.T) {
	var buildCtx rungrad.ErrorContext
	buildPolicy := &rungrad.ErrorPolicy{
		Render: func(ctx rungrad.ErrorContext) error {
			buildCtx = ctx
			return nil
		},
	}
	buildRes := runErrorPolicyApp(errorPolicyResolutionApp(buildPolicy, nil), "--profile", "bad profile", "whoami")
	if buildRes.Exit != rungrad.ExitUsage || buildRes.Stdout != "" || buildRes.Stderr != "" {
		t.Fatalf("build resolution result = %#v", buildRes)
	}
	if buildCtx.Command == nil || buildCtx.CommandPath != "rgerr whoami" {
		t.Fatalf("build resolution command = %v path %q", buildCtx.Command, buildCtx.CommandPath)
	}
	if buildCtx.Profile != "" || buildCtx.ConfigPath != "" || buildCtx.AuthFilePath != "" || buildCtx.Services != nil {
		t.Fatalf("build resolution fields should be empty before storeReady: %#v", buildCtx)
	}
	if buildCtx.CredentialSource != "" || buildCtx.CredentialDisplay != "" {
		t.Fatalf("build resolution credential fields = %q/%q", buildCtx.CredentialSource, buildCtx.CredentialDisplay)
	}

	var authCtx rungrad.ErrorContext
	authPolicy := &rungrad.ErrorPolicy{
		Render: func(ctx rungrad.ErrorContext) error {
			authCtx = ctx
			return nil
		},
	}
	auth := errorPolicyResolverFunc(func(ac *rungrad.AuthContext) (rungrad.Credential, error) {
		return rungrad.Credential{}, config.ErrMissingCredential
	})
	authRes := runErrorPolicyApp(errorPolicyResolutionApp(authPolicy, auth), "whoami")
	if authRes.Exit != rungrad.ExitAuth || authRes.Stdout != "" || authRes.Stderr != "" {
		t.Fatalf("auth resolution result = %#v", authRes)
	}
	if authCtx.Profile != "work" || authCtx.ConfigPath == "" || authCtx.AuthFilePath == "" {
		t.Fatalf("auth resolved fields = profile %q config %q auth %q", authCtx.Profile, authCtx.ConfigPath, authCtx.AuthFilePath)
	}
	if got := authCtx.Services["api"]; got != "https://api.default" {
		t.Fatalf("Services[api] = %q, want default", got)
	}
	if authCtx.CredentialSource != "" || authCtx.CredentialDisplay != "" {
		t.Fatalf("auth credential fields should be empty on resolver failure: %q/%q", authCtx.CredentialSource, authCtx.CredentialDisplay)
	}
}

func TestErrorContextReferenceFieldsAreIsolatedBetweenHooks(t *testing.T) {
	var renderArgs []string
	var renderServices map[string]string
	policy := &rungrad.ErrorPolicy{
		Classify: func(ctx rungrad.ErrorContext) int {
			// Mutate reference fields deliberately; Render should receive its own
			// clone, and the caller's argv must stay untouched.
			ctx.Args[0] = "mutated"
			ctx.Services["api"] = "https://mutated.invalid"
			return ctx.DefaultExitCode
		},
		Render: func(ctx rungrad.ErrorContext) error {
			renderArgs = append([]string(nil), ctx.Args...)
			renderServices = map[string]string{}
			for k, v := range ctx.Services {
				renderServices[k] = v
			}
			return nil
		},
	}
	auth := errorPolicyResolverFunc(func(ac *rungrad.AuthContext) (rungrad.Credential, error) {
		return rungrad.Credential{}, config.ErrMissingCredential
	})
	app := errorPolicyResolutionApp(policy, auth)
	args := []string{"whoami"}
	res := testutil.RunWith(app, testutil.Options{
		UserConfigDir: func() (string, error) { return "/tmp/rungrad-errorpolicy-config", nil },
	}, args...)
	if res.Exit != rungrad.ExitAuth || res.Stdout != "" || res.Stderr != "" {
		t.Fatalf("result = %#v", res)
	}
	if args[0] != "whoami" {
		t.Fatalf("caller args mutated to %#v", args)
	}
	if !reflect.DeepEqual(renderArgs, []string{"whoami"}) {
		t.Fatalf("render Args = %#v, want original", renderArgs)
	}
	if got := renderServices["api"]; got != "https://api.default" {
		t.Fatalf("render Services[api] = %q, want default", got)
	}
}

func TestDefaultMachineCommandResolutionJSONEnvelopes(t *testing.T) {
	tests := []struct {
		name     string
		advanced bool
		args     []string
	}{
		{name: "json before unknown", args: []string{"--json", "bogus"}},
		{name: "json after unknown", args: []string{"bogus", "--json"}},
		{name: "nested unknown", args: []string{"item", "bogus", "--json"}},
		{name: "unknown flag", args: []string{"read", "--bogus", "--json"}},
		{name: "advanced jq", advanced: true, args: []string{"bogus", "--jq", "."}},
		{name: "advanced template", advanced: true, args: []string{"bogus", "--template", "{{.}}"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runErrorPolicyApp(errorPolicyApp(tt.advanced, nil, nil), tt.args...)
			if res.Exit != rungrad.ExitUsage || res.Stdout != "" {
				t.Fatalf("result = %#v", res)
			}
			body := decodeErrorJSON(t, res.Stderr)
			if body["error"] == "" || body["exit_code"] != float64(res.Exit) {
				t.Fatalf("body = %#v, exit = %d", body, res.Exit)
			}
		})
	}
}

func TestErrorPolicyRendererFailureFallback(t *testing.T) {
	const secret = "renderer-secret-value"
	newApp := func(machine bool) *rungrad.App {
		policy := &rungrad.ErrorPolicy{
			Render: func(ctx rungrad.ErrorContext) error {
				_, _ = fmt.Fprint(ctx.Stderr, "HOST BYTES")
				return errors.New("render boom " + secret)
			},
		}
		app := errorPolicyApp(true, policy, nil)
		app.AddCommand(&rungrad.Command{
			Use:         "secretfail",
			Short:       "register then fail",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				f.RegisterSecret(secret)
				return rungrad.NewError(rungrad.ExitAPI, "original failed")
			},
		})
		return app
	}

	human := runErrorPolicyApp(newApp(false), "secretfail")
	if human.Exit != rungrad.ExitAPI || human.Stdout != "" {
		t.Fatalf("human result = %#v", human)
	}
	if strings.Contains(human.Stderr, "HOST BYTES") || strings.Contains(human.Stderr, secret) {
		t.Fatalf("human stderr leaked staged bytes or secret: %q", human.Stderr)
	}
	wantHuman := "Error: original failed\nError: error renderer failed: render boom [REDACTED]\n"
	if human.Stderr != wantHuman {
		t.Fatalf("human stderr = %q, want %q", human.Stderr, wantHuman)
	}

	for _, args := range [][]string{{"secretfail", "--json"}, {"secretfail", "--jq", "."}, {"secretfail", "--template", "{{.}}"}} {
		machine := runErrorPolicyApp(newApp(true), args...)
		if machine.Exit != rungrad.ExitAPI || machine.Stdout != "" {
			t.Fatalf("machine result for %v = %#v", args, machine)
		}
		if strings.Contains(machine.Stderr, "HOST BYTES") || strings.Contains(machine.Stderr, secret) {
			t.Fatalf("machine stderr leaked staged bytes or secret: %q", machine.Stderr)
		}
		body := decodeErrorJSON(t, machine.Stderr)
		if body["error"] != "original failed" || body["exit_code"] != float64(rungrad.ExitAPI) {
			t.Fatalf("body = %#v", body)
		}
		if body["renderer_error"] != "render boom [REDACTED]" {
			t.Fatalf("renderer_error = %#v", body["renderer_error"])
		}
	}
}

func TestErrorPolicyInvalidClassifierFallsBackToDefault(t *testing.T) {
	for _, classify := range []func(rungrad.ErrorContext) int{
		func(rungrad.ErrorContext) int { return 0 },
		func(rungrad.ErrorContext) int { return -5 },
	} {
		policy := &rungrad.ErrorPolicy{Classify: classify}
		res := runErrorPolicyApp(errorPolicyApp(false, policy, nil), "boom", "--json")
		if res.Exit != rungrad.ExitAPI {
			t.Fatalf("exit = %d, stderr=%q", res.Exit, res.Stderr)
		}
		body := decodeErrorJSON(t, res.Stderr)
		if body["exit_code"] != float64(rungrad.ExitAPI) {
			t.Fatalf("body = %#v", body)
		}
	}
}

func TestErrorPolicyMachineIntentDetectorEdges(t *testing.T) {
	tests := []struct {
		name      string
		advanced  bool
		args      []string
		wantJSON  bool
		wantExit  int
		wantError string
	}{
		{name: "terminator", args: []string{"bogus", "--", "--json"}, wantExit: rungrad.ExitUsage},
		{name: "json false resolution", args: []string{"bogus", "--json=false"}, wantExit: rungrad.ExitUsage},
		{name: "json false flag parse", args: []string{"read", "--bogus", "--json=false"}, wantExit: rungrad.ExitUsage},
		{name: "compact jq unknown flag", args: []string{"read", "--jq", "."}, wantExit: rungrad.ExitUsage, wantError: "unknown flag: --jq"},
		{name: "advanced jq before unknown", advanced: true, args: []string{"--jq", ".", "bogus"}, wantJSON: true, wantExit: rungrad.ExitUsage},
		{name: "advanced template after unknown", advanced: true, args: []string{"bogus", "--template", "{{.}}"}, wantJSON: true, wantExit: rungrad.ExitUsage},
		{name: "config consumes json", args: []string{"--config", "--json", "bogus"}, wantExit: rungrad.ExitUsage},
		{name: "config then json", args: []string{"--config", "somewhere", "--json", "bogus"}, wantJSON: true, wantExit: rungrad.ExitUsage},
		{name: "local value consumes json after parse", args: []string{"local", "--name", "--json"}, wantExit: rungrad.ExitAPI, wantError: "local failed"},
		{name: "noargs local value consumes json after parse", args: []string{"parent", "--name", "--json", "extra"}, wantExit: rungrad.ExitUsage, wantError: "unknown command \"extra\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runErrorPolicyApp(errorPolicyApp(tt.advanced, nil, nil), tt.args...)
			if res.Exit != tt.wantExit || res.Stdout != "" {
				t.Fatalf("result = %#v", res)
			}
			if tt.wantJSON {
				body := decodeErrorJSON(t, res.Stderr)
				if body["exit_code"] != float64(tt.wantExit) {
					t.Fatalf("body = %#v", body)
				}
				return
			}
			expectHumanError(t, res.Stderr)
			if tt.wantError != "" && !strings.Contains(res.Stderr, tt.wantError) {
				t.Fatalf("stderr = %q, want substring %q", res.Stderr, tt.wantError)
			}
		})
	}
}

func TestErrorPolicyFlagsSnapshotMutationDoesNotAffectFallback(t *testing.T) {
	policy := &rungrad.ErrorPolicy{
		Render: func(ctx rungrad.ErrorContext) error {
			ctx.Flags.JSON = true
			return errors.New("force fallback")
		},
	}
	app := errorPolicyApp(false, policy, nil)
	res := runErrorPolicyApp(app, "boom")
	if res.Exit != rungrad.ExitAPI || res.Stdout != "" {
		t.Fatalf("result = %#v", res)
	}
	expectHumanError(t, res.Stderr)
	if app.Flags().JSON {
		t.Fatal("app.Flags().JSON changed after ErrorContext mutation")
	}
}

func TestErrorContextDoesNotExposeFactory(t *testing.T) {
	typ := reflect.TypeOf(rungrad.ErrorContext{})
	factoryType := reflect.TypeOf((*rungrad.Factory)(nil))
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).Type == factoryType {
			t.Fatalf("ErrorContext exposes Factory through field %s", typ.Field(i).Name)
		}
	}
	for _, name := range []string{
		"Err", "DefaultExitCode", "ExitCode", "Command", "CommandPath", "Args",
		"Stderr", "Flags", "MachineOutput", "Profile", "ConfigPath",
		"AuthFilePath", "Services", "CredentialSource", "CredentialDisplay",
		"RedactString", "RedactText", "RedactJSON",
	} {
		if _, ok := typ.FieldByName(name); !ok {
			t.Fatalf("ErrorContext missing field %s", name)
		}
	}
}

func TestErrorPolicyNoDuplicateStderr(t *testing.T) {
	policy := &rungrad.ErrorPolicy{
		Render: func(ctx rungrad.ErrorContext) error {
			_, _ = fmt.Fprint(ctx.Stderr, "X")
			return nil
		},
	}
	res := runErrorPolicyApp(errorPolicyApp(false, policy, nil), "boom")
	if res.Exit != rungrad.ExitAPI || res.Stdout != "" || res.Stderr != "X" {
		t.Fatalf("result = %#v", res)
	}
}

func TestErrorPolicyClassifyOnceEquality(t *testing.T) {
	var seen int
	policy := &rungrad.ErrorPolicy{
		Classify: func(ctx rungrad.ErrorContext) int { return 37 },
		Render: func(ctx rungrad.ErrorContext) error {
			seen = ctx.ExitCode
			body := map[string]any{"error": ctx.RedactString(ctx.Err.Error()), "exit_code": ctx.ExitCode}
			b, err := json.Marshal(body)
			if err != nil {
				return err
			}
			_, _ = ctx.Stderr.Write(append(b, '\n'))
			return nil
		},
	}
	res := runErrorPolicyApp(errorPolicyApp(false, policy, nil), "boom", "--json")
	if res.Exit != 37 || seen != 37 || res.Stdout != "" {
		t.Fatalf("result = %#v seen=%d", res, seen)
	}
	body := decodeErrorJSON(t, res.Stderr)
	if body["exit_code"] != float64(res.Exit) {
		t.Fatalf("body = %#v exit=%d", body, res.Exit)
	}
}

func TestErrorPolicyRedactionClosures(t *testing.T) {
	const secret = "closure-secret-value"
	var redacted string
	policy := &rungrad.ErrorPolicy{
		Render: func(ctx rungrad.ErrorContext) error {
			redacted = ctx.RedactString("token=" + secret)
			var text bytes.Buffer
			text.Write(ctx.RedactText([]byte("token=" + secret)))
			_, _ = ctx.Stderr.Write(ctx.RedactJSON([]byte(`{"token":"` + secret + `"}` + "\n")))
			if text.String() != "token=[REDACTED]" {
				return fmt.Errorf("text redaction = %q", text.String())
			}
			return nil
		},
	}
	app := errorPolicyApp(false, policy, nil)
	app.AddCommand(&rungrad.Command{
		Use:   "secretfail",
		Short: "register then fail",
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			f.RegisterSecret(secret)
			return rungrad.NewError(rungrad.ExitAPI, "failed")
		},
	})
	res := runErrorPolicyApp(app, "secretfail")
	if res.Exit != rungrad.ExitAPI || res.Stdout != "" {
		t.Fatalf("result = %#v", res)
	}
	if redacted != "token=[REDACTED]" {
		t.Fatalf("string redaction = %q", redacted)
	}
	if strings.Contains(res.Stderr, secret) {
		t.Fatalf("stderr leaked secret: %q", res.Stderr)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(res.Stderr), &body); err != nil {
		t.Fatalf("stderr JSON: %v\n%s", err, res.Stderr)
	}
	if body["token"] != "[REDACTED]" {
		t.Fatalf("body = %#v", body)
	}
}
