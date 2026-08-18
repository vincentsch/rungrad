package update_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"runtime"
	"strings"
	"testing"

	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/update"
)

func TestEvaluate(t *testing.T) {
	cases := []struct {
		current string
		latest  string
		want    update.Status
		avail   bool
	}{
		{"v1.0.0", "v1.0.0", update.StatusUpToDate, false},
		{"1.0.0", "1.2.0", update.StatusUpdateAvailable, true},
		{"v2.0.0", "v1.9.9", update.StatusNewerThanLatest, false},
		{"dev", "v1.0.0", update.StatusDevelopmentBuild, false},
		{"v1.2.3-rc1", "v1.2.3", update.StatusUpToDate, false},
		{"v1.2.3", "not-a-version", update.StatusUnknownLatest, false},
	}
	for _, tc := range cases {
		got := update.Evaluate(tc.current, update.Release{Version: tc.latest})
		if got.Status != tc.want || got.Available != tc.avail {
			t.Errorf("Evaluate(%q,%q) = {%s, avail=%v}, want {%s, avail=%v}",
				tc.current, tc.latest, got.Status, got.Available, tc.want, tc.avail)
		}
	}
}

func TestCommandToolNameExamples(t *testing.T) {
	cmd := update.Command(update.CommandConfig{ToolName: "rgdemo"})
	want := []string{"rgdemo update --check", "rgdemo update --check --json", "rgdemo update"}
	if !reflect.DeepEqual(cmd.Examples, want) {
		t.Fatalf("examples = %v, want %v", cmd.Examples, want)
	}
	if len(cmd.Related) != 0 {
		t.Fatalf("related = %v, want [] (no version subcommand exists)", cmd.Related)
	}
}

func TestCommandEmptyToolNameFallsBackToMytool(t *testing.T) {
	cmd := update.Command(update.CommandConfig{})
	want := []string{"mytool update --check", "mytool update --check --json", "mytool update"}
	if !reflect.DeepEqual(cmd.Examples, want) {
		t.Fatalf("fallback examples = %v, want %v", cmd.Examples, want)
	}
}

type stubFetcher struct{ rel update.Release }

func (s stubFetcher) Latest() (update.Release, error) { return s.rel, nil }

func TestUpdateCheckIsReadonlyJSON(t *testing.T) {
	app := rungrad.New(rungrad.AppConfig{Name: "rgdemo", Version: "v1.0.0"})
	app.AddCommand(update.Command(update.CommandConfig{
		CurrentVersion: "v1.0.0",
		Fetcher:        stubFetcher{rel: update.Release{Version: "v1.0.0"}},
		Apply: func(update.Release) error {
			t.Fatal("Apply must not be called on --check")
			return nil
		},
	}))
	var out, errb bytes.Buffer
	code := app.Run([]string{"update", "--check", "--json"}, &out, &errb)
	if code != rungrad.ExitSuccess {
		t.Fatalf("exit = %d (stderr=%q)", code, errb.String())
	}
	if !json.Valid(out.Bytes()) {
		t.Fatalf("update --check --json not valid JSON: %q", out.String())
	}
	if !strings.Contains(out.String(), "up_to_date") {
		t.Fatalf("expected up_to_date status: %q", out.String())
	}
}

func TestUpdateDryRunDoesNotApply(t *testing.T) {
	called := false
	app := rungrad.New(rungrad.AppConfig{Name: "rgdemo", Version: "v1.0.0"})
	app.AddCommand(update.Command(update.CommandConfig{
		CurrentVersion: "v1.0.0",
		Fetcher:        stubFetcher{rel: update.Release{Version: "v1.1.0"}},
		Apply: func(update.Release) error {
			called = true
			return nil
		},
	}))
	var out, errb bytes.Buffer
	code := app.Run([]string{"update", "--dry-run", "--json"}, &out, &errb)
	if code != rungrad.ExitSuccess {
		t.Fatalf("exit = %d (stderr=%q)", code, errb.String())
	}
	if called {
		t.Fatalf("Apply was called during --dry-run")
	}
	var result update.Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if result.Status != update.StatusUpdateAvailable || !result.Available || result.Current != "v1.0.0" {
		t.Fatalf("dry-run should report available update without installing, got %+v", result)
	}
}

func TestUpdateApplyJSONReportsInstalledState(t *testing.T) {
	called := false
	app := rungrad.New(rungrad.AppConfig{Name: "rgdemo", Version: "v1.0.0"})
	app.AddCommand(update.Command(update.CommandConfig{
		CurrentVersion: "v1.0.0",
		Fetcher:        stubFetcher{rel: update.Release{Version: "v1.1.0"}},
		Apply: func(update.Release) error {
			called = true
			return nil
		},
	}))
	var out, errb bytes.Buffer
	code := app.Run([]string{"update", "--json"}, &out, &errb)
	if code != rungrad.ExitSuccess {
		t.Fatalf("exit = %d (stderr=%q)", code, errb.String())
	}
	if !called {
		t.Fatalf("Apply was not called")
	}
	var result update.Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if result.Status != update.StatusUpToDate || result.Available || result.Current != "v1.1.0" {
		t.Fatalf("installed result should report up-to-date state, got %+v", result)
	}
}

func TestAssetForMatchesPlatformAndIgnoresIntegrityArtifacts(t *testing.T) {
	archAlias := runtime.GOARCH
	if runtime.GOARCH == "amd64" {
		archAlias = "x86_64"
	}
	rel := update.Release{Assets: []update.Asset{
		{Name: "tool_" + runtime.GOOS + "_" + runtime.GOARCH + ".sha256", URL: "checksum"},
		{Name: "tool_" + runtime.GOOS + "_" + archAlias, URL: "binary"},
	}}
	a, ok := update.AssetFor(rel)
	if !ok {
		t.Fatalf("expected platform asset to match")
	}
	if a.URL != "binary" {
		t.Fatalf("matched wrong asset: %+v", a)
	}
}
