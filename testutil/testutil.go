// Package testutil makes dogfooding a rungrad tool the default. It runs a tool's
// own commands in-process, captures output, help, manifest, and docs artifacts,
// and provides golden/consistency assertions plus a mock HTTP backend.
package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/output"
)

// Result captures the output of running a command in-process.
type Result struct {
	Stdout string
	Stderr string
	Exit   int
}

// JSON unmarshals the captured stdout into v.
func (r Result) JSON(v any) error {
	return json.Unmarshal([]byte(r.Stdout), v)
}

// Options injects inputs for a run.
type Options struct {
	// Stdin feeds interactive prompts. When nil, an empty reader is used so a
	// prompt cannot block a test.
	Stdin io.Reader
	// Terminal, when set, overrides the prompt terminal detector.
	Terminal bool
	// TerminalSet controls whether Terminal is applied.
	TerminalSet bool
	// OutputTerminal, when set, overrides the stdout terminal detector that
	// drives human color styling. Independent of Terminal, which overrides prompt
	// detection.
	OutputTerminal bool
	// OutputTerminalSet controls whether OutputTerminal is applied.
	OutputTerminalSet bool
	// Env temporarily sets environment variables while the command runs.
	Env map[string]string
	// LookupEnv overrides environment lookup for pager selection, resolution,
	// and default auth. When set, it is used instead of Options.Env for those
	// framework lookups.
	LookupEnv func(string) (string, bool)
	// TerminalHeight overrides terminal height lookup for pager selection.
	TerminalHeight func() (int, bool)
	// Pager overrides pager execution.
	Pager rungrad.Pager
	// BrowserOpener overrides browser opening.
	BrowserOpener func(ctx context.Context, url string) error
	// UserConfigDir overrides default user config directory resolution.
	UserConfigDir func() (string, error)
}

// Run executes app with args and returns the captured result.
func Run(app *rungrad.App, args ...string) Result {
	return RunWith(app, Options{}, args...)
}

// RunWith executes app with explicit options.
func RunWith(app *rungrad.App, opts Options, args ...string) Result {
	stdin := opts.Stdin
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	restoreEnv := setEnv(opts.Env)
	defer restoreEnv()
	if opts.TerminalSet {
		f := app.Factory()
		oldSet, oldVal := f.PromptTerminalSet, f.PromptTerminal
		f.PromptTerminalSet, f.PromptTerminal = true, opts.Terminal
		defer func() {
			f.PromptTerminalSet, f.PromptTerminal = oldSet, oldVal
		}()
	}
	if opts.OutputTerminalSet {
		// Styling stdout and allowing stdin/stderr prompts are separate decisions;
		// tests often need to force only the output side.
		f := app.Factory()
		oldSet, oldVal := f.OutputTerminalSet, f.OutputTerminal
		f.OutputTerminalSet, f.OutputTerminal = true, opts.OutputTerminal
		defer func() {
			f.OutputTerminalSet, f.OutputTerminal = oldSet, oldVal
		}()
	}
	if opts.LookupEnv != nil {
		f := app.Factory()
		old := f.LookupEnv
		f.LookupEnv = opts.LookupEnv
		defer func() { f.LookupEnv = old }()
	}
	if opts.TerminalHeight != nil {
		f := app.Factory()
		old := f.TerminalHeight
		f.TerminalHeight = opts.TerminalHeight
		defer func() { f.TerminalHeight = old }()
	}
	if opts.Pager != nil {
		f := app.Factory()
		old := f.Pager
		f.Pager = opts.Pager
		defer func() { f.Pager = old }()
	}
	if opts.BrowserOpener != nil {
		f := app.Factory()
		old := f.BrowserOpener
		f.BrowserOpener = opts.BrowserOpener
		defer func() { f.BrowserOpener = old }()
	}
	if opts.UserConfigDir != nil {
		f := app.Factory()
		old := f.UserConfigDir
		f.UserConfigDir = opts.UserConfigDir
		defer func() { f.UserConfigDir = old }()
	}
	var out, errb bytes.Buffer
	code := app.RunIO(args, stdin, &out, &errb)
	return Result{Stdout: out.String(), Stderr: errb.String(), Exit: code}
}

func setEnv(env map[string]string) func() {
	if len(env) == 0 {
		return func() {}
	}
	// Process environment is global state. Tests using Options.Env should not run
	// in parallel with other tests that depend on the same environment variables.
	old := make(map[string]*string, len(env))
	for k, v := range env {
		if cur, ok := os.LookupEnv(k); ok {
			curCopy := cur
			old[k] = &curCopy
		} else {
			old[k] = nil
		}
		_ = os.Setenv(k, v)
	}
	return func() {
		for k, v := range old {
			if v == nil {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, *v)
			}
		}
	}
}

// AssertStableJSON verifies that stdout parses into v and re-encodes
// deterministically through rungrad's stable JSON encoder.
func AssertStableJSON(t *testing.T, stdout string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(stdout), v); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	first, err := output.StableJSON(v)
	if err != nil {
		t.Fatalf("stable JSON encode: %v", err)
	}
	second, err := output.StableJSON(v)
	if err != nil {
		t.Fatalf("stable JSON encode: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("stable JSON encoding is not repeatable")
	}
}

// AssertRedacted fails when any secret appears verbatim in stdout or stderr,
// proving the framework's output boundary removed it.
func AssertRedacted(t *testing.T, r Result, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		if s == "" {
			continue
		}
		if strings.Contains(r.Stdout, s) {
			t.Fatalf("secret leaked on stdout: %q in %q", s, r.Stdout)
		}
		if strings.Contains(r.Stderr, s) {
			t.Fatalf("secret leaked on stderr: %q in %q", s, r.Stderr)
		}
	}
}

// MockServer starts an httptest server with the given handler. The caller closes
// it, usually with t.Cleanup.
func MockServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}
