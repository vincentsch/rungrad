package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/vincentsch/rungrad/conformance"
)

// buildRef compiles the reference CLI to a temporary binary so the conformance
// runner can drive it as a real subprocess.
func buildRef(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "rgref")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build rgref: %v\n%s", err, out)
	}
	return bin
}

// TestReferenceCLIScoresPerfect closes the loop: the framework's own reference
// tool is scored against the spec the framework ships, and must pass every
// applicable rule.
func TestReferenceCLIScoresPerfect(t *testing.T) {
	bin := buildRef(t)

	rs, err := conformance.DefaultRuleset()
	if err != nil {
		t.Fatal(err)
	}
	runner, err := conformance.NewRunner(conformance.Target{
		Path:        bin,
		Read:        []string{"item", "list"},
		Mutate:      []string{"item", "create", "demo"},
		Auth:        []string{"whoami"},
		Ambiguous:   []string{"item", "get", "dup"},
		NotFound:    []string{"item", "get", "ghost"},
		APIError:    []string{"item", "get", "broken"},
		Forbidden:   []string{"item", "get", "forbidden"},
		RateLimited: []string{"item", "get", "throttled"},
		Destructive: []string{"item", "delete", "alpha"},
		HasUpdate:   true,
		Secret:      []string{"whoami"},
		SecretEnv:   "RGREF_TOKEN",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	if err := runner.DiscoverManifest(); err != nil {
		t.Fatalf("DiscoverManifest: %v", err)
	}
	score := runner.Score(rs)
	t.Log("\n" + score.Report())

	if score.Manifest.Status != conformance.ManifestPresent {
		t.Fatalf("manifest status = %q, want %q", score.Manifest.Status, conformance.ManifestPresent)
	}
	if fails := score.RequiredFailures(); len(fails) > 0 {
		t.Fatalf("required rules failed: %v", fails)
	}
	if score.Overall < 1.0 {
		// Surface which applicable rules did not pass.
		for _, r := range score.Rules {
			if r.Result == conformance.ResultFail {
				t.Errorf("FAIL %s: %s", r.ID, r.Reason)
			}
		}
		t.Fatalf("overall score %.2f, want 1.00", score.Overall)
	}
}
