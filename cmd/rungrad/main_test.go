package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/conformance"
	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/scaffold"
	"github.com/vincentsch/rungrad/testutil"
)

func TestNewDryRunListsFiles(t *testing.T) {
	res := testutil.Run(newApp(), "new", "demo", "--dry-run", "--json")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d: %s", res.Exit, res.Stderr)
	}
	if !json.Valid([]byte(res.Stdout)) {
		t.Fatalf("dry-run --json not valid JSON: %s", res.Stdout)
	}
}

func TestNewWritesProject(t *testing.T) {
	dir := t.TempDir()
	res := testutil.Run(newApp(), "new", "demo", "--dir", dir)
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d: %s", res.Exit, res.Stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "demo", "go.mod")); err != nil {
		t.Fatalf("expected go.mod in scaffold: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "demo", "main.go")); err != nil {
		t.Fatalf("expected main.go in scaffold: %v", err)
	}
}

func TestNewRefusesNonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if res := testutil.Run(newApp(), "new", "demo", "--dir", dir); res.Exit != 0 {
		t.Fatalf("first scaffold failed: %s", res.Stderr)
	}
	res := testutil.Run(newApp(), "new", "demo", "--dir", dir)
	if res.Exit == rungrad.ExitSuccess {
		t.Fatalf("expected failure scaffolding over an existing project")
	}
}

func TestNewInvalidNameExitsUsage(t *testing.T) {
	res := testutil.Run(newApp(), "new", "my tool", "--dir", t.TempDir())
	if res.Exit != rungrad.ExitUsage {
		t.Fatalf("invalid name exit = %d, want %d (stderr=%q)", res.Exit, rungrad.ExitUsage, res.Stderr)
	}
}

func TestNewProductFlagWithoutProfileExitsUsage(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{name: "non-empty env prefix", args: []string{"--env-prefix", "ACME"}, want: "--env-prefix requires --product-profile"},
		{name: "empty surface", args: []string{"--surface="}, want: "--surface requires --product-profile"},
		{name: "empty product name", args: []string{"--product-name="}, want: "--product-name requires --product-profile"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"new", "demo", "--dry-run"}, tt.args...)
			res := testutil.Run(newApp(), args...)
			if res.Exit != rungrad.ExitUsage {
				t.Fatalf("product flag without profile exit = %d, want %d (stderr=%q)", res.Exit, rungrad.ExitUsage, res.Stderr)
			}
			if !strings.Contains(res.Stderr, tt.want) {
				t.Fatalf("stderr did not explain missing product profile: %q", res.Stderr)
			}
		})
	}
}

func TestNewProductProfileRejectsEmptyRepeatableFlags(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{name: "empty service", args: []string{"--service="}, want: "service must be name=url"},
		{name: "empty example", args: []string{"--example="}, want: "example must not be empty"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"new", "demo", "--dry-run", "--product-profile"}, tt.args...)
			res := testutil.Run(newApp(), args...)
			if res.Exit != rungrad.ExitUsage {
				t.Fatalf("empty repeatable flag exit = %d, want %d (stderr=%q)", res.Exit, rungrad.ExitUsage, res.Stderr)
			}
			if !strings.Contains(res.Stderr, tt.want) {
				t.Fatalf("stderr did not explain empty repeatable flag: %q", res.Stderr)
			}
		})
	}
}

func TestScoreReferenceCLIJSON(t *testing.T) {
	bin := buildRefCLI(t)
	res := testutil.Run(newApp(),
		"score", bin,
		"--read", "item list",
		"--mutate", "item create demo",
		"--auth", "whoami",
		"--ambiguous", "item get dup",
		"--not-found", "item get ghost",
		"--api-error", "item get broken",
		"--forbidden", "item get forbidden",
		"--rate-limited", "item get throttled",
		"--destructive", "item delete alpha",
		"--secret", "whoami",
		"--secret-env", "RGREF_TOKEN",
		"--update",
		"--json",
	)
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("score exit %d: %s", res.Exit, res.Stderr)
	}
	var score conformance.Score
	if err := json.Unmarshal([]byte(res.Stdout), &score); err != nil {
		t.Fatalf("score JSON invalid: %v\n%s", err, res.Stdout)
	}
	if score.Overall != 1 || score.Applicable != 22 {
		t.Fatalf("unexpected score: overall=%v applicable=%d\n%s", score.Overall, score.Applicable, res.Stdout)
	}
	if score.Manifest.Status != conformance.ManifestPresent {
		t.Fatalf("manifest status = %q, want %q\n%s", score.Manifest.Status, conformance.ManifestPresent, res.Stdout)
	}
	if score.Manifest.SchemaVersion != manifest.SchemaVersion {
		t.Fatalf("manifest schema_version = %q, want %q", score.Manifest.SchemaVersion, manifest.SchemaVersion)
	}
	if score.Manifest.ToolName == "" {
		t.Fatalf("manifest tool_name is empty\n%s", res.Stdout)
	}
	if score.Manifest.UsedRuleCount == 0 {
		t.Fatalf("used_rule_count = 0, want manifest-backed rules\n%s", res.Stdout)
	}
	if len(score.Manifest.UsedRules) != score.Manifest.UsedRuleCount {
		t.Fatalf("used_rule_count = %d, len used_rules = %d\n%s", score.Manifest.UsedRuleCount, len(score.Manifest.UsedRules), res.Stdout)
	}
	if !sort.StringsAreSorted(score.Manifest.UsedRules) {
		t.Fatalf("used_rules are not sorted: %v", score.Manifest.UsedRules)
	}
	if !containsString(score.Manifest.UsedRules, "output.json-parseable") {
		t.Fatalf("used_rules missing output.json-parseable: %v", score.Manifest.UsedRules)
	}
	if containsString(score.Manifest.UsedRules, "exit.unknown-usage") {
		t.Fatalf("used_rules unexpectedly contains exit.unknown-usage: %v", score.Manifest.UsedRules)
	}
}

func TestScoreReferenceCLIOmittedBackendFixtures(t *testing.T) {
	bin := buildRefCLI(t)
	res := testutil.Run(newApp(),
		"score", bin,
		"--read", "item list",
		"--mutate", "item create demo",
		"--auth", "whoami",
		"--ambiguous", "item get dup",
		"--not-found", "item get ghost",
		"--secret", "whoami",
		"--secret-env", "RGREF_TOKEN",
		"--update",
		"--json",
	)
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("score exit %d: %s", res.Exit, res.Stderr)
	}
	var score conformance.Score
	if err := json.Unmarshal([]byte(res.Stdout), &score); err != nil {
		t.Fatalf("score JSON invalid: %v\n%s", err, res.Stdout)
	}
	if score.Overall != 1 || score.Applicable != 17 {
		t.Fatalf("unexpected score without backend fixtures: overall=%v applicable=%d\n%s", score.Overall, score.Applicable, res.Stdout)
	}
	results := map[string]string{}
	for _, rule := range score.Rules {
		results[rule.ID] = rule.Result
	}
	for _, id := range []string{"exit.api-error", "exit.forbidden", "exit.rate-limited", "dryrun.destructive-preview", "dryrun.destructive-confirm-required"} {
		if results[id] != conformance.ResultNotApplicable {
			t.Fatalf("%s result = %q, want %q\n%s", id, results[id], conformance.ResultNotApplicable, res.Stdout)
		}
	}
}

func TestScoreScaffoldedProjectJSON(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// Score a real generated binary instead of an in-process app so the smoke
	// covers scaffold generation, module wiring, build output, manifest discovery,
	// and scorer subprocess behavior together.
	root, err := scaffold.Write(t.TempDir(), scaffold.Options{Name: "scored", RungradReplace: repoRoot})
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOFLAGS=-mod=mod")

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = root
	tidy.Env = env
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	bin := filepath.Join(root, "scored")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = root
	build.Env = env
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build scaffolded project failed: %v\n%s", err, out)
	}

	runGeneratedReadmeCommands(t, bin)

	res := testutil.Run(newApp(),
		"score", bin,
		"--read", "widget list",
		"--mutate", "widget create demo",
		"--destructive", "widget delete alpha",
		"--update",
		"--json",
	)
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("score exit %d: %s", res.Exit, res.Stderr)
	}
	var score conformance.Score
	if err := json.Unmarshal([]byte(res.Stdout), &score); err != nil {
		t.Fatalf("score JSON invalid: %v\n%s", err, res.Stdout)
	}
	if score.Overall != 1 || score.Passed != 15 || score.Applicable != 15 {
		t.Fatalf("unexpected scaffold score: overall=%v passed=%d applicable=%d\n%s", score.Overall, score.Passed, score.Applicable, res.Stdout)
	}
	if score.Manifest.Status != conformance.ManifestPresent {
		t.Fatalf("manifest status = %q, want %q", score.Manifest.Status, conformance.ManifestPresent)
	}
	if score.Manifest.UsedRuleCount == 0 {
		t.Fatalf("used_rule_count = 0, want manifest-backed rules\n%s", res.Stdout)
	}
	results := map[string]string{}
	for _, rule := range score.Rules {
		if rule.Result == conformance.ResultFail {
			t.Errorf("applicable rule %s failed: %s", rule.ID, rule.Reason)
		}
		results[rule.ID] = rule.Result
	}
	for _, id := range []string{
		"exit.missing-credential-auth",
		"resolution.no-prompt",
		"exit.not-found",
		"exit.api-error",
		"exit.forbidden",
		"exit.rate-limited",
		"auth.secret-not-printed",
	} {
		if results[id] != conformance.ResultNotApplicable {
			t.Errorf("%s = %q, want %q", id, results[id], conformance.ResultNotApplicable)
		}
	}
	for _, id := range []string{"update.check-readonly", "update.check-json", "dryrun.destructive-preview", "dryrun.destructive-confirm-required"} {
		if results[id] != conformance.ResultPass {
			t.Errorf("%s = %q, want %q", id, results[id], conformance.ResultPass)
		}
	}
}

func TestScoreAdopterFixtureJSON(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// Build the committed internal main so scoring exercises the same
	// subprocess manifest discovery path an external target would use.
	bin := filepath.Join(t.TempDir(), "adopterfixture")
	build := exec.Command("go", "build", "-o", bin, "./internal/adopterfixture/adopterbin")
	build.Dir = repoRoot
	build.Env = append(os.Environ(), "GOPROXY=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build adopter fixture: %v\n%s", err, out)
	}

	res := testutil.Run(newApp(),
		"score", bin,
		"--read", "widget list",
		"--mutate", "widget create demo",
		"--destructive", "widget delete alpha",
		"--update",
		"--json",
	)
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("score exit %d: %s", res.Exit, res.Stderr)
	}
	var score conformance.Score
	if err := json.Unmarshal([]byte(res.Stdout), &score); err != nil {
		t.Fatalf("score JSON invalid: %v\n%s", err, res.Stdout)
	}
	if score.Manifest.Status != conformance.ManifestPresent {
		t.Fatalf("manifest status = %q, want %q", score.Manifest.Status, conformance.ManifestPresent)
	}
	if score.Manifest.UsedRuleCount == 0 {
		t.Fatalf("used_rule_count = 0, want manifest-backed rules\n%s", res.Stdout)
	}
	for _, rule := range score.Rules {
		if rule.Result == conformance.ResultFail {
			t.Errorf("applicable rule %s failed: %s", rule.ID, rule.Reason)
		}
	}
}

func TestProductProfileDefaults(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	root, err := scaffold.Write(t.TempDir(), scaffold.Options{
		Name:           "prodtool",
		ProductProfile: true,
		RungradReplace: repoRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOFLAGS=-mod=mod")

	runGo(t, root, env, "mod", "tidy")
	runGo(t, root, env, "test", "./...")

	bin := filepath.Join(root, "prodtool")
	runGo(t, root, env, "build", "-o", bin, ".")
	runGeneratedReadmeCommands(t, bin)
	assertScaffoldScorePerfect(t, bin)
}

func TestProductProfileHostSurfaceCLISmoke(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	res := testutil.Run(newApp(),
		"new", "acmectl",
		"--dir", tmp,
		"--module", "example.com/acme/acmectl",
		"--product-profile",
		"--env-prefix", "ACME",
		"--product-name", "Acme Control",
		"--description", "Manage Acme services",
		"--service", "api=https://api.example.invalid",
		"--metadata-namespace", "example.com/acme",
		"--surface", "host",
		"--release-owner", "example",
		"--release-repo", "acmectl",
		"--docs-label", "Acme CLI",
		"--example", "acmectl widget list",
	)
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("new product profile exit %d: %s", res.Exit, res.Stderr)
	}

	root := filepath.Join(tmp, "acmectl")
	env := append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOFLAGS=-mod=mod")
	runGo(t, root, env, "mod", "edit", "-replace", "github.com/vincentsch/rungrad="+repoRoot)
	runGo(t, root, env, "mod", "tidy")
	runGo(t, root, env, "test", "./...")

	bin := filepath.Join(root, "acmectl")
	runGo(t, root, env, "build", "-o", bin, ".")
	runGeneratedReadmeCommands(t, bin)
	assertScaffoldScorePerfect(t, bin)
}

func TestScoreReferenceCLIManifestOff(t *testing.T) {
	bin := buildRefCLI(t)
	res := testutil.Run(newApp(),
		"score", bin,
		"--read", "item list",
		"--manifest", "off",
		"--json",
	)
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("score exit %d: %s", res.Exit, res.Stderr)
	}
	var score conformance.Score
	if err := json.Unmarshal([]byte(res.Stdout), &score); err != nil {
		t.Fatalf("score JSON invalid: %v\n%s", err, res.Stdout)
	}
	if score.Manifest.Status != conformance.ManifestDisabled {
		t.Fatalf("manifest status = %q, want %q\n%s", score.Manifest.Status, conformance.ManifestDisabled, res.Stdout)
	}
}

func TestScoreManifestRequiredFailsWithoutManifest(t *testing.T) {
	bin := buildBadFixture(t)
	res := testutil.Run(newApp(), "score", bin, "--read", "list", "--manifest", "required")
	if res.Exit != rungrad.ExitUsage {
		t.Fatalf("exit = %d, want %d (stderr=%q)", res.Exit, rungrad.ExitUsage, res.Stderr)
	}
	if res.Stdout != "" {
		t.Fatalf("stdout = %q, want empty", res.Stdout)
	}
}

func TestScoreUnknownManifestModeExitsUsage(t *testing.T) {
	bin := buildRefCLI(t)
	res := testutil.Run(newApp(), "score", bin, "--manifest", "bogus")
	if res.Exit != rungrad.ExitUsage {
		t.Fatalf("exit = %d, want %d (stderr=%q)", res.Exit, rungrad.ExitUsage, res.Stderr)
	}
}

func TestScoreReferenceCLIHumanManifestLine(t *testing.T) {
	bin := buildRefCLI(t)
	res := testutil.Run(newApp(), "score", bin, "--read", "item list")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("score exit %d: %s", res.Exit, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Manifest: present (") {
		t.Fatalf("manifest line missing:\n%s", res.Stdout)
	}
}

func TestScoreMissingTargetExitsUsage(t *testing.T) {
	res := testutil.Run(newApp(), "score", filepath.Join(t.TempDir(), "missing"))
	if res.Exit != rungrad.ExitUsage {
		t.Fatalf("missing target exit = %d, want %d (stderr=%q)", res.Exit, rungrad.ExitUsage, res.Stderr)
	}
}

func TestScoreStrictFailsOnRequiredFailures(t *testing.T) {
	bin := buildBadFixture(t)
	res := testutil.Run(newApp(), "score", bin, "--read", "list", "--strict")
	if res.Exit == rungrad.ExitSuccess {
		t.Fatalf("strict score unexpectedly succeeded:\nstdout=%s\nstderr=%s", res.Stdout, res.Stderr)
	}
}

func TestScoreStrictTripsOnManifestContradiction(t *testing.T) {
	bin := buildManifestContradictionFixture(t)
	res := testutil.Run(newApp(), "score", bin, "--read", "item list", "--strict")
	if res.Exit != rungrad.ExitUsage {
		t.Fatalf("exit = %d, want %d\nstdout=%s\nstderr=%s", res.Exit, rungrad.ExitUsage, res.Stdout, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "output.json-parseable") || !strings.Contains(res.Stdout, "manifest says") {
		t.Fatalf("strict output did not show manifest-backed required failure:\nstdout=%s\nstderr=%s", res.Stdout, res.Stderr)
	}
}

func buildRefCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "rgref")
	cmd := exec.Command("go", "build", "-o", bin, "../rgref")
	cmd.Env = append(os.Environ(), "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build rgref: %v\n%s", err, out)
	}
	return bin
}

func buildBadFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(`package main

import "os"

func main() { os.Exit(1) }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bad")
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Env = append(os.Environ(), "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build bad fixture: %v\n%s", err, out)
	}
	return bin
}

func buildManifestContradictionFixture(t *testing.T) string {
	t.Helper()
	// The manifest says item list has no JSON mode, while the binary really does
	// emit valid JSON. Strict scoring should fail because the manifest
	// contradiction is caught before the black-box JSON check can pass.
	m := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		SpecVersion:   "rungrad-spec/1",
		ToolName:      "contradict",
		GlobalFlags:   []manifest.Flag{},
		Commands: []manifest.Command{
			{
				Path:        []string{},
				Use:         "contradict",
				Examples:    []string{},
				Related:     []string{},
				OutputModes: []string{},
				LocalFlags:  []manifest.Flag{},
			},
			{
				Path:        []string{"item"},
				Use:         "item",
				Examples:    []string{},
				Related:     []string{},
				OutputModes: []string{},
				LocalFlags:  []manifest.Flag{},
			},
			{
				Path:        []string{"item", "list"},
				Use:         "list",
				Examples:    []string{},
				Related:     []string{},
				OutputModes: []string{"table"},
				LocalFlags:  []manifest.Flag{},
			},
		},
	}
	payload, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	code := `package main

import "os"

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "__rungrad_manifest" {
		os.Stdout.WriteString(` + strconvQuote(string(payload)) + `)
		return
	}
	if len(args) >= 2 && args[0] == "item" && args[1] == "list" {
		for _, arg := range args[2:] {
			if arg == "--json" {
				os.Stdout.WriteString("[\"alpha\"]\n")
				return
			}
		}
		os.Stdout.WriteString("alpha\n")
		return
	}
	os.Exit(1)
}
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "contradict")
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Env = append(os.Environ(), "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build contradiction fixture: %v\n%s", err, out)
	}
	return bin
}

func strconvQuote(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func runGo(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

func runGeneratedReadmeCommands(t *testing.T, bin string) {
	t.Helper()
	for _, args := range [][]string{
		{"widget", "list"},
		{"widget", "list", "--json"},
		{"widget", "create", "gamma", "--dry-run"},
		{"widget", "delete", "alpha", "--dry-run"},
		{"update", "--check"},
	} {
		cmd := exec.Command(bin, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %s failed: %v\n%s", bin, strings.Join(args, " "), err, out)
		}
	}
}

func assertScaffoldScorePerfect(t *testing.T, bin string) {
	t.Helper()
	res := testutil.Run(newApp(),
		"score", bin,
		"--read", "widget list",
		"--mutate", "widget create demo",
		"--destructive", "widget delete alpha",
		"--update",
		"--json",
	)
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("score exit %d: %s", res.Exit, res.Stderr)
	}
	var score conformance.Score
	if err := json.Unmarshal([]byte(res.Stdout), &score); err != nil {
		t.Fatalf("score JSON invalid: %v\n%s", err, res.Stdout)
	}
	if score.Overall != 1 {
		t.Fatalf("unexpected scaffold score: overall=%v\n%s", score.Overall, res.Stdout)
	}
	if score.Manifest.Status != conformance.ManifestPresent {
		t.Fatalf("manifest status = %q, want %q\n%s", score.Manifest.Status, conformance.ManifestPresent, res.Stdout)
	}
}
