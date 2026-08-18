package rungrad_test

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/output"
	"github.com/vincentsch/rungrad/resolve"
	"github.com/vincentsch/rungrad/testutil"
)

// advancedOutputProbe lets guard tests prove a rejected invocation returned
// before the command handler ran.
type advancedOutputProbe struct {
	readRan    bool
	humanRan   bool
	plainRan   bool
	claimedRan bool
	previewRan bool
}

// newAdvancedOutputApp builds one command tree that exercises the advanced
// output contract. Passing advanced=false keeps the same commands but omits the
// opt-in flags, which makes legacy behavior comparable in the tests.
func newAdvancedOutputApp(advanced bool, probe *advancedOutputProbe) *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:           "rgadvanced",
		Short:          "advanced output test",
		AdvancedOutput: advanced,
	})
	model := map[string]any{
		"a":      1,
		"b":      2,
		"field":  "alpha",
		"nested": map[string]any{"amp": "a&b", "z": "last"},
	}
	app.AddCommand(
		&rungrad.Command{
			Use:         "read",
			Short:       "read data",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				if probe != nil {
					probe.readRan = true
				}
				return f.WriteResult(model, func(w io.Writer) {
					mode := f.TerminalMode()
					if mode.ANSI {
						fmt.Fprint(w, "\x1b[1m")
					}
					fmt.Fprintln(w, "alpha human")
					if mode.ANSI {
						fmt.Fprint(w, "\x1b[0m")
					}
				})
			},
		},
		&rungrad.Command{
			Use:         "copy",
			Short:       "copy data",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModePlain},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				if probe != nil {
					probe.plainRan = true
				}
				summary := output.MutationSummary{Action: "Copied", Resource: "item", Name: "alpha"}
				return f.WriteOutput(rungrad.Output{
					Model: summary,
					Human: func(w io.Writer) {
						output.RenderMutationMode(w, summary, f.TerminalMode())
					},
					Plain: func(w io.Writer) {
						fmt.Fprintln(w, "item alpha")
					},
				})
			},
		},
		&rungrad.Command{
			Use:         "human",
			Short:       "human only",
			OutputModes: []string{rungrad.OutputModeHuman},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				if probe != nil {
					probe.humanRan = true
				}
				fmt.Fprintln(f.Stdout, "human only")
				return nil
			},
		},
		&rungrad.Command{
			Use:         "claimed-plain",
			Short:       "claims plain but lacks renderer",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModePlain},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				if probe != nil {
					probe.claimedRan = true
				}
				return f.WriteResult(map[string]string{"ok": "true"}, func(w io.Writer) {
					fmt.Fprintln(w, "human fallback")
				})
			},
		},
		&rungrad.Command{
			Use:         "preview",
			Short:       "preview data",
			Mutates:     true,
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModePlain, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				if probe != nil {
					probe.previewRan = true
				}
				if f.DryRun() {
					return f.WritePreview(output.DryRunPreview{
						Method: "POST",
						Path:   "/widgets",
						Body:   []output.Field{{Name: "name", Value: "demo"}},
					})
				}
				return f.WriteResult(map[string]string{"created": "demo"}, func(w io.Writer) {
					fmt.Fprintln(w, "created demo")
				})
			},
		},
		&rungrad.Command{
			Use:         "hint",
			Short:       "hint data",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				f.Infof("human-only hint")
				return f.WriteResult(map[string]string{"field": "alpha"}, func(w io.Writer) {
					fmt.Fprintln(w, "alpha")
				})
			},
		},
		&rungrad.Command{
			Use:         "mode",
			Short:       "mode data",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				mode := f.TerminalMode()
				return f.WriteResult(map[string]bool{"ansi": mode.ANSI, "color": mode.Color, "sanitize": mode.Sanitize}, nil)
			},
		},
		&rungrad.Command{
			Use:         "resolve <name>",
			Short:       "resolve data",
			Args:        cobra.ExactArgs(1),
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				id, err := f.Resolve(args[0], func(string) ([]resolve.Match, error) {
					return []resolve.Match{{ID: "2", Name: "dup"}, {ID: "1", Name: "dup"}}, nil
				}, resolve.Options{ResourceType: "item", AllowPrompt: true})
				if err != nil {
					return err
				}
				return f.WriteResult(map[string]string{"id": id}, nil)
			},
		},
		&rungrad.Command{
			Use:         "destroy",
			Short:       "destroy data",
			Destructive: true,
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				if err := f.ConfirmDestructive(rungrad.ConfirmOptions{Action: "destroy item", Target: "alpha"}); err != nil {
					return err
				}
				return f.WriteResult(map[string]string{"destroyed": "alpha"}, nil)
			},
		},
		&rungrad.Command{
			Use:         "big",
			Short:       "big integer data",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return f.WriteResult(map[string]int64{"id": 9007199254740993}, nil)
			},
		},
		&rungrad.Command{
			Use:         "raw",
			Short:       "raw terminal bytes",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModePlain, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				model := map[string]string{"name": "Bad\x1b[31mName"}
				return f.WriteOutput(rungrad.Output{
					Model: model,
					Human: func(w io.Writer) {
						fmt.Fprintln(w, "Bad\x1b[31mName")
					},
					Plain: func(w io.Writer) {
						fmt.Fprintln(w, "Plain\x1b[31mName")
					},
				})
			},
		},
		&rungrad.Command{
			Use:         "long",
			Short:       "long human output",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModePlain, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				model := map[string]string{"ok": "true"}
				return f.WriteOutput(rungrad.Output{
					Model: model,
					Human: func(w io.Writer) {
						for i := 1; i <= 45; i++ {
							fmt.Fprintf(w, "line %02d\n", i)
						}
					},
					Plain: func(w io.Writer) {
						for i := 1; i <= 45; i++ {
							fmt.Fprintf(w, "plain %02d\n", i)
						}
					},
				})
			},
		},
	)
	return app
}

func TestAdvancedOutputFlagRegistration(t *testing.T) {
	advanced := newAdvancedOutputApp(true, nil)
	for _, name := range []string{"plain", "jq", "template", "include-meta", "no-color", "no-ansi", "no-pager"} {
		if advanced.Root().PersistentFlags().Lookup(name) == nil {
			t.Fatalf("advanced app missing --%s", name)
		}
	}
	plain := newAdvancedOutputApp(false, nil)
	for _, name := range []string{"plain", "jq", "template", "include-meta", "no-color", "no-ansi", "no-pager"} {
		if plain.Root().PersistentFlags().Lookup(name) != nil {
			t.Fatalf("non-advanced app unexpectedly registered --%s", name)
		}
	}
}

func TestAdvancedOutputConflictValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"jq template", []string{"read", "--jq", ".", "--template", "{{.}}"}, "--jq and --template cannot be combined"},
		{"plain json", []string{"read", "--plain", "--json"}, "--plain cannot be combined"},
		{"plain jq", []string{"read", "--plain", "--jq", "."}, "--plain cannot be combined"},
		{"plain template", []string{"read", "--plain", "--template", "{{.field}}"}, "--plain cannot be combined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := testutil.Run(newAdvancedOutputApp(true, nil), tt.args...)
			assertExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, tt.want)
		})
	}
}

func TestAdvancedOutputCapabilityGuardRunsBeforeHandler(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"transform refused", []string{"human", "--jq", "."}, "does not support --jq or --template"},
		{"plain refused", []string{"human", "--plain"}, "does not support --plain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := &advancedOutputProbe{}
			res := testutil.Run(newAdvancedOutputApp(true, probe), tt.args...)
			assertExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, tt.want)
			if probe.humanRan {
				t.Fatalf("handler ran for %s", tt.name)
			}
		})
	}
}

func TestAdvancedOutputJQSuccessAndDeterminism(t *testing.T) {
	app := newAdvancedOutputApp(true, nil)
	first := testutil.Run(app, "read", "--jq", ".field")
	if first.Exit != rungrad.ExitSuccess {
		t.Fatalf("first exit %d stderr=%q", first.Exit, first.Stderr)
	}
	if first.Stdout != "\"alpha\"\n" {
		t.Fatalf("jq stdout = %q", first.Stdout)
	}
	second := testutil.Run(app, "read", "--jq", ".field")
	if second.Exit != rungrad.ExitSuccess {
		t.Fatalf("second exit %d stderr=%q", second.Exit, second.Stderr)
	}
	if first.Stdout != second.Stdout {
		t.Fatalf("jq output not repeatable: %q vs %q", first.Stdout, second.Stdout)
	}
}

func TestAdvancedOutputTemplateSuccess(t *testing.T) {
	res := testutil.Run(newAdvancedOutputApp(true, nil), "read", "--template", "{{.field}}:{{.a}}")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if res.Stdout != "alpha:1\n" {
		t.Fatalf("template stdout = %q", res.Stdout)
	}
}

func TestAdvancedOutputInvalidTransformsRunBeforeHandler(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"jq", []string{"read", "--jq", "bad("}, "invalid --jq expression:"},
		{"template", []string{"read", "--template", "{{."}, "invalid --template:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := &advancedOutputProbe{}
			res := testutil.Run(newAdvancedOutputApp(true, probe), tt.args...)
			assertExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, tt.want)
			if probe.readRan {
				t.Fatalf("handler ran for invalid %s", tt.name)
			}
		})
	}
}

func TestAdvancedOutputTransformRuntimeFailures(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"jq", []string{"read", "--jq", `error("x")`}, "--jq expression failed:"},
		{"template", []string{"read", "--template", "{{.missing}}"}, "--template rendering failed:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := testutil.Run(newAdvancedOutputApp(true, nil), tt.args...)
			assertExitStdoutEmptyStderrContains(t, res, rungrad.ExitAPI, tt.want)
		})
	}
}

func TestAdvancedOutputPlainRequiresExplicitRenderer(t *testing.T) {
	res := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
	}, "copy", "--plain")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if res.Stdout != "item alpha\n" {
		t.Fatalf("plain stdout = %q", res.Stdout)
	}
	if strings.Contains(res.Stdout, "\x1b[") {
		t.Fatalf("plain stdout contains ANSI escapes: %q", res.Stdout)
	}

	missing := testutil.Run(newAdvancedOutputApp(true, nil), "claimed-plain", "--plain")
	assertExitStdoutEmptyStderrContains(t, missing, rungrad.ExitAPI, "plain output renderer is not configured")
}

func TestAdvancedTerminalFlagsAndSanitization(t *testing.T) {
	coloredMutation := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
	}, "copy")
	if coloredMutation.Exit != rungrad.ExitSuccess {
		t.Fatalf("colored mutation exit %d stderr=%q", coloredMutation.Exit, coloredMutation.Stderr)
	}
	if !strings.Contains(coloredMutation.Stdout, "\x1b[1;32mCopied\x1b[0m") {
		t.Fatalf("terminal mutation missing color: %q", coloredMutation.Stdout)
	}

	noColorMutation := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
	}, "copy", "--no-color")
	if noColorMutation.Exit != rungrad.ExitSuccess {
		t.Fatalf("no-color mutation exit %d stderr=%q", noColorMutation.Exit, noColorMutation.Stderr)
	}
	if noColorMutation.Stdout != "Copied item alpha\n" || strings.Contains(noColorMutation.Stdout, "\x1b") {
		t.Fatalf("no-color mutation stdout = %q", noColorMutation.Stdout)
	}

	noANSIMutation := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
	}, "copy", "--no-ansi")
	if noANSIMutation.Exit != rungrad.ExitSuccess {
		t.Fatalf("no-ansi mutation exit %d stderr=%q", noANSIMutation.Exit, noANSIMutation.Stderr)
	}
	if noANSIMutation.Stdout != "Copied item alpha\n" || strings.Contains(noANSIMutation.Stdout, "\x1b") {
		t.Fatalf("no-ansi mutation stdout = %q", noANSIMutation.Stdout)
	}

	coloredPreview := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
	}, "preview", "--dry-run")
	if coloredPreview.Exit != rungrad.ExitSuccess {
		t.Fatalf("colored preview exit %d stderr=%q", coloredPreview.Exit, coloredPreview.Stderr)
	}
	if !strings.Contains(coloredPreview.Stdout, "\x1b[1;33mDRY RUN\x1b[0m") {
		t.Fatalf("terminal preview missing color: %q", coloredPreview.Stdout)
	}

	noColorPreview := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
	}, "preview", "--dry-run", "--no-color")
	if noColorPreview.Exit != rungrad.ExitSuccess {
		t.Fatalf("no-color preview exit %d stderr=%q", noColorPreview.Exit, noColorPreview.Stderr)
	}
	if strings.Contains(noColorPreview.Stdout, "\x1b") {
		t.Fatalf("no-color preview emitted escapes: %q", noColorPreview.Stdout)
	}

	noANSIPreview := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
	}, "preview", "--dry-run", "--no-ansi")
	if noANSIPreview.Exit != rungrad.ExitSuccess {
		t.Fatalf("no-ansi preview exit %d stderr=%q", noANSIPreview.Exit, noANSIPreview.Stderr)
	}
	if strings.Contains(noANSIPreview.Stdout, "\x1b") {
		t.Fatalf("no-ansi preview emitted escapes: %q", noANSIPreview.Stdout)
	}

	noANSI := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
	}, "raw", "--no-ansi")
	if noANSI.Exit != rungrad.ExitSuccess {
		t.Fatalf("no-ansi exit %d stderr=%q", noANSI.Exit, noANSI.Stderr)
	}
	if noANSI.Stdout != "Bad Name\n" {
		t.Fatalf("no-ansi stdout = %q", noANSI.Stdout)
	}

	nonTerminal := testutil.Run(newAdvancedOutputApp(true, nil), "raw")
	if nonTerminal.Exit != rungrad.ExitSuccess {
		t.Fatalf("non-terminal raw exit %d stderr=%q", nonTerminal.Exit, nonTerminal.Stderr)
	}
	if nonTerminal.Stdout != "Bad Name\n" {
		t.Fatalf("non-terminal sanitized stdout = %q", nonTerminal.Stdout)
	}

	plain := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
	}, "raw", "--plain")
	if plain.Exit != rungrad.ExitSuccess {
		t.Fatalf("plain raw exit %d stderr=%q", plain.Exit, plain.Stderr)
	}
	if plain.Stdout != "Plain Name\n" {
		t.Fatalf("plain raw stdout = %q", plain.Stdout)
	}

	tmpl := testutil.Run(newAdvancedOutputApp(true, nil), "raw", "--template", "{{.name}}")
	if tmpl.Exit != rungrad.ExitSuccess {
		t.Fatalf("template raw exit %d stderr=%q", tmpl.Exit, tmpl.Stderr)
	}
	if tmpl.Stdout != "Bad Name\n" {
		t.Fatalf("template raw stdout = %q", tmpl.Stdout)
	}

	jq := testutil.Run(newAdvancedOutputApp(true, nil), "raw", "--jq", ".name")
	if jq.Exit != rungrad.ExitSuccess {
		t.Fatalf("jq raw exit %d stderr=%q", jq.Exit, jq.Stderr)
	}
	if strings.Contains(jq.Stdout, "\x1b") || !strings.Contains(jq.Stdout, `\u001b`) {
		t.Fatalf("jq output should be escaped JSON, got %q", jq.Stdout)
	}
}

func TestAdvancedOutputNonAdvancedAppStaysCompact(t *testing.T) {
	app := newAdvancedOutputApp(false, nil)
	json := testutil.Run(app, "read", "--json")
	if json.Exit != rungrad.ExitSuccess {
		t.Fatalf("json exit %d stderr=%q", json.Exit, json.Stderr)
	}
	if !strings.Contains(json.Stdout, "\"field\": \"alpha\"") {
		t.Fatalf("json stdout = %q", json.Stdout)
	}
	human := testutil.Run(app, "read")
	if human.Exit != rungrad.ExitSuccess {
		t.Fatalf("human exit %d stderr=%q", human.Exit, human.Stderr)
	}
	if human.Stdout != "alpha human\n" {
		t.Fatalf("human stdout = %q", human.Stdout)
	}
	unknown := testutil.Run(app, "read", "--jq", ".")
	assertExitStdoutEmptyStderrContains(t, unknown, rungrad.ExitUsage, "unknown flag: --jq")
	for _, flag := range []string{"--no-color", "--no-ansi", "--no-pager"} {
		res := testutil.Run(app, "read", flag)
		assertExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, "unknown flag: "+flag)
	}
}

func TestAdvancedPagerPolicy(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		opts       testutil.Options
		wantPager  bool
		wantStdout string
	}{
		{
			name:      "human terminal long output pages",
			args:      []string{"long"},
			opts:      pagerOptions(1, "pager -x"),
			wantPager: true,
		},
		{
			name:       "human output fitting height does not page",
			args:       []string{"long"},
			opts:       pagerOptions(100, "pager -x"),
			wantStdout: longHumanOutput(),
		},
		{
			name:       "json never pages",
			args:       []string{"long", "--json"},
			opts:       pagerOptions(1, "pager -x"),
			wantStdout: "{\n  \"ok\": \"true\"\n}\n",
		},
		{
			name:       "jq never pages",
			args:       []string{"long", "--jq", ".ok"},
			opts:       pagerOptions(1, "pager -x"),
			wantStdout: "\"true\"\n",
		},
		{
			name:       "template never pages",
			args:       []string{"long", "--template", "{{.ok}}"},
			opts:       pagerOptions(1, "pager -x"),
			wantStdout: "true\n",
		},
		{
			name:       "plain never pages",
			args:       []string{"long", "--plain"},
			opts:       pagerOptions(1, "pager -x"),
			wantStdout: longPlainOutput(),
		},
		{
			name: "non-terminal stdout never pages",
			args: []string{"long"},
			opts: testutil.Options{
				TerminalHeight: func() (int, bool) { return 1, true },
				Pager:          &recordingPager{},
				LookupEnv:      pagerLookup("pager -x"),
			},
			wantStdout: longHumanOutput(),
		},
		{
			name:       "no-pager disables pager",
			args:       []string{"long", "--no-pager"},
			opts:       pagerOptions(1, "pager -x"),
			wantStdout: longHumanOutput(),
		},
		{
			name:       "no-ansi disables pager",
			args:       []string{"long", "--no-ansi"},
			opts:       pagerOptions(1, "pager -x"),
			wantStdout: longHumanOutput(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pager := tt.opts.Pager.(*recordingPager)
			if tt.wantPager {
				pager.write = "pager displayed\n"
				tt.wantStdout = "pager displayed\n"
			}
			res := testutil.RunWith(newAdvancedOutputApp(true, nil), tt.opts, tt.args...)
			if res.Exit != rungrad.ExitSuccess {
				t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
			}
			if tt.wantPager {
				if pager.calls != 1 {
					t.Fatalf("pager calls = %d, want 1", pager.calls)
				}
				if pager.content != longHumanOutput() {
					t.Fatalf("pager content = %q", pager.content)
				}
			} else if pager.calls != 0 {
				t.Fatalf("pager calls = %d, want 0", pager.calls)
			}
			if res.Stdout != tt.wantStdout {
				t.Fatalf("stdout = %q, want %q", res.Stdout, tt.wantStdout)
			}
		})
	}
}

func TestCompactAppNeverPagesLongHumanOutput(t *testing.T) {
	pager := &recordingPager{}
	res := testutil.RunWith(newAdvancedOutputApp(false, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
		TerminalHeight:    func() (int, bool) { return 1, true },
		Pager:             pager,
		LookupEnv:         pagerLookup("pager -x"),
	}, "long")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if pager.calls != 0 {
		t.Fatalf("compact app invoked pager %d times", pager.calls)
	}
	if res.Stdout != longHumanOutput() {
		t.Fatalf("compact stdout = %q", res.Stdout)
	}
}

func TestAdvancedPagerFailureFallsBackToStdout(t *testing.T) {
	pager := &recordingPager{err: errors.New("pager start failed")}
	res := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
		TerminalHeight:    func() (int, bool) { return 1, true },
		Pager:             pager,
		LookupEnv:         pagerLookup("pager -x"),
	}, "long")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if pager.calls != 1 {
		t.Fatalf("pager calls = %d, want 1", pager.calls)
	}
	if res.Stdout != longHumanOutput() {
		t.Fatalf("fallback stdout = %q", res.Stdout)
	}
}

func TestAdvancedPagerCommandSelection(t *testing.T) {
	tests := []struct {
		name     string
		lookup   func(string) (string, bool)
		wantArgs []string
		wantPage bool
	}{
		{
			name: "tool env wins",
			lookup: func(name string) (string, bool) {
				switch name {
				case "RGADVANCED_PAGER":
					return "toolpager --raw ; rm", true
				case "PAGER":
					return "fallback", true
				default:
					return "", false
				}
			},
			wantArgs: []string{"toolpager", "--raw", ";", "rm"},
			wantPage: true,
		},
		{
			name: "blank tool env falls through to pager",
			lookup: func(name string) (string, bool) {
				switch name {
				case "RGADVANCED_PAGER":
					return "   ", true
				case "PAGER":
					return "fallback -p", true
				default:
					return "", false
				}
			},
			wantArgs: []string{"fallback", "-p"},
			wantPage: true,
		},
		{
			name: "pager env",
			lookup: func(name string) (string, bool) {
				if name == "PAGER" {
					return "envpager -F", true
				}
				return "", false
			},
			wantArgs: []string{"envpager", "-F"},
			wantPage: true,
		},
		{
			name: "blank pager env disables default",
			lookup: func(name string) (string, bool) {
				if name == "PAGER" {
					return "   ", true
				}
				return "", false
			},
		},
		{
			name:     "default pager",
			lookup:   func(string) (string, bool) { return "", false },
			wantArgs: []string{"less", "-FRX"},
			wantPage: runtime.GOOS != "windows",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pager := &recordingPager{}
			res := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
				OutputTerminal:    true,
				OutputTerminalSet: true,
				Pager:             pager,
				LookupEnv:         tt.lookup,
			}, "long")
			if res.Exit != rungrad.ExitSuccess {
				t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
			}
			if !tt.wantPage {
				if pager.calls != 0 {
					t.Fatalf("pager calls = %d, want 0", pager.calls)
				}
				return
			}
			if pager.calls != 1 {
				t.Fatalf("pager calls = %d, want 1", pager.calls)
			}
			if !reflect.DeepEqual(pager.args, tt.wantArgs) {
				t.Fatalf("pager args = %v, want %v", pager.args, tt.wantArgs)
			}
		})
	}
}

func TestAdvancedPagerCommandResolvedOncePerRender(t *testing.T) {
	pager := &recordingPager{write: "pager displayed\n"}
	pagerLookups := 0
	res := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
		Pager:             pager,
		LookupEnv: func(name string) (string, bool) {
			if name != "PAGER" {
				return "", false
			}
			pagerLookups++
			if pagerLookups == 1 {
				return "pager", true
			}
			return "   ", true
		},
	}, "long")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if pagerLookups != 1 {
		t.Fatalf("PAGER lookups = %d, want 1", pagerLookups)
	}
	if pager.calls != 1 || !reflect.DeepEqual(pager.args, []string{"pager"}) {
		t.Fatalf("pager calls/args = %d/%v, want 1/[pager]", pager.calls, pager.args)
	}
	if res.Stdout != "pager displayed\n" {
		t.Fatalf("stdout = %q", res.Stdout)
	}
}

func TestAdvancedPagerUsesLinesEnvWhenHeightHookAbsent(t *testing.T) {
	pager := &recordingPager{}
	res := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
		Pager:             pager,
		LookupEnv: func(name string) (string, bool) {
			switch name {
			case "LINES":
				return "100", true
			case "PAGER":
				return "pager", true
			default:
				return "", false
			}
		},
	}, "long")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if pager.calls != 0 {
		t.Fatalf("pager calls = %d, want 0 for LINES=100", pager.calls)
	}

	pager = &recordingPager{}
	res = testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
		Pager:             pager,
		LookupEnv: func(name string) (string, bool) {
			switch name {
			case "LINES":
				return "1", true
			case "PAGER":
				return "pager", true
			default:
				return "", false
			}
		},
	}, "long")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if pager.calls != 1 {
		t.Fatalf("pager calls = %d, want 1 for LINES=1", pager.calls)
	}
}

func TestRunWithRestoresPagerHooks(t *testing.T) {
	app := newAdvancedOutputApp(true, nil)
	oldPager := &recordingPager{}
	app.Factory().PromptTerminalSet = true
	app.Factory().PromptTerminal = true
	app.Factory().OutputTerminalSet = true
	app.Factory().OutputTerminal = true
	app.Factory().LookupEnv = pagerLookup("oldpager")
	app.Factory().TerminalHeight = func() (int, bool) { return 1, true }
	app.Factory().Pager = oldPager

	overridePager := &recordingPager{}
	res := testutil.RunWith(app, testutil.Options{
		Terminal:          false,
		TerminalSet:       true,
		OutputTerminal:    false,
		OutputTerminalSet: true,
		LookupEnv:         pagerLookup("newpager"),
		TerminalHeight:    func() (int, bool) { return 100, true },
		Pager:             overridePager,
	}, "long")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("override run exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if oldPager.calls != 0 || overridePager.calls != 0 {
		t.Fatalf("unexpected pager calls during override: old=%d override=%d", oldPager.calls, overridePager.calls)
	}
	if !app.Factory().PromptTerminalSet || !app.Factory().PromptTerminal {
		t.Fatalf("prompt terminal override was not restored: set=%t value=%t", app.Factory().PromptTerminalSet, app.Factory().PromptTerminal)
	}
	if !app.Factory().OutputTerminalSet || !app.Factory().OutputTerminal {
		t.Fatalf("output terminal override was not restored: set=%t value=%t", app.Factory().OutputTerminalSet, app.Factory().OutputTerminal)
	}

	res = testutil.Run(app, "long")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("post-restore run exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if oldPager.calls != 1 {
		t.Fatalf("old pager calls after restore = %d, want 1", oldPager.calls)
	}
	if oldPager.args[0] != "oldpager" {
		t.Fatalf("old lookup was not restored; pager args = %v", oldPager.args)
	}
}

func TestAdvancedOutputWritePreviewPlainAndTransform(t *testing.T) {
	plain := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
	}, "preview", "--dry-run", "--plain")
	if plain.Exit != rungrad.ExitSuccess {
		t.Fatalf("plain preview exit %d stderr=%q", plain.Exit, plain.Stderr)
	}
	wantPlain := "DRY RUN: would POST /widgets\n  body:\n    name = demo\n  no changes were made\n"
	if plain.Stdout != wantPlain {
		t.Fatalf("plain preview = %q, want %q", plain.Stdout, wantPlain)
	}
	if strings.Contains(plain.Stdout, "\x1b[") {
		t.Fatalf("plain preview contains ANSI escapes: %q", plain.Stdout)
	}

	jq := testutil.Run(newAdvancedOutputApp(true, nil), "preview", "--dry-run", "--jq", ".")
	if jq.Exit != rungrad.ExitSuccess {
		t.Fatalf("jq preview exit %d stderr=%q", jq.Exit, jq.Stderr)
	}
	wantJQ := "{\"body\":{\"name\":\"demo\"},\"dry_run\":true,\"method\":\"POST\",\"path\":\"/widgets\"}\n"
	if jq.Stdout != wantJQ {
		t.Fatalf("jq preview = %q, want %q", jq.Stdout, wantJQ)
	}
}

func TestAdvancedOutputHelpListsOutputModes(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"read", "--help"}, "Output modes:\n  human, json, jq, template"},
		{[]string{"copy", "--help"}, "Output modes:\n  human, json, plain"},
		{[]string{"human", "--help"}, "Output modes:\n  human"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			res := testutil.Run(newAdvancedOutputApp(true, nil), tt.args...)
			if res.Exit != rungrad.ExitSuccess {
				t.Fatalf("help exit %d stderr=%q", res.Exit, res.Stderr)
			}
			if !strings.Contains(res.Stdout, tt.want) {
				t.Fatalf("help missing %q:\n%s", tt.want, res.Stdout)
			}
		})
	}
}

func TestOutputModesHelpIsNotAdvancedGated(t *testing.T) {
	res := testutil.Run(newAdvancedOutputApp(false, nil), "read", "--help")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("help exit %d stderr=%q", res.Exit, res.Stderr)
	}
	want := "Output modes:\n  human, json, jq, template"
	if !strings.Contains(res.Stdout, want) {
		t.Fatalf("non-advanced help missing %q:\n%s", want, res.Stdout)
	}
}

func TestAdvancedTransformModesAreMachineOutput(t *testing.T) {
	hint := testutil.Run(newAdvancedOutputApp(true, nil), "hint", "--jq", ".field")
	if hint.Exit != rungrad.ExitSuccess {
		t.Fatalf("hint exit %d stderr=%q", hint.Exit, hint.Stderr)
	}
	if hint.Stdout != "\"alpha\"\n" {
		t.Fatalf("hint stdout = %q", hint.Stdout)
	}
	if hint.Stderr != "" {
		t.Fatalf("transform mode emitted Infof stderr: %q", hint.Stderr)
	}

	mode := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
	}, "mode", "--jq", ".ansi")
	if mode.Exit != rungrad.ExitSuccess {
		t.Fatalf("mode exit %d stderr=%q", mode.Exit, mode.Stderr)
	}
	if mode.Stdout != "false\n" {
		t.Fatalf("TerminalMode under transform = %q, want false", mode.Stdout)
	}

	tmplMode := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
	}, "mode", "--template", "{{.sanitize}}")
	if tmplMode.Exit != rungrad.ExitSuccess {
		t.Fatalf("template mode exit %d stderr=%q", tmplMode.Exit, tmplMode.Stderr)
	}
	if tmplMode.Stdout != "false\n" {
		t.Fatalf("TerminalMode under template = %q, want false", tmplMode.Stdout)
	}

	resolved := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		Stdin:       failOnRead{t},
		Terminal:    true,
		TerminalSet: true,
	}, "resolve", "dup", "--jq", ".id")
	assertExitStdoutEmptyStderrContains(t, resolved, rungrad.ExitUsage, "ambiguous item name")

	destroyed := testutil.RunWith(newAdvancedOutputApp(true, nil), testutil.Options{
		Stdin:       failOnRead{t},
		Terminal:    true,
		TerminalSet: true,
	}, "destroy", "--jq", ".destroyed")
	assertExitStdoutEmptyStderrContains(t, destroyed, rungrad.ExitUsage, "destructive action requires --confirm")
}

func TestAdvancedTransformsPreserveLargeIntegers(t *testing.T) {
	jq := testutil.Run(newAdvancedOutputApp(true, nil), "big", "--jq", ".id")
	if jq.Exit != rungrad.ExitSuccess {
		t.Fatalf("jq exit %d stderr=%q", jq.Exit, jq.Stderr)
	}
	if jq.Stdout != "9007199254740993\n" {
		t.Fatalf("jq large integer = %q", jq.Stdout)
	}

	tmpl := testutil.Run(newAdvancedOutputApp(true, nil), "big", "--template", "{{.id}}")
	if tmpl.Exit != rungrad.ExitSuccess {
		t.Fatalf("template exit %d stderr=%q", tmpl.Exit, tmpl.Stderr)
	}
	if tmpl.Stdout != "9007199254740993\n" {
		t.Fatalf("template large integer = %q", tmpl.Stdout)
	}
}

type recordingPager struct {
	calls   int
	args    []string
	content string
	write   string
	err     error
}

func (p *recordingPager) Run(args []string, content io.Reader, stdout, stderr io.Writer) error {
	p.calls++
	p.args = append([]string(nil), args...)
	b, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	p.content = string(b)
	if p.write != "" {
		_, _ = io.WriteString(stdout, p.write)
	}
	return p.err
}

func pagerOptions(height int, pagerCommand string) testutil.Options {
	return testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
		TerminalHeight:    func() (int, bool) { return height, true },
		Pager:             &recordingPager{},
		LookupEnv:         pagerLookup(pagerCommand),
	}
}

func pagerLookup(command string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		if name == "PAGER" {
			return command, true
		}
		return "", false
	}
}

func longHumanOutput() string {
	var b strings.Builder
	for i := 1; i <= 45; i++ {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	return b.String()
}

func longPlainOutput() string {
	var b strings.Builder
	for i := 1; i <= 45; i++ {
		fmt.Fprintf(&b, "plain %02d\n", i)
	}
	return b.String()
}

func assertExitStdoutEmptyStderrContains(t *testing.T, res testutil.Result, wantExit int, wantStderr string) {
	t.Helper()
	if res.Exit != wantExit {
		t.Fatalf("exit = %d, want %d (stdout=%q stderr=%q)", res.Exit, wantExit, res.Stdout, res.Stderr)
	}
	if res.Stdout != "" {
		t.Fatalf("stdout = %q, want empty", res.Stdout)
	}
	if !strings.Contains(res.Stderr, wantStderr) {
		t.Fatalf("stderr missing %q: %q", wantStderr, res.Stderr)
	}
}
