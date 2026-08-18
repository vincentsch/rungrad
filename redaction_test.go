package rungrad_test

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/output"
	"github.com/vincentsch/rungrad/redact"
	"github.com/vincentsch/rungrad/resolve"
	"github.com/vincentsch/rungrad/testutil"
)

const (
	redactionSecret        = "secret-token-alpha-12345"
	redactionSpecialSecret = "quote\"slash\\tail-fragment-secret"
	redactionShortSecret   = "abc"
)

func redactionApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:           "rgredact",
		Short:          "redaction test",
		AdvancedOutput: true,
	})
	allModes := []string{
		rungrad.OutputModeHuman,
		rungrad.OutputModeJSON,
		rungrad.OutputModePlain,
		rungrad.OutputModeJQ,
		rungrad.OutputModeTemplate,
	}
	machineModes := []string{
		rungrad.OutputModeHuman,
		rungrad.OutputModeJSON,
		rungrad.OutputModeJQ,
		rungrad.OutputModeTemplate,
	}
	app.AddCommand(
		&rungrad.Command{
			Use:          "leak",
			Short:        "leak in every output mode",
			OutputModes:  allModes,
			SupportsMeta: true,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				f.RegisterSecret(redactionSecret)
				return f.WriteOutput(rungrad.Output{
					Model: redactionPayload(redactionSecret),
					Meta: output.Meta{
						RequestID: redactionSecret,
						Extra:     map[string]any{"echo": "meta " + redactionSecret},
					},
					Human: func(w io.Writer) {
						fmt.Fprintf(w, "human %s\n", redactionSecret)
					},
					Plain: func(w io.Writer) {
						fmt.Fprintf(w, "plain %s\n", redactionSecret)
					},
				})
			},
		},
		&rungrad.Command{
			Use:          "leak-special",
			Short:        "leak escaped secret",
			OutputModes:  allModes,
			SupportsMeta: true,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				f.RegisterSecret(redactionSpecialSecret)
				return f.WriteOutput(rungrad.Output{
					Model: redactionPayload(redactionSpecialSecret),
					Meta: output.Meta{
						RequestID: redactionSpecialSecret,
						Extra:     map[string]any{"echo": "meta " + redactionSpecialSecret},
					},
					Human: func(w io.Writer) {
						fmt.Fprintf(w, "human %s\n", redactionSpecialSecret)
					},
					Plain: func(w io.Writer) {
						fmt.Fprintf(w, "plain %s\n", redactionSpecialSecret)
					},
				})
			},
		},
		&rungrad.Command{
			Use:          "leak-info",
			Short:        "leak through info",
			OutputModes:  machineModes,
			SupportsMeta: false,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				f.RegisterSecret(redactionSecret)
				f.Infof("hint %s", redactionSecret)
				return f.WriteResult(map[string]string{"value": redactionSecret}, func(w io.Writer) {
					fmt.Fprintf(w, "human %s\n", redactionSecret)
				})
			},
		},
		&rungrad.Command{
			Use:          "leak-long",
			Short:        "leak through pager",
			OutputModes:  allModes,
			SupportsMeta: false,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				f.RegisterSecret(redactionSecret)
				return f.WriteOutput(rungrad.Output{
					Model: map[string]string{"ok": "true"},
					Human: func(w io.Writer) {
						for i := 1; i <= 45; i++ {
							fmt.Fprintf(w, "line %02d %s\n", i, redactionSecret)
						}
					},
					Plain: func(w io.Writer) {
						fmt.Fprintln(w, "plain")
					},
				})
			},
		},
		&rungrad.Command{
			Use:          "leak-short",
			Short:        "ignore short registered values",
			OutputModes:  machineModes,
			SupportsMeta: false,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				f.RegisterSecret(redactionShortSecret)
				f.RegisterSecret(redactionSecret)
				return f.WriteResult(map[string]string{
					"short": redactionShortSecret,
					"long":  redactionSecret,
				}, nil)
			},
		},
		&rungrad.Command{
			Use:          "leak-resolve <name>",
			Short:        "leak through resolve prompt",
			Args:         cobra.ExactArgs(1),
			OutputModes:  machineModes,
			SupportsMeta: false,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				f.RegisterSecret(redactionSecret)
				_, err := f.Resolve(args[0], func(string) ([]resolve.Match, error) {
					return []resolve.Match{
						{ID: "id-" + redactionSecret + "-1", Name: "one-" + redactionSecret, Context: "ctx-" + redactionSecret},
						{ID: "id-" + redactionSecret + "-2", Name: "two-" + redactionSecret, Context: "ctx-" + redactionSecret},
					}, nil
				}, resolve.Options{ResourceType: "item", AllowPrompt: true})
				if err != nil {
					return err
				}
				return f.WriteResult(map[string]string{"ok": "true"}, nil)
			},
		},
		&rungrad.Command{
			Use:          "leak-confirm",
			Short:        "leak through confirm prompt",
			Destructive:  true,
			OutputModes:  machineModes,
			SupportsMeta: false,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				f.RegisterSecret(redactionSecret)
				if err := f.ConfirmDestructive(rungrad.ConfirmOptions{
					Action: "delete",
					Target: "target-" + redactionSecret,
				}); err != nil {
					return err
				}
				return f.WriteResult(map[string]string{"ok": "true"}, nil)
			},
		},
		&rungrad.Command{
			Use:          "leak-preview",
			Short:        "leak through dry-run preview",
			Mutates:      true,
			OutputModes:  allModes,
			SupportsMeta: false,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				f.RegisterSecret(redactionSecret)
				if f.DryRun() {
					return f.WritePreview(output.DryRunPreview{
						Method:      "POST",
						Path:        "/widgets/" + redactionSecret,
						Idempotency: "idem-" + redactionSecret,
						Body: []output.Field{
							{Name: "visible", Value: "body " + redactionSecret},
							{Name: "masked", Value: redactionSecret, Secret: true},
						},
					})
				}
				return f.WriteResult(map[string]string{"ok": "true"}, nil)
			},
		},
		&rungrad.Command{
			Use:          "fail",
			Short:        "leak through errors",
			OutputModes:  machineModes,
			SupportsMeta: false,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				f.RegisterSecret(redactionSecret)
				return rungrad.NewError(rungrad.ExitAPI, "backend rejected "+redactionSecret)
			},
		},
	)
	return app
}

func redactionPayload(secret string) map[string]any {
	return map[string]any{
		"equal":    secret,
		"embedded": "prefix " + secret + " suffix",
		"nested":   map[string]string{"value": "nested " + secret},
	}
}

func TestOutputBoundaryRedactsSuccessModes(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		jsonOutput bool
	}{
		{"human", []string{"leak"}, false},
		{"json", []string{"leak", "--json"}, true},
		{"plain", []string{"leak", "--plain"}, false},
		{"jq", []string{"leak", "--jq", ".equal"}, true},
		{"template", []string{"leak", "--template", "{{.equal}} {{.embedded}}"}, false},
		{"meta", []string{"leak", "--json", "--include-meta"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := testutil.Run(redactionApp(), tt.args...)
			if res.Exit != rungrad.ExitSuccess {
				t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
			}
			testutil.AssertRedacted(t, res, redactionSecret)
			if !strings.Contains(res.Stdout, redact.Token) {
				t.Fatalf("stdout missing redaction token: %q", res.Stdout)
			}
			if tt.jsonOutput && !json.Valid([]byte(res.Stdout)) {
				t.Fatalf("stdout is invalid JSON: %q", res.Stdout)
			}
		})
	}
}

func TestOutputBoundaryRedactsEscapedJSONSecret(t *testing.T) {
	res := testutil.Run(redactionApp(), "leak-special", "--json")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	testutil.AssertRedacted(t, res, redactionSpecialSecret)
	if !json.Valid([]byte(res.Stdout)) {
		t.Fatalf("stdout is invalid JSON: %q", res.Stdout)
	}
	if strings.Contains(res.Stdout, "tail-fragment-secret") {
		t.Fatalf("escaped secret tail survived: %q", res.Stdout)
	}
	if escaped := jsonEscapedInner(t, redactionSpecialSecret); strings.Contains(res.Stdout, escaped) {
		t.Fatalf("escaped secret form survived: %q in %q", escaped, res.Stdout)
	}
	if !strings.Contains(res.Stdout, redact.Token) {
		t.Fatalf("stdout missing redaction token: %q", res.Stdout)
	}
}

func TestOutputBoundaryRedactsErrors(t *testing.T) {
	text := testutil.Run(redactionApp(), "fail")
	assertExitStdoutEmptyStderrContains(t, text, rungrad.ExitAPI, redact.Token)
	testutil.AssertRedacted(t, text, redactionSecret)

	js := testutil.Run(redactionApp(), "fail", "--json")
	if js.Exit != rungrad.ExitAPI {
		t.Fatalf("json exit %d stderr=%q", js.Exit, js.Stderr)
	}
	if js.Stdout != "" {
		t.Fatalf("json error stdout = %q, want empty", js.Stdout)
	}
	if !json.Valid([]byte(js.Stderr)) {
		t.Fatalf("stderr is invalid JSON: %q", js.Stderr)
	}
	if !strings.Contains(js.Stderr, redact.Token) {
		t.Fatalf("json error missing redaction token: %q", js.Stderr)
	}
	testutil.AssertRedacted(t, js, redactionSecret)
}

func TestOutputBoundaryRedactsInfof(t *testing.T) {
	res := testutil.Run(redactionApp(), "leak-info")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	testutil.AssertRedacted(t, res, redactionSecret)
	if !strings.Contains(res.Stdout, redact.Token) || !strings.Contains(res.Stderr, redact.Token) {
		t.Fatalf("stdout/stderr missing token: stdout=%q stderr=%q", res.Stdout, res.Stderr)
	}
}

func TestOutputBoundaryRedactsResolvePrompt(t *testing.T) {
	res := testutil.RunWith(redactionApp(), testutil.Options{
		Stdin:       strings.NewReader("1\n"),
		Terminal:    true,
		TerminalSet: true,
	}, "leak-resolve", "name-"+redactionSecret)
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	testutil.AssertRedacted(t, res, redactionSecret)
	if !strings.Contains(res.Stderr, redact.Token) {
		t.Fatalf("resolve prompt missing token: %q", res.Stderr)
	}
}

func TestOutputBoundaryRedactsConfirmPrompt(t *testing.T) {
	res := testutil.RunWith(redactionApp(), testutil.Options{
		Stdin:       strings.NewReader("yes\n"),
		Terminal:    true,
		TerminalSet: true,
	}, "leak-confirm")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	testutil.AssertRedacted(t, res, redactionSecret)
	if !strings.Contains(res.Stderr, redact.Token) {
		t.Fatalf("confirm prompt missing token: %q", res.Stderr)
	}
}

func TestOutputBoundaryRedactsBeforePager(t *testing.T) {
	pager := &recordingPager{}
	res := testutil.RunWith(redactionApp(), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
		TerminalHeight:    func() (int, bool) { return 1, true },
		Pager:             pager,
		LookupEnv:         pagerLookup("pager"),
	}, "leak-long")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if pager.calls != 1 {
		t.Fatalf("pager calls = %d, want 1", pager.calls)
	}
	if strings.Contains(pager.content, redactionSecret) {
		t.Fatalf("pager content leaked secret: %q", pager.content)
	}
	if !strings.Contains(pager.content, redact.Token) {
		t.Fatalf("pager content missing redaction token: %q", pager.content)
	}
}

func TestOutputBoundaryIgnoresShortSecrets(t *testing.T) {
	res := testutil.Run(redactionApp(), "leak-short", "--json")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if !json.Valid([]byte(res.Stdout)) {
		t.Fatalf("stdout is invalid JSON: %q", res.Stdout)
	}
	if !strings.Contains(res.Stdout, redactionShortSecret) {
		t.Fatalf("short value was redacted: %q", res.Stdout)
	}
	testutil.AssertRedacted(t, res, redactionSecret)
	if !strings.Contains(res.Stdout, redact.Token) {
		t.Fatalf("stdout missing redaction token: %q", res.Stdout)
	}
}

func TestOutputBoundaryRedactsDryRunPreview(t *testing.T) {
	human := testutil.Run(redactionApp(), "leak-preview", "--dry-run")
	if human.Exit != rungrad.ExitSuccess {
		t.Fatalf("human exit %d stderr=%q", human.Exit, human.Stderr)
	}
	testutil.AssertRedacted(t, human, redactionSecret)
	if !strings.Contains(human.Stdout, redact.Token) || !strings.Contains(human.Stdout, "masked = ***") {
		t.Fatalf("human preview missing token or masked field: %q", human.Stdout)
	}

	js := testutil.Run(redactionApp(), "leak-preview", "--dry-run", "--json")
	if js.Exit != rungrad.ExitSuccess {
		t.Fatalf("json exit %d stderr=%q", js.Exit, js.Stderr)
	}
	if !json.Valid([]byte(js.Stdout)) {
		t.Fatalf("stdout is invalid JSON: %q", js.Stdout)
	}
	testutil.AssertRedacted(t, js, redactionSecret)
	if !strings.Contains(js.Stdout, redact.Token) || !strings.Contains(js.Stdout, `"masked": "***"`) {
		t.Fatalf("json preview missing token or masked field: %q", js.Stdout)
	}
}

func TestOutputBoundaryRedactionDeterministic(t *testing.T) {
	app := redactionApp()
	first := testutil.Run(app, "leak", "--json")
	second := testutil.Run(app, "leak", "--json")
	if first.Exit != rungrad.ExitSuccess || second.Exit != rungrad.ExitSuccess {
		t.Fatalf("exits %d/%d stderr=%q/%q", first.Exit, second.Exit, first.Stderr, second.Stderr)
	}
	if first.Stdout != second.Stdout {
		t.Fatalf("redacted JSON not deterministic:\nfirst  %q\nsecond %q", first.Stdout, second.Stdout)
	}
}

func TestOutputBoundaryAutoRegistersAuthToken(t *testing.T) {
	const token = "auth-token-alpha-12345"
	res := testutil.RunWith(demoApp(), testutil.Options{
		Env: map[string]string{"RGDEMO_TOKEN": token},
	}, "whoami", "--json")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if !json.Valid([]byte(res.Stdout)) {
		t.Fatalf("stdout is invalid JSON: %q", res.Stdout)
	}
	testutil.AssertRedacted(t, res, token)
	if !strings.Contains(res.Stdout, redact.Token) {
		t.Fatalf("stdout missing redaction token: %q", res.Stdout)
	}
}

func jsonEscapedInner(t *testing.T, value string) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
	}
	if len(b) < 2 {
		t.Fatalf("marshaled string too short: %q", b)
	}
	return string(b[1 : len(b)-1])
}
