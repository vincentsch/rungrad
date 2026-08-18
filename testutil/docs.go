package testutil

import (
	"encoding/json"
	"strings"
	"testing"

	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/docsgen"
	"github.com/vincentsch/rungrad/manifest"
)

// AssertDocsInSync regenerates the committed docs under dir from a fresh app
// when update is true (via docsgen.Write) and otherwise fails the test with the
// missing, stale, and orphaned paths reported by docsgen.Check, plus a
// regenerate hint. newApp builds a fresh app per operation.
func AssertDocsInSync(t *testing.T, newApp func() *rungrad.App, update bool, dir string) {
	t.Helper()
	if update {
		if err := docsgen.Write(newApp(), dir); err != nil {
			t.Fatalf("write generated docs in %s: %v", dir, err)
		}
		return
	}
	result, err := docsgen.Check(newApp(), dir)
	if err != nil {
		t.Fatalf("check generated docs in %s: %v", dir, err)
	}
	if !result.OK() {
		t.Fatalf("generated docs in %s are out of sync; regenerate with this package's -update test flag.\n%s",
			dir, formatPathBuckets(result.Missing, result.Stale, result.Orphaned))
	}
}

// AssertHelpGoldens captures --help for every visible command of a fresh app
// and golden-checks dir, one <command-path>.txt file per command (root => the
// tool name; subcommands => the tool name plus underscore-joined path segments).
// When update is true it rewrites and prunes the directory; otherwise it fails
// on drift with a regenerate hint.
func AssertHelpGoldens(t *testing.T, newApp func() *rungrad.App, update bool, dir string) {
	t.Helper()
	app := newApp()
	tool := app.Root().Name()
	want := map[string]string{}
	for path, content := range CaptureAllHelp(app) {
		name := tool + ".txt"
		if path != "" {
			name = tool + "_" + strings.ReplaceAll(path, " ", "_") + ".txt"
		}
		want[name] = content
	}
	AssertGoldenDir(t, update, dir, want)
}

// CaptureManifest runs __rungrad_manifest in process, fails the test if the
// command errors, the output is not JSON, or manifest.Validate rejects it, and
// returns the parsed manifest.
func CaptureManifest(t *testing.T, app *rungrad.App) manifest.Manifest {
	t.Helper()
	res := Run(app, "__rungrad_manifest")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("__rungrad_manifest exit %d: %s", res.Exit, res.Stderr)
	}
	var m manifest.Manifest
	if err := json.Unmarshal([]byte(res.Stdout), &m); err != nil {
		t.Fatalf("__rungrad_manifest JSON: %v\n%s", err, res.Stdout)
	}
	if err := manifest.Validate(&m); err != nil {
		t.Fatalf("manifest.Validate: %v\n%s", err, res.Stdout)
	}
	return m
}
