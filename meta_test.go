package rungrad_test

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/output"
	"github.com/vincentsch/rungrad/testutil"
)

func intp(v int) *int       { return &v }
func int64p(v int64) *int64 { return &v }
func boolp(v bool) *bool    { return &v }

func newMetaApp(advanced bool, probe *advancedOutputProbe) *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:           "rgmeta",
		Short:          "metadata test",
		AdvancedOutput: advanced,
	})
	meta := output.Meta{
		RequestID:  "req-1",
		RequestIDs: []string{"req-1", "req-0"},
		Pagination: &output.Pagination{
			Page:       intp(1),
			PerPage:    intp(2),
			TotalItems: intp(5),
			HasMore:    boolp(true),
		},
		RateLimit: &output.RateLimit{
			Limit:     int64p(100),
			Remaining: int64p(99),
		},
		Retry:       &output.Retry{Attempts: 2, WaitsMS: []int64{500}},
		Idempotency: &output.Idempotency{Key: "idem-1", Replayed: boolp(false)},
		Extra:       map[string]any{"region": "us-1"},
	}
	app.AddCommand(
		&rungrad.Command{
			Use:          "show",
			Short:        "show metadata",
			OutputModes:  []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
			SupportsMeta: true,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				if probe != nil {
					probe.readRan = true
				}
				return f.WriteResultWithMeta(map[string]string{"id": "alpha"}, meta, func(w io.Writer) {
					fmt.Fprintln(w, "alpha")
				})
			},
		},
		&rungrad.Command{
			Use:          "empty",
			Short:        "empty metadata",
			OutputModes:  []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			SupportsMeta: true,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return f.WriteResultWithMeta(map[string]string{"id": "beta"}, output.Meta{}, func(w io.Writer) {
					fmt.Fprintln(w, "beta")
				})
			},
		},
		&rungrad.Command{
			Use:          "zero-page",
			Short:        "zero page metadata",
			OutputModes:  []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			SupportsMeta: true,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				meta := output.Meta{Pagination: &output.Pagination{TotalItems: intp(0), HasMore: boolp(false)}}
				return f.WriteResultWithMeta(map[string]string{"id": "zero"}, meta, nil)
			},
		},
		&rungrad.Command{
			Use:          "plain-meta",
			Short:        "plain metadata",
			OutputModes:  []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModePlain},
			SupportsMeta: true,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return f.WriteOutput(rungrad.Output{
					Model: map[string]string{"id": "gamma"},
					Meta:  output.Meta{RequestID: "req-g"},
					Human: func(w io.Writer) { fmt.Fprintln(w, "gamma human") },
					Plain: func(w io.Writer) { fmt.Fprintln(w, "gamma") },
				})
			},
		},
		&rungrad.Command{
			Use:         "nometa",
			Short:       "no metadata",
			OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				if probe != nil {
					probe.humanRan = true
				}
				return f.WriteResult(map[string]string{"id": "none"}, nil)
			},
		},
	)
	return app
}

func TestIncludeMetaEnvelopeShape(t *testing.T) {
	res := testutil.Run(newMetaApp(true, nil), "show", "--include-meta", "--json")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	want := "{\n" +
		"  \"data\": {\n" +
		"    \"id\": \"alpha\"\n" +
		"  },\n" +
		"  \"meta\": {\n" +
		"    \"request_id\": \"req-1\",\n" +
		"    \"request_ids\": [\n" +
		"      \"req-1\",\n" +
		"      \"req-0\"\n" +
		"    ],\n" +
		"    \"pagination\": {\n" +
		"      \"page\": 1,\n" +
		"      \"per_page\": 2,\n" +
		"      \"total_items\": 5,\n" +
		"      \"has_more\": true\n" +
		"    },\n" +
		"    \"rate_limit\": {\n" +
		"      \"limit\": 100,\n" +
		"      \"remaining\": 99\n" +
		"    },\n" +
		"    \"retry\": {\n" +
		"      \"attempts\": 2,\n" +
		"      \"waits_ms\": [\n" +
		"        500\n" +
		"      ]\n" +
		"    },\n" +
		"    \"idempotency\": {\n" +
		"      \"key\": \"idem-1\",\n" +
		"      \"replayed\": false\n" +
		"    },\n" +
		"    \"extra\": {\n" +
		"      \"region\": \"us-1\"\n" +
		"    }\n" +
		"  }\n" +
		"}\n"
	if res.Stdout != want {
		t.Fatalf("stdout =\n%s\nwant\n%s", res.Stdout, want)
	}
}

func TestIncludeMetaPaginationFalseAndZero(t *testing.T) {
	res := testutil.Run(newMetaApp(true, nil), "zero-page", "--include-meta", "--json")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	for _, want := range []string{`"total_items": 0`, `"has_more": false`} {
		if !strings.Contains(res.Stdout, want) {
			t.Fatalf("stdout missing %s:\n%s", want, res.Stdout)
		}
	}
}

func TestIncludeMetaEmptyMetaDeterministicShape(t *testing.T) {
	app := newMetaApp(true, nil)
	first := testutil.Run(app, "empty", "--include-meta", "--json")
	if first.Exit != rungrad.ExitSuccess {
		t.Fatalf("first exit %d stderr=%q", first.Exit, first.Stderr)
	}
	want := "{\n" +
		"  \"data\": {\n" +
		"    \"id\": \"beta\"\n" +
		"  },\n" +
		"  \"meta\": {}\n" +
		"}\n"
	if first.Stdout != want {
		t.Fatalf("stdout =\n%s\nwant\n%s", first.Stdout, want)
	}
	second := testutil.Run(app, "empty", "--include-meta", "--json")
	if second.Exit != rungrad.ExitSuccess {
		t.Fatalf("second exit %d stderr=%q", second.Exit, second.Stderr)
	}
	if first.Stdout != second.Stdout {
		t.Fatalf("metadata envelope not repeatable:\n%s\n---\n%s", first.Stdout, second.Stdout)
	}
}

func TestIncludeMetaWithoutFlagOmitsEnvelope(t *testing.T) {
	res := testutil.Run(newMetaApp(true, nil), "show", "--json")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	want := "{\n  \"id\": \"alpha\"\n}\n"
	if res.Stdout != want {
		t.Fatalf("stdout = %q, want %q", res.Stdout, want)
	}
	if strings.Contains(res.Stdout, `"data"`) || strings.Contains(res.Stdout, `"meta"`) {
		t.Fatalf("stdout unexpectedly contains envelope keys:\n%s", res.Stdout)
	}
}

func TestIncludeMetaTransformsSeeEnvelope(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"jq meta", []string{"show", "--include-meta", "--jq", ".meta.request_id"}, "\"req-1\"\n"},
		{"jq data", []string{"show", "--include-meta", "--jq", ".data.id"}, "\"alpha\"\n"},
		{"template meta", []string{"show", "--include-meta", "--template", "{{.meta.pagination.total_items}}"}, "5\n"},
		{"jq without meta", []string{"show", "--jq", ".id"}, "\"alpha\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := testutil.Run(newMetaApp(true, nil), tt.args...)
			if res.Exit != rungrad.ExitSuccess {
				t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
			}
			if res.Stdout != tt.want {
				t.Fatalf("stdout = %q, want %q", res.Stdout, tt.want)
			}
		})
	}
}

func TestIncludeMetaUnsupportedFailsBeforeHandler(t *testing.T) {
	probe := &advancedOutputProbe{}
	res := testutil.Run(newMetaApp(true, probe), "nometa", "--include-meta", "--json")
	assertExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, "does not support --include-meta")
	if probe.humanRan {
		t.Fatal("handler ran for unsupported --include-meta")
	}
}

func TestIncludeMetaRejectedWithPlain(t *testing.T) {
	res := testutil.Run(newMetaApp(true, nil), "show", "--include-meta", "--plain")
	assertExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, "--plain cannot be combined")
}

func TestIncludeMetaRejectedWithDryRun(t *testing.T) {
	res := testutil.Run(newMetaApp(true, nil), "show", "--include-meta", "--dry-run", "--json")
	assertExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, "--include-meta cannot be combined with --dry-run")
}

func TestIncludeMetaRequiresMachineOutput(t *testing.T) {
	probe := &advancedOutputProbe{}
	res := testutil.Run(newMetaApp(true, probe), "show", "--include-meta")
	assertExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, "--include-meta requires --json, --jq, or --template")
	if probe.readRan {
		t.Fatal("handler ran for --include-meta without machine output")
	}
}

func TestIncludeMetaPlainCommandMachinePathWrapsMeta(t *testing.T) {
	json := testutil.Run(newMetaApp(true, nil), "plain-meta", "--include-meta", "--json")
	if json.Exit != rungrad.ExitSuccess {
		t.Fatalf("json exit %d stderr=%q", json.Exit, json.Stderr)
	}
	wantJSON := "{\n" +
		"  \"data\": {\n" +
		"    \"id\": \"gamma\"\n" +
		"  },\n" +
		"  \"meta\": {\n" +
		"    \"request_id\": \"req-g\"\n" +
		"  }\n" +
		"}\n"
	if json.Stdout != wantJSON {
		t.Fatalf("json stdout =\n%s\nwant\n%s", json.Stdout, wantJSON)
	}

	plain := testutil.RunWith(newMetaApp(true, nil), testutil.Options{
		OutputTerminal:    true,
		OutputTerminalSet: true,
	}, "plain-meta", "--plain")
	if plain.Exit != rungrad.ExitSuccess {
		t.Fatalf("plain exit %d stderr=%q", plain.Exit, plain.Stderr)
	}
	if plain.Stdout != "gamma\n" {
		t.Fatalf("plain stdout = %q", plain.Stdout)
	}

	conflict := testutil.Run(newMetaApp(true, nil), "plain-meta", "--include-meta", "--plain")
	assertExitStdoutEmptyStderrContains(t, conflict, rungrad.ExitUsage, "--plain cannot be combined")
}

func TestIncludeMetaNonAdvancedAppRejectsFlag(t *testing.T) {
	res := testutil.Run(newMetaApp(false, nil), "show", "--include-meta", "--json")
	assertExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, "unknown flag: --include-meta")
}

func TestIncludeMetaManifestAdvertisesSupport(t *testing.T) {
	m, res := readManifest(t, newMetaApp(true, nil))
	if err := manifest.Validate(&m); err != nil {
		t.Fatalf("Validate(manifest) = %v\n%s", err, res.Stdout)
	}
	show := findManifestCommand(&m, "show")
	if show == nil || !show.SupportsMeta {
		t.Fatalf("show supports_meta = %+v", show)
	}
	nometa := findManifestCommand(&m, "nometa")
	if nometa == nil || nometa.SupportsMeta {
		t.Fatalf("nometa supports_meta = %+v", nometa)
	}
	if flag := findManifestFlag(m.GlobalFlags, "include-meta"); flag == nil {
		t.Fatalf("advanced manifest missing include-meta flag: %+v", m.GlobalFlags)
	}

	plain, _ := readManifest(t, newMetaApp(false, nil))
	if flag := findManifestFlag(plain.GlobalFlags, "include-meta"); flag != nil {
		t.Fatalf("non-advanced manifest unexpectedly contains include-meta: %+v", *flag)
	}
}
