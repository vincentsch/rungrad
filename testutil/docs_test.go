package testutil

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/docsgen"
)

func docsApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{Name: "rgdocs", Short: "docs test CLI", Version: "0.0.0"})
	app.AddCommand(&rungrad.Command{
		Use:         "ping",
		Short:       "Ping a service",
		Examples:    []string{"rgdocs ping"},
		Related:     []string{"rgdocs pong"},
		OutputModes: []string{"table", "json"},
		Run:         func(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil },
	})
	return app
}

func TestCaptureManifest(t *testing.T) {
	m := CaptureManifest(t, docsApp())
	if m.ToolName != "rgdocs" {
		t.Fatalf("tool name = %q, want rgdocs", m.ToolName)
	}
	if findManifestPath(m.Commands, "ping") == nil {
		t.Fatalf("manifest missing ping command: %+v", m.Commands)
	}
}

func TestAssertDocsInSyncRoundTrip(t *testing.T) {
	dir := t.TempDir()
	AssertDocsInSync(t, docsApp, true, dir)
	AssertDocsInSync(t, docsApp, false, dir)
}

func TestAssertDocsInSyncUsesCheckResult(t *testing.T) {
	dir := t.TempDir()
	if err := docsgen.Write(docsApp(), dir); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "rgdocs.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rgdocs_ping.md"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := docsgen.Check(docsApp(), dir)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !reflect.DeepEqual(result.Missing, []string{"rgdocs.md"}) {
		t.Fatalf("missing = %v", result.Missing)
	}
	if !reflect.DeepEqual(result.Stale, []string{"rgdocs_ping.md"}) {
		t.Fatalf("stale = %v", result.Stale)
	}
	if !reflect.DeepEqual(result.Orphaned, []string{"old.md"}) {
		t.Fatalf("orphaned = %v", result.Orphaned)
	}
}

func TestAssertHelpGoldensRoundTrip(t *testing.T) {
	dir := t.TempDir()
	AssertHelpGoldens(t, docsApp, true, dir)
	got := sortedFileNames(t, dir)
	want := []string{"rgdocs.txt", "rgdocs_ping.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("help golden files = %v, want %v", got, want)
	}
	AssertHelpGoldens(t, docsApp, false, dir)
}
