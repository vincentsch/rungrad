package rungrad_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/output"
	"github.com/vincentsch/rungrad/resolve"
	"github.com/vincentsch/rungrad/testutil"
)

// demoApp builds a small tool exercising the framework surface.
func demoApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:    "rgdemo",
		Short:   "demo tool",
		Version: "0.0.0-test",
		EnvVar:  "RGDEMO_TOKEN",
	})
	app.AddCommand(
		&rungrad.Command{
			Use:         "ping",
			Short:       "print pong",
			Examples:    []string{"rgdemo ping"},
			Related:     []string{"rgdemo whoami"},
			OutputModes: []string{"table", "json"},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return f.WriteResult(
					map[string]string{"reply": "pong"},
					func(w io.Writer) {},
				)
			},
		},
		&rungrad.Command{
			Use:          "whoami",
			Short:        "show the current user",
			RequiresAuth: true,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return f.WriteResult(map[string]string{"token": f.Token}, nil)
			},
		},
		&rungrad.Command{
			Use:   "find <name>",
			Short: "resolve a name",
			Args:  cobra.ExactArgs(1),
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				_, err := resolve.Resolve(args[0], func(string) ([]resolve.Match, error) {
					return nil, nil
				}, resolve.Options{ResourceType: "widget"})
				return err
			},
		},
		&rungrad.Command{
			Use:     "create <name>",
			Short:   "create a widget",
			Mutates: true,
			Args:    cobra.ExactArgs(1),
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				if f.DryRun() {
					return f.WritePreview(output.DryRunPreview{
						Method: "POST", Path: "/widgets",
						Body: []output.Field{{Name: "name", Value: args[0]}},
					})
				}
				return f.WriteResult(output.MutationSummary{Action: "Created", Resource: "widget", Name: args[0]}, nil)
			},
		},
		&rungrad.Command{
			Use:         "delete <name>",
			Short:       "delete a widget",
			Destructive: true,
			Args:        cobra.ExactArgs(1),
			Configure: func(cmd *cobra.Command) {
				cmd.Flags().Bool("confirm", false, "Confirm without a prompt")
			},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				if f.DryRun() {
					return f.WritePreview(output.DryRunPreview{Method: "DELETE", Path: "/widgets/" + args[0]})
				}
				confirmed, _ := cmd.Flags().GetBool("confirm")
				summary := output.MutationSummary{Action: "Deleted", Resource: "widget", Name: args[0]}
				if err := f.ConfirmDestructive(rungrad.ConfirmOptions{Action: "delete widget", Target: args[0], Confirmed: confirmed}); err != nil {
					return err
				}
				return f.WriteResult(summary, func(w io.Writer) { output.RenderMutation(w, summary) })
			},
		},
	)
	return app
}

func run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := demoApp().Run(args, &out, &errb)
	return out.String(), errb.String(), code
}

// flagProbeApp builds a runnable command whose flags can be configured by a
// test. The no-op Run matters because Cobra skips pre-runs for non-runnable
// commands.
func flagProbeApp(requiresAuth bool, configure func(cmd *cobra.Command)) *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:   "rgdemo",
		Short:  "demo",
		EnvVar: "RGDEMO_TOKEN",
	})
	app.AddCommand(&rungrad.Command{
		Use:          "probe",
		Short:        "flag validation probe",
		RequiresAuth: requiresAuth,
		Configure:    configure,
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			return nil
		},
	})
	return app
}

type failOnRead struct{ t *testing.T }

// Read fails immediately so tests can prove a refusal path returned before
// touching stdin.
func (r failOnRead) Read([]byte) (int, error) {
	r.t.Fatal("stdin must not be read in non-interactive mode")
	return 0, io.EOF
}

func TestFactoryTerminalMode(t *testing.T) {
	tests := []struct {
		name    string
		factory *rungrad.Factory
		want    output.TerminalMode
	}{
		{
			name: "json disables ansi even with override",
			factory: &rungrad.Factory{
				Flags:             &rungrad.GlobalFlags{JSON: true},
				OutputTerminalSet: true,
				OutputTerminal:    true,
			},
		},
		{
			name: "explicit override true",
			factory: &rungrad.Factory{
				Flags:             &rungrad.GlobalFlags{},
				OutputTerminalSet: true,
				OutputTerminal:    true,
			},
			want: output.TerminalMode{ANSI: true, Color: true},
		},
		{
			name: "explicit override false",
			factory: &rungrad.Factory{
				Flags:             &rungrad.GlobalFlags{},
				OutputTerminalSet: true,
				OutputTerminal:    false,
			},
			want: output.TerminalMode{Sanitize: true},
		},
		{
			name: "non file stdout is plain",
			factory: &rungrad.Factory{
				Flags:  &rungrad.GlobalFlags{},
				Stdout: &bytes.Buffer{},
			},
			want: output.TerminalMode{Sanitize: true},
		},
		{
			name: "nil stdout is plain",
			factory: &rungrad.Factory{
				Flags: &rungrad.GlobalFlags{},
			},
			want: output.TerminalMode{Sanitize: true},
		},
		{
			name: "prompt override does not affect output",
			factory: &rungrad.Factory{
				Flags:             &rungrad.GlobalFlags{},
				PromptTerminalSet: true,
				PromptTerminal:    true,
			},
			want: output.TerminalMode{Sanitize: true},
		},
		{
			name: "no color leaves ansi enabled",
			factory: &rungrad.Factory{
				Flags:             &rungrad.GlobalFlags{NoColor: true},
				OutputTerminalSet: true,
				OutputTerminal:    true,
			},
			want: output.TerminalMode{ANSI: true, Color: false},
		},
		{
			name: "no ansi sanitizes",
			factory: &rungrad.Factory{
				Flags:             &rungrad.GlobalFlags{NoANSI: true},
				OutputTerminalSet: true,
				OutputTerminal:    true,
			},
			want: output.TerminalMode{Sanitize: true},
		},
		{
			name: "plain disables terminal mode",
			factory: &rungrad.Factory{
				Flags:             &rungrad.GlobalFlags{Plain: true},
				OutputTerminalSet: true,
				OutputTerminal:    true,
			},
		},
		{
			name: "nil flags with output override true",
			factory: &rungrad.Factory{
				OutputTerminalSet: true,
				OutputTerminal:    true,
			},
			want: output.TerminalMode{ANSI: true, Color: true},
		},
		{
			name:    "bare factory",
			factory: &rungrad.Factory{},
			want:    output.TerminalMode{Sanitize: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.factory.TerminalMode(); got != tt.want {
				t.Fatalf("TerminalMode() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAppDerivesPagerEnvVar(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"rgref", "RGREF_PAGER"},
		{"my-tool", "MY_TOOL_PAGER"},
		{"my--tool!", "MY_TOOL_PAGER"},
		{"r9", "R9_PAGER"},
		{"!!!", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := rungrad.New(rungrad.AppConfig{Name: tt.name, Short: "pager env"})
			if got := app.Factory().PagerEnvVar; got != tt.want {
				t.Fatalf("PagerEnvVar = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSuccessExitZeroAndJSON(t *testing.T) {
	out, _, code := run(t, "ping", "--json")
	if code != rungrad.ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("ping --json not valid JSON: %q", out)
	}
	if !strings.Contains(out, "pong") {
		t.Fatalf("missing payload: %q", out)
	}
}

func TestRepeatableOutput(t *testing.T) {
	a, _, _ := run(t, "ping", "--json")
	b, _, _ := run(t, "ping", "--json")
	if a != b {
		t.Fatalf("output not repeatable:\n%q\n%q", a, b)
	}
}

func TestUnknownSubcommandExitsUsage(t *testing.T) {
	_, _, code := run(t, "nope")
	if code != rungrad.ExitUsage {
		t.Fatalf("exit = %d, want %d", code, rungrad.ExitUsage)
	}
}

func TestUnknownFlagExitsUsage(t *testing.T) {
	_, stderr, code := run(t, "ping", "--bogus")
	if code != rungrad.ExitUsage {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, rungrad.ExitUsage, stderr)
	}
}

func TestArgumentValidationExitsUsage(t *testing.T) {
	_, stderr, code := run(t, "create")
	if code != rungrad.ExitUsage {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, rungrad.ExitUsage, stderr)
	}
}

func TestMissingRequiredFlagExitsUsage(t *testing.T) {
	app := flagProbeApp(false, func(cmd *cobra.Command) {
		cmd.Flags().String("name", "", "name")
		_ = cmd.MarkFlagRequired("name")
	})
	var out, errb bytes.Buffer
	code := app.Run([]string{"probe"}, &out, &errb)
	if code != rungrad.ExitUsage {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, rungrad.ExitUsage, errb.String())
	}
}

func TestRequiredTogetherFlagGroupExitsUsage(t *testing.T) {
	app := flagProbeApp(false, func(cmd *cobra.Command) {
		cmd.Flags().Bool("left", false, "left")
		cmd.Flags().Bool("right", false, "right")
		cmd.MarkFlagsRequiredTogether("left", "right")
	})
	var out, errb bytes.Buffer
	code := app.Run([]string{"probe", "--left"}, &out, &errb)
	if code != rungrad.ExitUsage {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, rungrad.ExitUsage, errb.String())
	}
}

func TestMutuallyExclusiveFlagGroupExitsUsage(t *testing.T) {
	app := flagProbeApp(false, func(cmd *cobra.Command) {
		cmd.Flags().Bool("left", false, "left")
		cmd.Flags().Bool("right", false, "right")
		cmd.MarkFlagsMutuallyExclusive("left", "right")
	})
	var out, errb bytes.Buffer
	code := app.Run([]string{"probe", "--left", "--right"}, &out, &errb)
	if code != rungrad.ExitUsage {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, rungrad.ExitUsage, errb.String())
	}
}

func TestOneRequiredFlagGroupExitsUsage(t *testing.T) {
	app := flagProbeApp(false, func(cmd *cobra.Command) {
		cmd.Flags().Bool("left", false, "left")
		cmd.Flags().Bool("right", false, "right")
		cmd.MarkFlagsOneRequired("left", "right")
	})
	var out, errb bytes.Buffer
	code := app.Run([]string{"probe"}, &out, &errb)
	if code != rungrad.ExitUsage {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, rungrad.ExitUsage, errb.String())
	}
}

func TestMissingRequiredFlagWinsOverAuth(t *testing.T) {
	// Force the auth path to have no credential; the usage exit proves flag
	// validation ran before credential loading.
	t.Setenv("RGDEMO_TOKEN", "")
	app := flagProbeApp(true, func(cmd *cobra.Command) {
		cmd.Flags().String("name", "", "name")
		_ = cmd.MarkFlagRequired("name")
	})
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	var out, errb bytes.Buffer
	code := app.Run([]string{"probe", "--config", cfg}, &out, &errb)
	if code != rungrad.ExitUsage {
		t.Fatalf("exit = %d, want %d (validation must precede auth) (stderr=%q)",
			code, rungrad.ExitUsage, errb.String())
	}
}

func TestInvalidFlagGroupWinsOverAuth(t *testing.T) {
	// Force the auth path to have no credential; the usage exit proves flag
	// validation ran before credential loading.
	t.Setenv("RGDEMO_TOKEN", "")
	app := flagProbeApp(true, func(cmd *cobra.Command) {
		cmd.Flags().Bool("left", false, "left")
		cmd.Flags().Bool("right", false, "right")
		cmd.MarkFlagsMutuallyExclusive("left", "right")
	})
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	var out, errb bytes.Buffer
	code := app.Run([]string{"probe", "--config", cfg, "--left", "--right"}, &out, &errb)
	if code != rungrad.ExitUsage {
		t.Fatalf("exit = %d, want %d (validation must precede auth) (stderr=%q)",
			code, rungrad.ExitUsage, errb.String())
	}
}

func TestMissingCredentialExitsAuth(t *testing.T) {
	var out, errb bytes.Buffer
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	code := demoApp().Run([]string{"whoami", "--config", cfg}, &out, &errb)
	if code != rungrad.ExitAuth {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, rungrad.ExitAuth, errb.String())
	}
}

func TestNotFoundExits(t *testing.T) {
	_, _, code := run(t, "find", "ghost")
	if code != rungrad.ExitNotFound {
		t.Fatalf("exit = %d, want %d", code, rungrad.ExitNotFound)
	}
}

func TestDryRunPreviewsWithoutMutating(t *testing.T) {
	out, _, code := run(t, "create", "thing", "--dry-run", "--json")
	if code != rungrad.ExitSuccess {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "dry_run") {
		t.Fatalf("expected dry-run preview, got %q", out)
	}
}

func TestDestructiveCommandSetsAnnotations(t *testing.T) {
	cmd, _, err := demoApp().Root().Find([]string{"delete"})
	if err != nil {
		t.Fatalf("find delete command: %v", err)
	}
	if cmd.Annotations[rungrad.AnnotationDestructive] != "true" {
		t.Fatalf("destructive annotation = %q, want true", cmd.Annotations[rungrad.AnnotationDestructive])
	}
	if cmd.Annotations[rungrad.AnnotationMutates] != "true" {
		t.Fatalf("mutates annotation = %q, want true", cmd.Annotations[rungrad.AnnotationMutates])
	}
}

func TestConfirmDestructiveDryRunBypasses(t *testing.T) {
	f := &rungrad.Factory{Flags: &rungrad.GlobalFlags{DryRun: true}, Stdin: failOnRead{t}}
	if err := f.ConfirmDestructive(rungrad.ConfirmOptions{Action: "delete", Target: "x"}); err != nil {
		t.Fatalf("dry-run should bypass confirmation: %v", err)
	}
}

func TestConfirmDestructiveConfirmedBypasses(t *testing.T) {
	f := &rungrad.Factory{Flags: &rungrad.GlobalFlags{}, Stdin: failOnRead{t}}
	if err := f.ConfirmDestructive(rungrad.ConfirmOptions{Confirmed: true}); err != nil {
		t.Fatalf("Confirmed should bypass the prompt: %v", err)
	}
}

func TestConfirmDestructiveNonInteractiveRefusesWithoutReadingStdin(t *testing.T) {
	cases := map[string]*rungrad.Factory{
		"json":         {Flags: &rungrad.GlobalFlags{JSON: true}},
		"no-prompt":    {Flags: &rungrad.GlobalFlags{NoPrompt: true}, PromptTerminalSet: true, PromptTerminal: true},
		"non-terminal": {Flags: &rungrad.GlobalFlags{}, PromptTerminalSet: true, PromptTerminal: false},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			f.Stdin = failOnRead{t}
			err := f.ConfirmDestructive(rungrad.ConfirmOptions{Action: "delete", Target: "x"})
			if rungrad.ExitCodeFor(err) != rungrad.ExitUsage {
				t.Fatalf("%s: exit = %d, want %d", name, rungrad.ExitCodeFor(err), rungrad.ExitUsage)
			}
		})
	}
}

func TestConfirmDestructiveInteractiveAccepts(t *testing.T) {
	for _, answer := range []string{"y\n", "yes\n", "Y\n", "YES\n"} {
		res := testutil.RunWith(demoApp(), testutil.Options{
			Stdin:       strings.NewReader(answer),
			Terminal:    true,
			TerminalSet: true,
		}, "delete", "thing")
		if res.Exit != rungrad.ExitSuccess {
			t.Fatalf("answer %q: exit %d stderr=%q", answer, res.Exit, res.Stderr)
		}
		if !strings.Contains(res.Stdout, "Deleted") {
			t.Fatalf("answer %q: missing deletion report: %q", answer, res.Stdout)
		}
	}
}

func TestConfirmDestructiveQuietStillPrompts(t *testing.T) {
	res := testutil.RunWith(demoApp(), testutil.Options{
		Stdin:       strings.NewReader("y\n"),
		Terminal:    true,
		TerminalSet: true,
	}, "delete", "thing", "--quiet")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("quiet interactive delete exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "delete widget") || !strings.Contains(res.Stderr, "thing") {
		t.Fatalf("--quiet hid the destructive prompt (must go to stderr, not Infof): %q", res.Stderr)
	}
}

func TestConfirmDestructiveInteractiveDeclines(t *testing.T) {
	for name, stdin := range map[string]string{"decline": "n\n", "eof": "", "unrecognized": "maybe\n"} {
		res := testutil.RunWith(demoApp(), testutil.Options{
			Stdin:       strings.NewReader(stdin),
			Terminal:    true,
			TerminalSet: true,
		}, "delete", "thing")
		if res.Exit != rungrad.ExitUsage {
			t.Fatalf("%s: exit = %d, want %d (stderr=%q)", name, res.Exit, rungrad.ExitUsage, res.Stderr)
		}
	}
}

func TestHelpHasExamplesAndRelated(t *testing.T) {
	out, _, _ := run(t, "ping", "--help")
	if !strings.Contains(strings.ToLower(out), "examples:") {
		t.Fatalf("help missing examples: %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "related commands:") {
		t.Fatalf("help missing related commands: %q", out)
	}
}

func TestReusedAppDoesNotLeakFlags(t *testing.T) {
	var loud bool
	app := rungrad.New(rungrad.AppConfig{Name: "reuse", Short: "reuse test"})
	app.AddCommand(&rungrad.Command{
		Use: "ping",
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().BoolVar(&loud, "loud", false, "Use loud mode")
		},
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			mode := "plain"
			if loud {
				mode = "loud"
			}
			return f.WriteResult(map[string]string{"mode": mode}, func(w io.Writer) {
				_, _ = io.WriteString(w, mode+"\n")
			})
		},
	})

	var out1, err1 bytes.Buffer
	if code := app.Run([]string{"ping", "--json", "--loud"}, &out1, &err1); code != rungrad.ExitSuccess {
		t.Fatalf("first run exit %d: %s", code, err1.String())
	}
	if !strings.Contains(out1.String(), `"loud"`) {
		t.Fatalf("first run should be loud JSON, got %q", out1.String())
	}

	var out2, err2 bytes.Buffer
	if code := app.Run([]string{"ping"}, &out2, &err2); code != rungrad.ExitSuccess {
		t.Fatalf("second run exit %d: %s", code, err2.String())
	}
	if got := strings.TrimSpace(out2.String()); got != "plain" {
		t.Fatalf("second run leaked flags, got %q", out2.String())
	}
}

func TestJSONAmbiguousErrorHasStructuredCandidates(t *testing.T) {
	app := rungrad.New(rungrad.AppConfig{Name: "rgdemo", Short: "demo"})
	app.AddCommand(&rungrad.Command{
		Use:  "get <name>",
		Args: cobra.ExactArgs(1),
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			_, err := f.Resolve(args[0], func(string) ([]resolve.Match, error) {
				return []resolve.Match{{ID: "2", Name: "dup"}, {ID: "1", Name: "dup"}}, nil
			}, resolve.Options{ResourceType: "item", AllowPrompt: true})
			return err
		},
	})

	var out, errb bytes.Buffer
	code := app.Run([]string{"get", "dup", "--json", "--no-prompt"}, &out, &errb)
	if code != rungrad.ExitUsage {
		t.Fatalf("exit = %d, want usage (stdout=%q stderr=%q)", code, out.String(), errb.String())
	}
	var body struct {
		Error      string          `json:"error"`
		ExitCode   int             `json:"exit_code"`
		Candidates []resolve.Match `json:"candidates"`
	}
	if err := json.Unmarshal(errb.Bytes(), &body); err != nil {
		t.Fatalf("stderr is not structured JSON: %v\n%s", err, errb.String())
	}
	if body.ExitCode != rungrad.ExitUsage || len(body.Candidates) != 2 {
		t.Fatalf("unexpected JSON error body: %+v", body)
	}
	if body.Candidates[0].ID != "1" || body.Candidates[1].ID != "2" {
		t.Fatalf("candidates not sorted: %+v", body.Candidates)
	}
}

func TestUnknownRuntimeErrorExitsAPI(t *testing.T) {
	app := rungrad.New(rungrad.AppConfig{Name: "rgdemo", Short: "demo"})
	app.AddCommand(&rungrad.Command{
		Use: "boom",
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			return errors.New("disk failed")
		},
	})
	var out, errb bytes.Buffer
	code := app.Run([]string{"boom"}, &out, &errb)
	if code != rungrad.ExitAPI {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, rungrad.ExitAPI, errb.String())
	}
}

func TestCyclicResultModelExitsAPI(t *testing.T) {
	app := rungrad.New(rungrad.AppConfig{Name: "rgdemo", Short: "demo"})
	app.AddCommand(&rungrad.Command{
		Use: "loop",
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			s := make([]any, 1)
			s[0] = s
			return f.WriteResult(s, nil)
		},
	})
	var out, errb bytes.Buffer
	code := app.Run([]string{"loop", "--json"}, &out, &errb)
	if code != rungrad.ExitAPI {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, rungrad.ExitAPI, errb.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout must be empty on cycle, got %q", out.String())
	}
	if !strings.Contains(errb.String(), "cycle detected") {
		t.Fatalf("stderr missing cycle detected: %q", errb.String())
	}
}

func TestRuntimeErrorWithUsageWordsExitsAPI(t *testing.T) {
	app := rungrad.New(rungrad.AppConfig{Name: "rgdemo", Short: "demo"})
	app.AddCommand(&rungrad.Command{
		Use: "upload",
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			return errors.New("the upload requires a valid region and timed out")
		},
	})
	var out, errb bytes.Buffer
	code := app.Run([]string{"upload"}, &out, &errb)
	if code != rungrad.ExitAPI {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, rungrad.ExitAPI, errb.String())
	}
}
