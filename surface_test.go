package rungrad_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/config"
	"github.com/vincentsch/rungrad/docsgen"
	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/output"
	"github.com/vincentsch/rungrad/testutil"
)

func surfaceHostBindings() rungrad.GlobalFlagBindings {
	return rungrad.GlobalFlagBindings{
		JSON:        rungrad.GlobalFlagBinding{Name: "machine-json"},
		DryRun:      rungrad.GlobalFlagBinding{Name: "preview"},
		NoPrompt:    rungrad.GlobalFlagBinding{Name: "silent"},
		Quiet:       rungrad.GlobalFlagBinding{Name: "hush"},
		Config:      rungrad.GlobalFlagBinding{Name: "conf"},
		Profile:     rungrad.GlobalFlagBinding{Name: "prof"},
		AuthFile:    rungrad.GlobalFlagBinding{Name: "creds"},
		Plain:       rungrad.GlobalFlagBinding{Name: "copy"},
		JQ:          rungrad.GlobalFlagBinding{Name: "query"},
		Template:    rungrad.GlobalFlagBinding{Name: "tmpl"},
		IncludeMeta: rungrad.GlobalFlagBinding{Name: "meta"},
		NoColor:     rungrad.GlobalFlagBinding{Name: "mono"},
		NoANSI:      rungrad.GlobalFlagBinding{Name: "raw"},
		NoPager:     rungrad.GlobalFlagBinding{Name: "nopage"},
		Services: map[string]rungrad.GlobalFlagBinding{
			"api": {Name: "api-endpoint"},
		},
	}
}

func surfaceConfig(surface rungrad.SurfaceConfig) rungrad.AppConfig {
	return rungrad.AppConfig{
		Name:           "rgsurface",
		Short:          "surface test",
		Version:        "v9.8.7",
		EnvVar:         "RGSURFACE_TOKEN",
		AdvancedOutput: true,
		Surface:        surface,
		Resolution: &rungrad.ResolutionConfig{
			Profile:  true,
			AuthFile: true,
			Services: []rungrad.Service{
				{Name: "api", Flag: "api-url", EnvVar: "RGSURFACE_API_URL", ConfigKey: "api_url", Default: "https://api.default", Usage: "API URL"},
			},
		},
	}
}

func surfaceHostSurface(bindings rungrad.GlobalFlagBindings) rungrad.SurfaceConfig {
	return rungrad.SurfaceConfig{
		GlobalFlags: rungrad.GlobalFlagSurface{
			Mode:     rungrad.SurfaceHostOwned,
			Bindings: bindings,
		},
	}
}

func newSurfaceApp(surface rungrad.SurfaceConfig, auth rungrad.CredentialResolver) *rungrad.App {
	cfg := surfaceConfig(surface)
	cfg.Auth = auth
	app := rungrad.New(cfg)
	app.AddCommand(
		&rungrad.Command{
			Use:          "list",
			Short:        "list data",
			OutputModes:  []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModePlain, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
			SupportsMeta: true,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				api, _ := f.Service("api")
				f.Infof("surface hint")
				model := map[string]any{"name": "surface", "api": api.Value}
				return f.WriteOutput(rungrad.Output{
					Model: model,
					Meta:  output.Meta{RequestID: "req-surface", Extra: map[string]any{"service_url": api.Value}},
					Human: func(w io.Writer) {
						mode := f.TerminalMode()
						if mode.Color {
							fmt.Fprint(w, "\x1b[31m")
						} else if mode.ANSI {
							fmt.Fprint(w, "\x1b[1m")
						}
						fmt.Fprintln(w, "surface human")
						if mode.ANSI {
							fmt.Fprint(w, "\x1b[0m")
						}
					},
					Plain: func(w io.Writer) { fmt.Fprintln(w, "surface plain") },
				})
			},
		},
		&rungrad.Command{
			Use:         "delete",
			Short:       "delete data",
			Destructive: true,
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				if f.DryRun() {
					return f.WritePreview(output.DryRunPreview{Method: "DELETE", Path: "/surface"})
				}
				if err := f.ConfirmDestructive(rungrad.ConfirmOptions{Action: "delete surface", Target: "alpha"}); err != nil {
					return err
				}
				return f.WriteResult(map[string]string{"deleted": "alpha"}, nil)
			},
		},
		&rungrad.Command{
			Use:         "show",
			Short:       "show resolution",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				api, _ := f.Service("api")
				return f.WriteResult(map[string]any{
					"profile":        f.Profile(),
					"config_path":    f.ConfigPath(),
					"auth_file_path": f.AuthFilePath(),
					"api":            api.Value,
					"api_source":     api.Source.String(),
				}, func(w io.Writer) {})
			},
		},
		&rungrad.Command{
			Use:   "long",
			Short: "long output",
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return f.WriteResult(map[string]string{"ok": "true"}, func(w io.Writer) {
					for i := 0; i < 45; i++ {
						fmt.Fprintf(w, "line %02d\n", i)
					}
				})
			},
		},
	)
	return app
}

func TestDefaultSurfacePreservesFrameworkOwnedBehavior(t *testing.T) {
	app := newSurfaceApp(rungrad.SurfaceConfig{}, nil)
	for _, name := range []string{
		"json", "dry-run", "no-prompt", "quiet", "config",
		"plain", "jq", "template", "include-meta", "no-color", "no-ansi", "no-pager",
		"profile", "auth-file", "api-url",
	} {
		if app.Root().PersistentFlags().Lookup(name) == nil {
			t.Fatalf("default surface missing --%s", name)
		}
	}
	if app.Root().Version != "v9.8.7" {
		t.Fatalf("root.Version = %q", app.Root().Version)
	}
	if res := testutil.Run(app, "__rungrad_manifest"); res.Exit != rungrad.ExitSuccess {
		t.Fatalf("manifest endpoint exit = %d stderr=%q", res.Exit, res.Stderr)
	}
	defer expectPanicContaining(t, "completion")()
	app.AddCommand(&rungrad.Command{Use: "completion", Run: func(*rungrad.Factory, *cobra.Command, []string) error { return nil }})
}

func TestDisabledGlobalsRegisterNoRungradFlagsAndMachineIntentIsInert(t *testing.T) {
	app := newSurfaceApp(rungrad.SurfaceConfig{
		GlobalFlags: rungrad.GlobalFlagSurface{Mode: rungrad.SurfaceDisabled},
	}, nil)
	if got := app.Root().PersistentFlags().FlagUsages(); got != "" {
		t.Fatalf("disabled globals registered flags:\n%s", got)
	}
	var productJSON bool
	app.Root().PersistentFlags().BoolVar(&productJSON, "json", false, "product json")

	for _, args := range [][]string{
		{"--json", "bogus"},
		{"list", "--jq", "."},
		{"list", "--template", "{{.}}"},
	} {
		unknown := testutil.Run(app, args...)
		if unknown.Exit != rungrad.ExitUsage || unknown.Stdout != "" {
			t.Fatalf("disabled globals result for %v = %#v", args, unknown)
		}
		if !strings.HasPrefix(unknown.Stderr, "Error: ") || json.Valid([]byte(unknown.Stderr)) {
			t.Fatalf("disabled globals drove machine error for %v: %q", args, unknown.Stderr)
		}
	}

	out := testutil.Run(app, "list", "--json")
	if out.Exit != rungrad.ExitSuccess || !productJSON {
		t.Fatalf("product json run = %#v productJSON=%t", out, productJSON)
	}
	if out.Stdout != "surface human\n" {
		t.Fatalf("product-local --json drove framework output mode: %q", out.Stdout)
	}
}

func TestHostOwnedGlobalsDriveRuntimeFeatures(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeResolutionConfig(t, cfgPath, config.Config{Version: 1})
	authPath := filepath.Join(dir, "credentials.json")
	app := newSurfaceApp(surfaceHostSurface(surfaceHostBindings()), nil)

	jsonOut := testutil.Run(app, "list", "--machine-json")
	if jsonOut.Exit != rungrad.ExitSuccess || jsonOut.Stderr != "" || !strings.Contains(jsonOut.Stdout, `"name": "surface"`) {
		t.Fatalf("host json result = %#v", jsonOut)
	}
	if strings.Contains(jsonOut.Stdout, "\x1b[") {
		t.Fatalf("host json emitted ANSI: %q", jsonOut.Stdout)
	}

	jq := testutil.Run(app, "list", "--query", ".name")
	if jq.Exit != rungrad.ExitSuccess || strings.TrimSpace(jq.Stdout) != `"surface"` || jq.Stderr != "" {
		t.Fatalf("host jq result = %#v", jq)
	}
	tmpl := testutil.Run(app, "list", "--tmpl", "{{.name}}")
	if tmpl.Exit != rungrad.ExitSuccess || strings.TrimSpace(tmpl.Stdout) != "surface" || tmpl.Stderr != "" {
		t.Fatalf("host template result = %#v", tmpl)
	}
	plain := testutil.Run(app, "list", "--copy")
	if plain.Exit != rungrad.ExitSuccess || plain.Stdout != "surface plain\n" {
		t.Fatalf("host plain result = %#v", plain)
	}
	meta := testutil.Run(app, "list", "--machine-json", "--meta")
	if meta.Exit != rungrad.ExitSuccess || !strings.Contains(meta.Stdout, `"data": {`) || !strings.Contains(meta.Stdout, `"request_id": "req-surface"`) {
		t.Fatalf("host meta result = %#v", meta)
	}
	quiet := testutil.Run(app, "list", "--hush")
	if quiet.Exit != rungrad.ExitSuccess || quiet.Stderr != "" {
		t.Fatalf("host quiet result = %#v", quiet)
	}
	colored := testutil.RunWith(app, testutil.Options{OutputTerminalSet: true, OutputTerminal: true}, "list")
	if !strings.Contains(colored.Stdout, "\x1b[31m") {
		t.Fatalf("terminal color baseline missing color: %#v", colored)
	}
	mono := testutil.RunWith(app, testutil.Options{OutputTerminalSet: true, OutputTerminal: true}, "list", "--mono")
	if strings.Contains(mono.Stdout, "\x1b[31m") || !strings.Contains(mono.Stdout, "\x1b[1m") {
		t.Fatalf("host no-color did not preserve non-color ANSI only: %#v", mono)
	}
	raw := testutil.RunWith(app, testutil.Options{OutputTerminalSet: true, OutputTerminal: true}, "list", "--raw")
	if strings.Contains(raw.Stdout, "\x1b[") {
		t.Fatalf("host no-ansi emitted ANSI: %#v", raw)
	}
	noPrompt := testutil.RunWith(app, testutil.Options{Stdin: failOnRead{t}, TerminalSet: true, Terminal: true}, "delete", "--silent")
	assertExitStdoutEmptyStderrContains(t, noPrompt, rungrad.ExitUsage, "destructive action requires --confirm")
	machineDelete := testutil.RunWith(app, testutil.Options{Stdin: failOnRead{t}, TerminalSet: true, Terminal: true}, "delete", "--machine-json")
	assertExitStdoutEmptyStderrContains(t, machineDelete, rungrad.ExitUsage, "destructive action requires --confirm")
	if !json.Valid([]byte(machineDelete.Stderr)) {
		t.Fatalf("machine delete stderr is not JSON: %q", machineDelete.Stderr)
	}
	preview := testutil.Run(app, "delete", "--preview")
	if preview.Exit != rungrad.ExitSuccess || !strings.Contains(preview.Stdout, "DRY RUN") {
		t.Fatalf("host dry-run result = %#v", preview)
	}
	paged := false
	long := testutil.RunWith(app, testutil.Options{
		OutputTerminalSet: true,
		OutputTerminal:    true,
		TerminalHeight:    func() (int, bool) { return 1, true },
		Pager: rungrad.PagerFunc(func(args []string, content io.Reader, stdout, stderr io.Writer) error {
			paged = true
			return nil
		}),
	}, "long", "--nopage")
	if long.Exit != rungrad.ExitSuccess || paged {
		t.Fatalf("host no-pager result = %#v paged=%t", long, paged)
	}

	resolved := decodeMap(t, testutil.Run(app,
		"show", "--conf", cfgPath, "--prof", "work", "--creds", authPath,
		"--api-endpoint", "https://flag.api", "--machine-json",
	))
	if resolved["profile"] != "work" || resolved["config_path"] != cfgPath ||
		resolved["auth_file_path"] != authPath || resolved["api"] != "https://flag.api" ||
		resolved["api_source"] != "flag" {
		t.Fatalf("host resolution output = %#v", resolved)
	}
}

func TestHostOwnedGlobalsResetBetweenRuns(t *testing.T) {
	app := newSurfaceApp(surfaceHostSurface(surfaceHostBindings()), nil)
	jsonFirst := testutil.Run(app, "list", "--machine-json")
	if jsonFirst.Exit != rungrad.ExitSuccess || !strings.Contains(jsonFirst.Stdout, `"name": "surface"`) {
		t.Fatalf("first json output = %#v", jsonFirst)
	}
	jsonSecond := testutil.Run(app, "list")
	if jsonSecond.Exit != rungrad.ExitSuccess || jsonSecond.Stdout != "surface human\n" || jsonSecond.Stderr != "surface hint\n" {
		t.Fatalf("json machine-output leaked state = %#v", jsonSecond)
	}
	first := testutil.Run(app, "list", "--query", ".name")
	if first.Exit != rungrad.ExitSuccess || strings.TrimSpace(first.Stdout) != `"surface"` {
		t.Fatalf("first transform = %#v", first)
	}
	second := testutil.Run(app, "list")
	if second.Exit != rungrad.ExitSuccess || second.Stdout != "surface human\n" || second.Stderr != "surface hint\n" {
		t.Fatalf("second transform leaked state = %#v", second)
	}
	tmplFirst := testutil.Run(app, "list", "--tmpl", "{{.name}}")
	if tmplFirst.Exit != rungrad.ExitSuccess || strings.TrimSpace(tmplFirst.Stdout) != "surface" {
		t.Fatalf("first template transform = %#v", tmplFirst)
	}
	tmplSecond := testutil.Run(app, "list")
	if tmplSecond.Exit != rungrad.ExitSuccess || tmplSecond.Stdout != "surface human\n" || tmplSecond.Stderr != "surface hint\n" {
		t.Fatalf("template transform leaked state = %#v", tmplSecond)
	}
	nonInteractive := testutil.RunWith(app, testutil.Options{Stdin: failOnRead{t}, TerminalSet: true, Terminal: true}, "delete", "--silent")
	assertExitStdoutEmptyStderrContains(t, nonInteractive, rungrad.ExitUsage, "destructive action requires --confirm")
	interactive := testutil.RunWith(app, testutil.Options{Stdin: strings.NewReader("y\n"), TerminalSet: true, Terminal: true}, "delete")
	if interactive.Exit != rungrad.ExitSuccess {
		t.Fatalf("no-prompt leaked into next run: %#v", interactive)
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeResolutionConfig(t, cfgPath, config.Config{Version: 1})
	authPath := filepath.Join(dir, "credentials.json")
	serviceFirst := decodeMap(t, testutil.Run(app,
		"show", "--conf", cfgPath, "--prof", "work", "--creds", authPath,
		"--api-endpoint", "https://flag.api", "--machine-json",
	))
	serviceSecond := decodeMap(t, testutil.Run(app, "show", "--machine-json"))
	if serviceFirst["api"] != "https://flag.api" || serviceFirst["api_source"] != "flag" ||
		serviceFirst["profile"] != "work" || serviceFirst["config_path"] != cfgPath ||
		serviceFirst["auth_file_path"] != authPath {
		t.Fatalf("first service override = %#v", serviceFirst)
	}
	if serviceSecond["api"] != "https://api.default" || serviceSecond["api_source"] != "builtin" ||
		serviceSecond["profile"] == "work" || serviceSecond["config_path"] == cfgPath ||
		serviceSecond["auth_file_path"] == authPath {
		t.Fatalf("service override leaked = %#v", serviceSecond)
	}
}

func TestHostOwnedRenamedJSONDrivesEarlyMachineErrors(t *testing.T) {
	app := newSurfaceApp(surfaceHostSurface(surfaceHostBindings()), nil)
	for _, args := range [][]string{
		{"--machine-json", "bogus"},
		{"bogus", "--machine-json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			res := testutil.Run(app, args...)
			if res.Exit != rungrad.ExitUsage || res.Stdout != "" {
				t.Fatalf("result = %#v", res)
			}
			var body map[string]any
			if err := json.Unmarshal([]byte(res.Stderr), &body); err != nil {
				t.Fatalf("stderr is not JSON: %v\n%s", err, res.Stderr)
			}
			if body["exit_code"] != float64(rungrad.ExitUsage) {
				t.Fatalf("body = %#v", body)
			}
		})
	}
}

func TestHostOwnedValueShorthandSkipsMachineFlagLookingValue(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(rungrad.GlobalFlagBindings) rungrad.GlobalFlagBindings
		args   []string
	}{
		{
			name: "string global shorthand",
			mutate: func(bindings rungrad.GlobalFlagBindings) rungrad.GlobalFlagBindings {
				bindings.Config.Shorthand = "c"
				return bindings
			},
			args: []string{"-c", "--machine-json", "bogus"},
		},
		{
			name: "service shorthand",
			mutate: func(bindings rungrad.GlobalFlagBindings) rungrad.GlobalFlagBindings {
				bindings.Services["api"] = rungrad.GlobalFlagBinding{Name: "api-endpoint", Shorthand: "a"}
				return bindings
			},
			args: []string{"-a", "--machine-json", "bogus"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			app := newSurfaceApp(surfaceHostSurface(tt.mutate(surfaceHostBindings())), nil)
			res := testutil.Run(app, tt.args...)
			if res.Exit != rungrad.ExitUsage || res.Stdout != "" {
				t.Fatalf("result = %#v", res)
			}
			if json.Valid([]byte(res.Stderr)) || !strings.HasPrefix(res.Stderr, "Error: ") {
				t.Fatalf("machine-looking shorthand value drove JSON error: %q", res.Stderr)
			}
		})
	}
}

func TestHostOwnedBindingValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*rungrad.AppConfig)
		want   string
	}{
		{
			name: "missing applicable",
			mutate: func(cfg *rungrad.AppConfig) {
				cfg.Surface.GlobalFlags.Bindings.Quiet = rungrad.GlobalFlagBinding{}
			},
			want: "missing host binding",
		},
		{
			name: "duplicate long",
			mutate: func(cfg *rungrad.AppConfig) {
				cfg.Surface.GlobalFlags.Bindings.DryRun.Name = "machine-json"
			},
			want: "duplicates",
		},
		{
			name: "duplicate shorthand",
			mutate: func(cfg *rungrad.AppConfig) {
				cfg.Surface.GlobalFlags.Bindings.DryRun.Shorthand = "p"
				cfg.Surface.GlobalFlags.Bindings.NoPrompt.Shorthand = "p"
			},
			want: "shorthand",
		},
		{
			name: "shorthand long collision",
			mutate: func(cfg *rungrad.AppConfig) {
				cfg.Surface.GlobalFlags.Bindings.Config.Name = "p"
				cfg.Surface.GlobalFlags.Bindings.DryRun.Shorthand = "p"
			},
			want: "collides with a long flag name",
		},
		{
			name: "unknown service",
			mutate: func(cfg *rungrad.AppConfig) {
				cfg.Surface.GlobalFlags.Bindings.Services["extra"] = rungrad.GlobalFlagBinding{Name: "extra-url"}
			},
			want: "unknown service binding key",
		},
		{
			name: "disabled advanced feature",
			mutate: func(cfg *rungrad.AppConfig) {
				cfg.AdvancedOutput = false
				cfg.Resolution = nil
				cfg.Surface.GlobalFlags.Bindings = rungrad.GlobalFlagBindings{
					JSON:     rungrad.GlobalFlagBinding{Name: "machine-json"},
					DryRun:   rungrad.GlobalFlagBinding{Name: "preview"},
					NoPrompt: rungrad.GlobalFlagBinding{Name: "silent"},
					Quiet:    rungrad.GlobalFlagBinding{Name: "hush"},
					Config:   rungrad.GlobalFlagBinding{Name: "conf"},
					Plain:    rungrad.GlobalFlagBinding{Name: "copy"},
				}
			},
			want: "provided for a feature that is not enabled",
		},
		{
			name: "disabled profile feature",
			mutate: func(cfg *rungrad.AppConfig) {
				cfg.Resolution.Profile = false
			},
			want: "provided for a feature that is not enabled",
		},
		{
			name: "disabled auth-file feature",
			mutate: func(cfg *rungrad.AppConfig) {
				cfg.Resolution.AuthFile = false
			},
			want: "provided for a feature that is not enabled",
		},
		{
			name: "disabled service feature",
			mutate: func(cfg *rungrad.AppConfig) {
				cfg.Resolution.Services = nil
			},
			want: "unknown service binding key",
		},
		{
			name: "rejected json shorthand",
			mutate: func(cfg *rungrad.AppConfig) {
				cfg.Surface.GlobalFlags.Bindings.JSON.Shorthand = "j"
			},
			want: "cannot define a shorthand",
		},
		{
			name: "rejected jq shorthand",
			mutate: func(cfg *rungrad.AppConfig) {
				cfg.Surface.GlobalFlags.Bindings.JQ.Shorthand = "q"
			},
			want: "cannot define a shorthand",
		},
		{
			name: "rejected template shorthand",
			mutate: func(cfg *rungrad.AppConfig) {
				cfg.Surface.GlobalFlags.Bindings.Template.Shorthand = "t"
			},
			want: "cannot define a shorthand",
		},
		{
			name: "hidden empty",
			mutate: func(cfg *rungrad.AppConfig) {
				cfg.Surface.GlobalFlags.Bindings.JSON = rungrad.GlobalFlagBinding{Hidden: true}
			},
			want: "hidden host binding",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := surfaceConfig(surfaceHostSurface(surfaceHostBindings()))
			tt.mutate(&cfg)
			defer expectPanicContaining(t, tt.want)()
			rungrad.New(cfg)
		})
	}

	app := newSurfaceApp(surfaceHostSurface(surfaceHostBindings()), nil)
	fs := pflag.NewFlagSet("external", pflag.ContinueOnError)
	fs.Bool("machine-json", false, "existing")
	if err := app.BindGlobalFlags(fs, surfaceHostBindings()); err == nil || !strings.Contains(err.Error(), "collides with an existing flag") {
		t.Fatalf("BindGlobalFlags collision error = %v", err)
	}

	normalized := pflag.NewFlagSet("normalized", pflag.ContinueOnError)
	normalized.SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		return pflag.NormalizedName(strings.ReplaceAll(name, "_", "-"))
	})
	bindings := surfaceHostBindings()
	bindings.Config.Name = "conf-path"
	bindings.Profile.Name = "conf_path"
	if err := app.BindGlobalFlags(normalized, bindings); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("BindGlobalFlags normalized duplicate error = %v", err)
	}
}

func TestHiddenHostBoundGlobalDrivesRuntimeButNotProjection(t *testing.T) {
	bindings := surfaceHostBindings()
	bindings.JQ.Hidden = true
	app := newSurfaceApp(surfaceHostSurface(bindings), nil)
	res := testutil.Run(app, "list", "--query", ".name")
	if res.Exit != rungrad.ExitSuccess || strings.TrimSpace(res.Stdout) != `"surface"` {
		t.Fatalf("hidden host query result = %#v", res)
	}
	m, _ := readManifest(t, app)
	if flag := findManifestFlag(m.GlobalFlags, "query"); flag != nil {
		t.Fatalf("hidden host flag appeared in manifest: %+v", *flag)
	}
	if index := docsgen.Generate(app)["index.md"]; strings.Contains(index, "--query") {
		t.Fatalf("hidden host flag appeared in docs index:\n%s", index)
	}
}

func TestVersionSurfaceOwnership(t *testing.T) {
	rungradOwned := newSurfaceApp(rungrad.SurfaceConfig{}, nil)
	res := testutil.Run(rungradOwned, "--version")
	if res.Exit != rungrad.ExitSuccess || !strings.Contains(res.Stdout, "v9.8.7") {
		t.Fatalf("rungrad-owned version result = %#v", res)
	}

	for _, mode := range []rungrad.SurfaceMode{rungrad.SurfaceHostOwned, rungrad.SurfaceDisabled} {
		t.Run(string(mode), func(t *testing.T) {
			app := newSurfaceApp(rungrad.SurfaceConfig{Version: mode}, nil)
			res := testutil.Run(app, "--version")
			assertExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, "unknown flag: --version")
			m, _ := readManifest(t, app)
			if m.ToolVersion != "v9.8.7" {
				t.Fatalf("manifest tool_version = %q", m.ToolVersion)
			}
		})
	}
}

func TestCompletionSurfaceOwnership(t *testing.T) {
	rungradOwned := newSurfaceApp(rungrad.SurfaceConfig{}, nil)
	defer expectPanicContaining(t, "completion")()
	rungradOwned.AddCommand(&rungrad.Command{Use: "completion", Run: func(*rungrad.Factory, *cobra.Command, []string) error { return nil }})
}

func TestRungradOwnedCompletionExcludedFromManifest(t *testing.T) {
	app := newSurfaceApp(rungrad.SurfaceConfig{}, nil)
	m, _ := readManifest(t, app)
	for _, cmd := range m.Commands {
		if len(cmd.Path) > 0 && cmd.Path[0] == "completion" {
			t.Fatalf("framework completion appeared in manifest: %+v", cmd)
		}
	}
}

func TestRungradOwnedCompletionStaysFilteredAfterExecute(t *testing.T) {
	app := newSurfaceApp(rungrad.SurfaceConfig{}, nil)
	if res := testutil.Run(app, "list"); res.Exit != rungrad.ExitSuccess {
		t.Fatalf("initial run = %#v", res)
	}
	docs := docsgen.Generate(app)
	if _, ok := docs["rgsurface_completion.md"]; ok {
		t.Fatalf("framework completion docs leaked after execute; got %v", surfaceKeys(docs))
	}
	if strings.Contains(docs["index.md"], "rgsurface completion") {
		t.Fatalf("framework completion leaked into docs index:\n%s", docs["index.md"])
	}
	for path := range testutil.CaptureAllHelp(app) {
		if strings.HasPrefix(path, "completion") {
			t.Fatalf("framework completion leaked into captured help as %q", path)
		}
	}
}

type surfaceStaticModule struct {
	commands []*rungrad.Command
	specs    []rungrad.CommandSpec
}

func (m surfaceStaticModule) Groups() []rungrad.Group        { return nil }
func (m surfaceStaticModule) Commands() []*rungrad.Command   { return m.commands }
func (m surfaceStaticModule) Catalog() []rungrad.CommandSpec { return m.specs }

func TestHostOwnedCompletionProjectsAndValidates(t *testing.T) {
	app := rungrad.New(surfaceConfig(rungrad.SurfaceConfig{Completion: rungrad.SurfaceHostOwned}))
	completion := &rungrad.Command{Use: "completion", Short: "shell completion"}
	completion.AddCommand(&rungrad.Command{
		Use:   "bash",
		Short: "bash completion",
		Run:   func(*rungrad.Factory, *cobra.Command, []string) error { return nil },
	})
	app.AddModule(surfaceStaticModule{
		commands: []*rungrad.Command{completion},
		specs: []rungrad.CommandSpec{
			{Path: "completion", Summary: "shell completion"},
			{Path: "completion bash", Summary: "bash completion"},
		},
	})
	if err := app.ValidateCatalog(); err != nil {
		t.Fatalf("ValidateCatalog() = %v", err)
	}
	m, _ := readManifest(t, app)
	if findManifestCommand(&m, "completion") == nil || findManifestCommand(&m, "completion", "bash") == nil {
		t.Fatalf("host completion missing from manifest: %+v", m.Commands)
	}
	docs := docsgen.Generate(app)
	if _, ok := docs["rgsurface_completion.md"]; !ok {
		t.Fatalf("host completion docs missing; got %v", surfaceKeys(docs))
	}
}

func TestDisabledCompletionReservedAndAbsent(t *testing.T) {
	app := newSurfaceApp(rungrad.SurfaceConfig{Completion: rungrad.SurfaceDisabled}, nil)
	assertExitStdoutEmptyStderrContains(t, testutil.Run(app, "completion", "bash"), rungrad.ExitUsage, "unknown command")
	defer expectPanicContaining(t, "completion")()
	app.AddCommand(&rungrad.Command{Use: "completion", Run: func(*rungrad.Factory, *cobra.Command, []string) error { return nil }})
}

func TestManifestEndpointModes(t *testing.T) {
	disabled := newSurfaceApp(rungrad.SurfaceConfig{Manifest: rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointDisabled}}, nil)
	res := testutil.Run(disabled, "__rungrad_manifest")
	assertExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, "unknown command")

	renamed := newSurfaceApp(rungrad.SurfaceConfig{Manifest: rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointRenamed, Name: "_surface_manifest"}}, nil)
	res = testutil.Run(renamed, "__rungrad_manifest")
	assertExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, "unknown command")
	renamedRes := testutil.Run(renamed, "_surface_manifest")
	if renamedRes.Exit != rungrad.ExitSuccess {
		t.Fatalf("renamed manifest exit = %d stderr=%q", renamedRes.Exit, renamedRes.Stderr)
	}
	var renamedDoc manifest.Manifest
	testutil.AssertStableJSON(t, renamedRes.Stdout, &renamedDoc)
	if err := manifest.Validate(&renamedDoc); err != nil {
		t.Fatalf("renamed manifest Validate = %v", err)
	}

	hostRendered := newSurfaceApp(rungrad.SurfaceConfig{Manifest: rungrad.ManifestEndpointSurface{
		Mode: rungrad.ManifestEndpointHostRendered,
		Render: func(ctx rungrad.ManifestEndpointContext) error {
			if ctx.Manifest.ToolName != "rgsurface" || ctx.Command == nil {
				return fmt.Errorf("bad context")
			}
			_, _ = fmt.Fprint(ctx.Stdout, "host manifest\n")
			return nil
		},
	}}, nil)
	hostRes := testutil.Run(hostRendered, "__rungrad_manifest")
	if hostRes.Exit != rungrad.ExitSuccess || hostRes.Stdout != "host manifest\n" || hostRes.Stderr != "" {
		t.Fatalf("host-rendered manifest result = %#v", hostRes)
	}
	failing := newSurfaceApp(rungrad.SurfaceConfig{Manifest: rungrad.ManifestEndpointSurface{
		Mode: rungrad.ManifestEndpointHostRendered,
		Render: func(ctx rungrad.ManifestEndpointContext) error {
			_, _ = fmt.Fprint(ctx.Stdout, "partial")
			return errors.New("host render failed")
		},
	}}, nil)
	failRes := testutil.Run(failing, "__rungrad_manifest")
	assertExitStdoutEmptyStderrContains(t, failRes, rungrad.ExitAPI, "host render failed")
}

func TestManifestEndpointValidationPanics(t *testing.T) {
	tests := []struct {
		name    string
		surface rungrad.ManifestEndpointSurface
		want    string
	}{
		{"rungrad name", rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointRungradOwned, Name: "x"}, "does not accept"},
		{"rungrad render", rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointRungradOwned, Render: func(rungrad.ManifestEndpointContext) error { return nil }}, "does not accept"},
		{"disabled name", rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointDisabled, Name: "x"}, "does not accept"},
		{"disabled render", rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointDisabled, Render: func(rungrad.ManifestEndpointContext) error { return nil }}, "does not accept"},
		{"renamed empty", rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointRenamed}, "requires Name"},
		{"renamed default", rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointRenamed, Name: "__rungrad_manifest"}, "default name"},
		{"renamed help", rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointRenamed, Name: "help"}, "reserved"},
		{"renamed completion", rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointRenamed, Name: "completion"}, "reserved"},
		{"renamed whitespace", rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointRenamed, Name: "two words"}, "single command token"},
		{"renamed render", rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointRenamed, Name: "custom", Render: func(rungrad.ManifestEndpointContext) error { return nil }}, "does not accept"},
		{"host rendered nil", rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointHostRendered}, "requires Render"},
		{"host rendered name", rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointHostRendered, Name: "custom", Render: func(rungrad.ManifestEndpointContext) error { return nil }}, "does not accept"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer expectPanicContaining(t, tt.want)()
			rungrad.New(surfaceConfig(rungrad.SurfaceConfig{Manifest: tt.surface}))
		})
	}
}

func TestManifestEndpointReservationByMode(t *testing.T) {
	for _, tc := range []struct {
		name    string
		surface rungrad.SurfaceConfig
		use     string
		panics  bool
	}{
		{name: "default reserved", surface: rungrad.SurfaceConfig{}, use: "__rungrad_manifest", panics: true},
		{name: "host-rendered reserved", surface: rungrad.SurfaceConfig{Manifest: rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointHostRendered, Render: func(rungrad.ManifestEndpointContext) error { return nil }}}, use: "__rungrad_manifest", panics: true},
		{name: "renamed reserves custom", surface: rungrad.SurfaceConfig{Manifest: rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointRenamed, Name: "_surface_manifest"}}, use: "_surface_manifest", panics: true},
		{name: "renamed frees default", surface: rungrad.SurfaceConfig{Manifest: rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointRenamed, Name: "_surface_manifest"}}, use: "__rungrad_manifest"},
		{name: "disabled frees default", surface: rungrad.SurfaceConfig{Manifest: rungrad.ManifestEndpointSurface{Mode: rungrad.ManifestEndpointDisabled}}, use: "__rungrad_manifest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newSurfaceApp(tc.surface, nil)
			if tc.panics {
				defer expectPanicContaining(t, tc.use)()
			}
			app.AddCommand(&rungrad.Command{Use: tc.use, Run: func(*rungrad.Factory, *cobra.Command, []string) error { return nil }})
			if tc.panics {
				t.Fatal("AddCommand did not panic")
			}
		})
	}
}

func TestManifestDocumentExportedBuilder(t *testing.T) {
	for _, app := range []*rungrad.App{
		newSurfaceApp(rungrad.SurfaceConfig{}, nil),
		newSurfaceApp(surfaceHostSurface(surfaceHostBindings()), nil),
	} {
		doc := app.ManifestDocument()
		if err := manifest.Validate(&doc); err != nil {
			t.Fatalf("ManifestDocument Validate = %v", err)
		}
		if doc.ToolName != "rgsurface" || doc.ToolVersion != "v9.8.7" {
			t.Fatalf("ManifestDocument identity = %+v", doc)
		}
	}
}

func expectPanicContaining(t *testing.T, want string) func() {
	t.Helper()
	return func() {
		t.Helper()
		r := recover()
		if r == nil {
			t.Fatalf("expected panic containing %q", want)
		}
		if !strings.Contains(fmt.Sprint(r), want) {
			t.Fatalf("panic = %v, want containing %q", r, want)
		}
	}
}

func surfaceKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSurfaceConfigTypeShape(t *testing.T) {
	typ := reflect.TypeOf(rungrad.SurfaceConfig{})
	for _, name := range []string{"GlobalFlags", "Version", "Completion", "Manifest"} {
		if _, ok := typ.FieldByName(name); !ok {
			t.Fatalf("SurfaceConfig missing field %s", name)
		}
	}
}
