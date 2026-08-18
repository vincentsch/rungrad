package main

import (
	"io"
	"strings"
	"testing"

	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/testutil"
)

// recordingPager is a test pager that records invocations and optional output.
// It lets pager policy tests inspect the rendered human content directly.
type recordingPager struct {
	calls   int
	args    []string
	content string
	write   string
}

// Run records the content the framework would send to a pager. Tests use it to
// prove whether a command reached the pager path without launching a real pager.
func (p *recordingPager) Run(args []string, content io.Reader, stdout, stderr io.Writer) error {
	p.calls++
	p.args = append([]string(nil), args...)
	b, _ := io.ReadAll(content)
	p.content = string(b)
	if p.write != "" {
		_, _ = io.WriteString(stdout, p.write)
	}
	return nil
}

// failOnRead is a stdin sentinel for paths that must be non-interactive.
type failOnRead struct{ t *testing.T }

// Read fails the test if a machine-output path tries to fall back to an
// interactive prompt. It makes "did not prompt" observable without timing out.
func (f failOnRead) Read([]byte) (int, error) {
	f.t.Fatalf("machine mode unexpectedly read stdin")
	return 0, io.EOF
}

func TestItemListPlain(t *testing.T) {
	want := "1\talpha\n2\tdup\n3\tdup\n"
	first := runRgrefWith(t, testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
	}, "item", "list", "--plain")
	if first.Exit != rungrad.ExitSuccess {
		t.Fatalf("plain exit %d stderr=%q", first.Exit, first.Stderr)
	}
	if first.Stdout != want {
		t.Fatalf("plain stdout = %q, want %q", first.Stdout, want)
	}
	if strings.Contains(first.Stdout, "\x1b") {
		t.Fatalf("plain stdout contains ANSI escapes: %q", first.Stdout)
	}

	second := runRgref(t, "item", "list", "--plain")
	if second.Exit != rungrad.ExitSuccess {
		t.Fatalf("second plain exit %d stderr=%q", second.Exit, second.Stderr)
	}
	if second.Stdout != first.Stdout {
		t.Fatalf("plain stdout not deterministic:\n%q\n%q", first.Stdout, second.Stdout)
	}

	pager := &recordingPager{}
	paged := runRgrefWith(t, testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
		TerminalHeight:    func() (int, bool) { return 1, true },
		Pager:             pager,
		LookupEnv:         rgrefPagerLookup("pager -x"),
	}, "item", "list", "--plain")
	if paged.Exit != rungrad.ExitSuccess {
		t.Fatalf("paged plain exit %d stderr=%q", paged.Exit, paged.Stderr)
	}
	if pager.calls != 0 {
		t.Fatalf("plain invoked pager %d times", pager.calls)
	}
	if paged.Stdout != want {
		t.Fatalf("paged plain stdout = %q, want %q", paged.Stdout, want)
	}
}

func TestItemListTransformsPreserveModel(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"jq size", []string{"item", "list", "--jq", ".[0].size"}, "9007199254740993\n"},
		{"template size", []string{"item", "list", "--template", "{{(index . 0).size}}"}, "9007199254740993\n"},
		{"jq label", []string{"item", "list", "--jq", ".[0].label"}, "\"A&B <demo> café\"\n"},
		{"template label", []string{"item", "list", "--template", "{{(index . 0).label}}"}, "A&B <demo> café\n"},
		{"jq names", []string{"item", "list", "--jq", ".[].name"}, "\"alpha\"\n\"dup\"\n\"dup\"\n"},
		{"template rows", []string{"item", "list", "--template", `{{range .}}{{.id}} {{.name}}{{"\n"}}{{end}}`}, "1 alpha\n2 dup\n3 dup\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runRgref(t, tt.args...)
			if res.Exit != rungrad.ExitSuccess {
				t.Fatalf("%v exit %d stderr=%q", tt.args, res.Exit, res.Stderr)
			}
			if res.Stdout != tt.want {
				t.Fatalf("%v stdout = %q, want %q", tt.args, res.Stdout, tt.want)
			}
		})
	}
}

func TestItemGetTransforms(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"jq", []string{"item", "get", "alpha", "--jq", ".id"}, "\"1\"\n"},
		{"template", []string{"item", "get", "alpha", "--template", "{{.id}}"}, "1\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runRgref(t, tt.args...)
			if res.Exit != rungrad.ExitSuccess {
				t.Fatalf("%v exit %d stderr=%q", tt.args, res.Exit, res.Stderr)
			}
			if res.Stdout != tt.want {
				t.Fatalf("%v stdout = %q, want %q", tt.args, res.Stdout, tt.want)
			}
		})
	}
}

func TestItemListMachineModesAreDeterministicEscapeFreeUnpaged(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"json", []string{"item", "list", "--json"}},
		{"jq", []string{"item", "list", "--jq", "."}},
		{"template", []string{"item", "list", "--template", "{{(index . 0).id}}"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first := runRgref(t, tt.args...)
			if first.Exit != rungrad.ExitSuccess {
				t.Fatalf("%v exit %d stderr=%q", tt.args, first.Exit, first.Stderr)
			}
			second := runRgref(t, tt.args...)
			if second.Exit != rungrad.ExitSuccess {
				t.Fatalf("%v second exit %d stderr=%q", tt.args, second.Exit, second.Stderr)
			}
			if first.Stdout != second.Stdout {
				t.Fatalf("%v stdout not deterministic:\n%q\n%q", tt.args, first.Stdout, second.Stdout)
			}
			if strings.Contains(first.Stdout, "\x1b") {
				t.Fatalf("%v stdout contains ANSI escapes: %q", tt.args, first.Stdout)
			}

			pager := &recordingPager{}
			paged := runRgrefWith(t, testutil.Options{
				OutputTerminal:    true,
				OutputTerminalSet: true,
				TerminalHeight:    func() (int, bool) { return 1, true },
				Pager:             pager,
				LookupEnv:         rgrefPagerLookup("pager -x"),
			}, tt.args...)
			if paged.Exit != rungrad.ExitSuccess {
				t.Fatalf("%v paged exit %d stderr=%q", tt.args, paged.Exit, paged.Stderr)
			}
			if pager.calls != 0 {
				t.Fatalf("%v invoked pager %d times", tt.args, pager.calls)
			}
		})
	}

	json := runRgref(t, "item", "list", "--json")
	if json.Exit != rungrad.ExitSuccess {
		t.Fatalf("json exit %d stderr=%q", json.Exit, json.Stderr)
	}
	if !strings.Contains(json.Stdout, "9007199254740993") {
		t.Fatalf("json stdout missing large integer: %q", json.Stdout)
	}
}

func TestMachineModeNeverPrompts(t *testing.T) {
	res := runRgrefWith(t, testutil.Options{
		Stdin:       failOnRead{t},
		Terminal:    true,
		TerminalSet: true,
	}, "item", "get", "dup", "--jq", ".id")
	assertRgrefExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, "ambiguous item name")
}

func TestUnsupportedModeCombinationsFailBeforeHandler(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"jq unsupported", []string{"item", "create", "gamma", "--jq", ".id"}, "does not support --jq or --template"},
		{"plain unsupported", []string{"item", "get", "alpha", "--plain"}, "does not support --plain"},
		{"meta unsupported", []string{"item", "create", "gamma", "--include-meta", "--json"}, "does not support --include-meta"},
		{"jq template conflict", []string{"item", "list", "--jq", ".", "--template", "{{.}}"}, "--jq and --template cannot be combined"},
		{"plain json conflict", []string{"item", "list", "--plain", "--json"}, "--plain cannot be combined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runRgref(t, tt.args...)
			assertRgrefExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, tt.want)
		})
	}
}

func TestItemListPagerPolicy(t *testing.T) {
	// A one-line terminal height makes the four-line human table long enough to
	// page. Machine and plain modes should return before the pager decision.
	tests := []struct {
		name         string
		args         []string
		wantPager    bool
		pagerWrite   string
		wantContains string
	}{
		{name: "human pages", args: []string{"item", "list"}, wantPager: true, pagerWrite: "PAGED\n"},
		{name: "no color still pages", args: []string{"item", "list", "--no-color"}, wantPager: true},
		{name: "no pager", args: []string{"item", "list", "--no-pager"}, wantContains: "alpha"},
		{name: "no ansi", args: []string{"item", "list", "--no-ansi"}, wantContains: "alpha"},
		{name: "json", args: []string{"item", "list", "--json"}},
		{name: "jq", args: []string{"item", "list", "--jq", ".[].name"}},
		{name: "template", args: []string{"item", "list", "--template", "{{(index . 0).id}}"}},
		{name: "plain", args: []string{"item", "list", "--plain"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pager := &recordingPager{write: tt.pagerWrite}
			res := runRgrefWith(t, testutil.Options{
				OutputTerminal:    true,
				OutputTerminalSet: true,
				TerminalHeight:    func() (int, bool) { return 1, true },
				Pager:             pager,
				LookupEnv:         rgrefPagerLookup("pager -x"),
			}, tt.args...)
			if res.Exit != rungrad.ExitSuccess {
				t.Fatalf("%v exit %d stderr=%q", tt.args, res.Exit, res.Stderr)
			}
			if tt.wantPager {
				if pager.calls != 1 {
					t.Fatalf("%v pager calls = %d, want 1", tt.args, pager.calls)
				}
				if !strings.Contains(pager.content, "alpha") {
					t.Fatalf("%v pager content missing alpha: %q", tt.args, pager.content)
				}
				if tt.pagerWrite != "" && res.Stdout != tt.pagerWrite {
					t.Fatalf("%v stdout = %q, want %q", tt.args, res.Stdout, tt.pagerWrite)
				}
				return
			}
			if pager.calls != 0 {
				t.Fatalf("%v pager calls = %d, want 0", tt.args, pager.calls)
			}
			if tt.wantContains != "" && !strings.Contains(res.Stdout, tt.wantContains) {
				t.Fatalf("%v stdout missing %q: %q", tt.args, tt.wantContains, res.Stdout)
			}
			if strings.Contains(res.Stdout, "\x1b") {
				t.Fatalf("%v stdout contains ANSI escapes: %q", tt.args, res.Stdout)
			}
		})
	}
}

// rgrefPagerLookup exposes only PAGER so tests exercise the normal fallback
// pager selection path without depending on the host environment.
func rgrefPagerLookup(command string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		if name == "PAGER" {
			return command, true
		}
		return "", false
	}
}

// assertRgrefExitStdoutEmptyStderrContains checks guard failures that should
// stop before command handlers write anything to stdout.
func assertRgrefExitStdoutEmptyStderrContains(t *testing.T, res testutil.Result, wantExit int, wantStderr string) {
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
