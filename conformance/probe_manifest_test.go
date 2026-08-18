package conformance

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/spec"
)

func TestManifestCommandFor(t *testing.T) {
	m := baseManifest()
	cases := []struct {
		fixture []string
		want    []string
		matched bool
	}{
		{fixture: []string{"item", "list"}, want: []string{"item", "list"}, matched: true},
		{fixture: []string{"item", "create", "demo"}, want: []string{"item", "create"}, matched: true},
		{fixture: []string{"item", "get", "dup"}, want: []string{"item", "get"}, matched: true},
		{fixture: []string{"item"}, want: []string{"item"}, matched: true},
		{fixture: []string{"item", "bogus"}, want: []string{"item"}, matched: true},
		{fixture: []string{"update"}, want: []string{"update"}, matched: true},
		{fixture: []string{"nonexistent"}, matched: false},
		{fixture: []string{}, matched: false},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.fixture, " "), func(t *testing.T) {
			got, matched := manifestCommandFor(&m, tc.fixture)
			if matched != tc.matched {
				t.Fatalf("matched = %v, want %v", matched, tc.matched)
			}
			if !matched {
				return
			}
			if !equalPath(got.Path, tc.want) {
				t.Fatalf("path = %v, want %v", got.Path, tc.want)
			}
		})
	}

	noParent := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		SpecVersion:   spec.Version,
		ToolName:      "demo",
		GlobalFlags:   []manifest.Flag{},
		Commands: []manifest.Command{
			manifestTestCommand([]string{}, nil),
			manifestTestCommand([]string{"item", "list"}, nil),
		},
	}
	got, matched := manifestCommandFor(&noParent, []string{"item", "list"})
	if !matched {
		t.Fatal("item list without parent did not match")
	}
	if !equalPath(got.Path, []string{"item", "list"}) {
		t.Fatalf("path = %v, want [item list]", got.Path)
	}
}

func TestProbeUnmatchedFixturePathFails(t *testing.T) {
	m := baseManifest()
	r := newManifestProbeRunner(t, m, Target{Read: []string{"nope"}})
	requireProbe(t, probeSuccessExitZero(r), ResultFail, "manifest")

	r = newManifestProbeRunner(t, m, Target{NotFound: []string{"nope", "x"}})
	requireProbe(t, probeNotFound(r), ResultFail, "manifest")
}

func TestProbeReadOutputModeContradictions(t *testing.T) {
	m := baseManifest()
	commandByPath(t, &m, "read").OutputModes = []string{"table"}
	r := newManifestProbeRunner(t, m, Target{Read: []string{"read"}})
	requireProbe(t, probeJSONParseable(r), ResultFail, "manifest")
	requireProbe(t, probeDualForm(r), ResultFail, "manifest")

	m = baseManifest()
	commandByPath(t, &m, "read").OutputModes = []string{"json"}
	r = newManifestProbeRunner(t, m, Target{Read: []string{"read"}})
	requireProbe(t, probeJSONParseable(r), ResultPass, "")
	requireProbe(t, probeDualForm(r), ResultFail, "manifest")
}

func TestProbeMutateContradiction(t *testing.T) {
	m := baseManifest()
	commandByPath(t, &m, "create").Mutates = false
	r := newManifestProbeRunner(t, m, Target{Mutate: []string{"create"}})
	requireProbe(t, probeDryRunAccepted(r), ResultFail, "manifest")
	requireProbe(t, probeDryRunNoSideEffects(r), ResultFail, "manifest")

	m = baseManifest()
	commandByPath(t, &m, "create").SupportsDryRun = false
	r = newManifestProbeRunner(t, m, Target{Mutate: []string{"create"}})
	requireProbe(t, probeDryRunAccepted(r), ResultFail, "manifest")
	requireProbe(t, probeDryRunNoSideEffects(r), ResultFail, "manifest")
}

func TestProbeDestructiveContradiction(t *testing.T) {
	m := baseManifest()
	commandByPath(t, &m, "delete").Destructive = false
	r := newManifestProbeRunner(t, m, Target{Destructive: []string{"delete"}})
	requireProbe(t, probeDestructiveDryRunNoConfirm(r), ResultFail, "manifest")
	requireProbe(t, probeDestructiveRequiresConfirm(r), ResultFail, "manifest")

	m = baseManifest()
	commandByPath(t, &m, "delete").RequiresConfirmation = false
	r = newManifestProbeRunner(t, m, Target{Destructive: []string{"delete"}})
	requireProbe(t, probeDestructiveDryRunNoConfirm(r), ResultFail, "manifest")
	requireProbe(t, probeDestructiveRequiresConfirm(r), ResultFail, "manifest")
}

func TestProbeAuthContradiction(t *testing.T) {
	m := baseManifest()
	commandByPath(t, &m, "whoami").RequiresAuth = false
	r := newManifestProbeRunner(t, m, Target{
		Auth:      []string{"whoami"},
		Secret:    []string{"whoami"},
		SecretEnv: "DEMO_TOKEN",
	})
	requireProbe(t, probeMissingCredential(r), ResultFail, "manifest")
	requireProbe(t, probeSecretNotPrinted(r), ResultFail, "manifest")
}

func TestProbeConfigFlagContradiction(t *testing.T) {
	m := baseManifest()
	m.GlobalFlags = []manifest.Flag{}
	r := newManifestProbeRunner(t, m, Target{Read: []string{"read"}})
	requireProbe(t, probeConfigFlag(r), ResultFail, "manifest")
}

func TestProbeUpdateContradiction(t *testing.T) {
	m := baseManifest()
	removeCommandByPath(&m, "update")
	r := newManifestProbeRunner(t, m, Target{HasUpdate: true})
	requireProbe(t, probeUpdateCheckReadonly(r), ResultFail, "manifest")
	requireProbe(t, probeUpdateCheckJSON(r), ResultFail, "manifest")

	m = baseManifest()
	commandByPath(t, &m, "update").OutputModes = []string{"table"}
	r = newManifestProbeRunner(t, m, Target{HasUpdate: true})
	requireProbe(t, probeUpdateCheckReadonly(r), ResultPass, "")
	requireProbe(t, probeUpdateCheckJSON(r), ResultFail, "manifest")
}

func TestProbeHelpContradiction(t *testing.T) {
	m := baseManifest()
	commandByPath(t, &m, "read").Examples = []string{}
	r := newManifestProbeRunner(t, m, Target{Read: []string{"read"}})
	requireProbe(t, probeHelpExamples(r), ResultFail, "manifest")
	requireProbe(t, probeHelpRelated(r), ResultPass, "")

	m = baseManifest()
	commandByPath(t, &m, "read").Related = []string{}
	r = newManifestProbeRunner(t, m, Target{Read: []string{"read"}})
	requireProbe(t, probeHelpRelated(r), ResultFail, "manifest")
	requireProbe(t, probeHelpExamples(r), ResultPass, "")
}

func TestProbeOmittedFixturesStayNAWithManifest(t *testing.T) {
	r := newManifestProbeRunner(t, baseManifest(), Target{Read: []string{"read"}})
	score := r.Score(defaultRulesetForTest(t))
	if got := ruleResult(score, "output.json-parseable"); got != ResultPass {
		t.Fatalf("output.json-parseable = %q, want %q", got, ResultPass)
	}
	for _, id := range []string{
		"dryrun.accepted",
		"dryrun.destructive-preview",
		"exit.api-error",
		"auth.secret-not-printed",
		"resolution.no-prompt",
	} {
		if got := ruleResult(score, id); got != ResultNotApplicable {
			t.Fatalf("%s = %q, want %q", id, got, ResultNotApplicable)
		}
	}
	if score.Manifest.Status != ManifestPresent {
		t.Fatalf("manifest status = %q, want %q", score.Manifest.Status, ManifestPresent)
	}
}

func TestProbeAutoFallbackBadManifestUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		status string
	}{
		{name: "invalid", body: "{ not json", status: ManifestInvalid},
		{name: "unsupported", body: unsupportedManifestJSON, status: ManifestUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := buildManifestScoreFixture(t, tc.body)
			r, err := NewRunner(Target{Path: bin, Read: []string{"read"}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })
			if err := r.DiscoverManifest(); err != nil {
				t.Fatalf("DiscoverManifest: %v", err)
			}
			score := r.Score(defaultRulesetForTest(t))
			if score.Manifest.Status != tc.status {
				t.Fatalf("manifest status = %q, want %q", score.Manifest.Status, tc.status)
			}
			if got := ruleResult(score, "output.json-parseable"); got != ResultPass {
				t.Fatalf("output.json-parseable = %q, want %q", got, ResultPass)
			}
			if score.Manifest.UsedRuleCount != 0 || len(score.Manifest.UsedRules) != 0 {
				t.Fatalf("manifest used rules = %d %v, want none", score.Manifest.UsedRuleCount, score.Manifest.UsedRules)
			}
		})
	}
}

func TestProbeUsedRulesAccounting(t *testing.T) {
	m := baseManifest()
	commandByPath(t, &m, "create").Mutates = false
	r := newManifestProbeRunner(t, m, Target{
		Read:      []string{"read"},
		Mutate:    []string{"create"},
		HasUpdate: true,
		Secret:    []string{"whoami"},
		SecretEnv: "DEMO_TOKEN",
	})
	score := r.Score(defaultRulesetForTest(t))
	if score.Manifest.UsedRuleCount == 0 {
		t.Fatal("used_rule_count = 0, want manifest-backed rules")
	}
	if score.Manifest.UsedRuleCount != len(score.Manifest.UsedRules) {
		t.Fatalf("used_rule_count = %d, len used_rules = %d", score.Manifest.UsedRuleCount, len(score.Manifest.UsedRules))
	}
	if !sort.StringsAreSorted(score.Manifest.UsedRules) {
		t.Fatalf("used_rules not sorted: %v", score.Manifest.UsedRules)
	}
	for _, id := range []string{"output.json-parseable", "dryrun.accepted"} {
		if !containsString(score.Manifest.UsedRules, id) {
			t.Fatalf("used_rules missing %s: %v", id, score.Manifest.UsedRules)
		}
	}
	if got := ruleResult(score, "dryrun.accepted"); got != ResultFail {
		t.Fatalf("dryrun.accepted = %q, want %q", got, ResultFail)
	}
	for _, id := range []string{"exit.unknown-usage", "resolution.no-prompt"} {
		if containsString(score.Manifest.UsedRules, id) {
			t.Fatalf("used_rules unexpectedly contains %s: %v", id, score.Manifest.UsedRules)
		}
	}

	bin := buildManifestScoreFixture(t, manifestJSON(t, baseManifest()))
	off, err := NewRunner(Target{Path: bin, Read: []string{"read"}, ManifestMode: ManifestModeOff})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = off.Close() })
	if err := off.DiscoverManifest(); err != nil {
		t.Fatalf("DiscoverManifest off: %v", err)
	}
	offScore := off.Score(defaultRulesetForTest(t))
	if offScore.Manifest.UsedRuleCount != 0 || len(offScore.Manifest.UsedRules) != 0 {
		t.Fatalf("manifest off used rules = %d %v, want none", offScore.Manifest.UsedRuleCount, offScore.Manifest.UsedRules)
	}
}

func TestReportManifestBackedLine(t *testing.T) {
	r := newManifestProbeRunner(t, baseManifest(), Target{Read: []string{"read"}})
	score := r.Score(defaultRulesetForTest(t))
	report := score.Report()
	lines := strings.Split(report, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "Manifest: present (") {
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "Manifest-backed: ") {
				t.Fatalf("line after present manifest = %q, want Manifest-backed line\n%s", lines[i+1], report)
			}
			if !strings.Contains(lines[i+1], fmt.Sprintf("%d rules used manifest data", score.Manifest.UsedRuleCount)) {
				t.Fatalf("manifest-backed line = %q, count = %d", lines[i+1], score.Manifest.UsedRuleCount)
			}
			goto disabled
		}
	}
	t.Fatalf("present manifest line missing:\n%s", report)

disabled:
	bin := buildManifestScoreFixture(t, manifestJSON(t, baseManifest()))
	off, err := NewRunner(Target{Path: bin, Read: []string{"read"}, ManifestMode: ManifestModeOff})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = off.Close() })
	if err := off.DiscoverManifest(); err != nil {
		t.Fatalf("DiscoverManifest off: %v", err)
	}
	if report := off.Score(defaultRulesetForTest(t)).Report(); strings.Contains(report, "Manifest-backed:") {
		t.Fatalf("disabled report contains Manifest-backed line:\n%s", report)
	}

	missingBin := buildManifestEndpointFixture(t, "", 1)
	missing, err := NewRunner(Target{Path: missingBin})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = missing.Close() })
	if err := missing.DiscoverManifest(); err != nil {
		t.Fatalf("DiscoverManifest missing: %v", err)
	}
	if report := missing.Score(defaultRulesetForTest(t)).Report(); strings.Contains(report, "Manifest-backed:") {
		t.Fatalf("missing report contains Manifest-backed line:\n%s", report)
	}
}

func baseManifest() manifest.Manifest {
	return manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		SpecVersion:   spec.Version,
		ToolName:      "demo",
		ToolVersion:   "v1.0.0",
		GlobalFlags:   []manifest.Flag{{Name: "config"}},
		Commands: []manifest.Command{
			manifestTestCommand([]string{}, nil),
			manifestTestCommand([]string{"read"}, func(c *manifest.Command) {
				c.OutputModes = []string{"table", "json"}
				c.Examples = []string{"demo read", "demo read --json"}
				c.Related = []string{"demo other"}
			}),
			manifestTestCommand([]string{"create"}, func(c *manifest.Command) {
				c.Mutates = true
				c.SupportsDryRun = true
			}),
			manifestTestCommand([]string{"delete"}, func(c *manifest.Command) {
				c.Mutates = true
				c.SupportsDryRun = true
				c.Destructive = true
				c.RequiresConfirmation = true
			}),
			manifestTestCommand([]string{"whoami"}, func(c *manifest.Command) {
				c.OutputModes = []string{"table", "json"}
				c.RequiresAuth = true
			}),
			manifestTestCommand([]string{"update"}, func(c *manifest.Command) {
				c.OutputModes = []string{"table", "json"}
			}),
			manifestTestCommand([]string{"get"}, nil),
			manifestTestCommand([]string{"item"}, nil),
			manifestTestCommand([]string{"item", "list"}, nil),
			manifestTestCommand([]string{"item", "get"}, nil),
			manifestTestCommand([]string{"item", "create"}, nil),
		},
	}
}

func manifestTestCommand(path []string, configure func(*manifest.Command)) manifest.Command {
	c := manifest.Command{
		Path:        append([]string{}, path...),
		Use:         strings.Join(path, " "),
		Examples:    []string{},
		Related:     []string{},
		OutputModes: []string{},
		LocalFlags:  []manifest.Flag{},
	}
	if len(path) == 0 {
		c.Use = "demo"
	}
	if configure != nil {
		configure(&c)
	}
	return c
}

func manifestJSON(t *testing.T, m manifest.Manifest) string {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// buildManifestScoreFixture creates a tiny target that is conformant for the
// commands reached after successful manifest pre-checks. Most contradiction tests
// never execute the offending command because the manifest failure short-circuits.
func buildManifestScoreFixture(t *testing.T, payload string) string {
	t.Helper()
	src := fmt.Sprintf(`package main

import "os"

func has(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "__rungrad_manifest" {
		os.Stdout.WriteString(%q)
		return
	}
	if len(args) == 0 {
		os.Exit(1)
	}
	switch args[0] {
	case "read":
		read(args[1:])
	case "create":
		create(args[1:])
	case "delete":
		deleteCommand(args[1:])
	case "whoami":
		whoami(args[1:])
	case "update":
		update(args[1:])
	case "get":
		get(args[1:])
	default:
		os.Exit(1)
	}
}

func read(args []string) {
	if has(args, "--help") {
		os.Stdout.WriteString("Usage: demo read\n\nExamples:\n  demo read\n  demo read --json\n\nRelated commands:\n  demo other\n")
		return
	}
	if has(args, "--json") {
		os.Stdout.WriteString("[\"alpha\",\"beta\"]\n")
		return
	}
	os.Stdout.WriteString("alpha\nbeta\n")
}

func create(args []string) {
	if has(args, "--dry-run") {
		os.Stdout.WriteString("DRY RUN: would create demo\n")
		return
	}
	os.Exit(0)
}

func deleteCommand(args []string) {
	if has(args, "--dry-run") {
		os.Stdout.WriteString("DRY RUN: would delete demo\n")
		return
	}
	if has(args, "--json") {
		os.Stderr.WriteString("{\"exit_code\":1}\n")
		os.Exit(1)
	}
	os.Exit(1)
}

func whoami(args []string) {
	if os.Getenv("DEMO_TOKEN") == "" && os.Getenv("RGREF_TOKEN") == "" {
		os.Exit(3)
	}
	if has(args, "--json") {
		os.Stdout.WriteString("{\"user\":\"demo\"}\n")
		return
	}
	os.Stdout.WriteString("demo\n")
}

func update(args []string) {
	if !has(args, "--check") {
		os.Exit(1)
	}
	if has(args, "--json") {
		os.Stdout.WriteString("{\"status\":\"ok\"}\n")
		return
	}
	os.Stdout.WriteString("already current\n")
}

func get(args []string) {
	if has(args, "--no-prompt") {
		os.Stderr.WriteString("candidates: alpha, beta\n")
		os.Exit(1)
	}
	if len(args) == 0 {
		os.Exit(1)
	}
	switch args[0] {
	case "ghost":
		os.Exit(5)
	case "broken":
		os.Exit(2)
	case "forbidden":
		os.Exit(4)
	case "throttled":
		os.Exit(6)
	default:
		os.Stdout.WriteString("{}\n")
	}
}
`, payload)
	return buildConformanceFixture(t, src)
}

func newManifestProbeRunner(t *testing.T, m manifest.Manifest, target Target) *Runner {
	t.Helper()
	target.Path = buildManifestScoreFixture(t, manifestJSON(t, m))
	r, err := NewRunner(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if err := r.DiscoverManifest(); err != nil {
		t.Fatalf("DiscoverManifest: %v", err)
	}
	return r
}

func commandByPath(t *testing.T, m *manifest.Manifest, path ...string) *manifest.Command {
	t.Helper()
	for i := range m.Commands {
		if equalPath(m.Commands[i].Path, path) {
			return &m.Commands[i]
		}
	}
	t.Fatalf("manifest command %v not found", path)
	return nil
}

func removeCommandByPath(m *manifest.Manifest, path ...string) {
	filtered := m.Commands[:0]
	for _, c := range m.Commands {
		if equalPath(c.Path, path) {
			continue
		}
		filtered = append(filtered, c)
	}
	m.Commands = filtered
}

func requireProbe(t *testing.T, got ProbeResult, wantResult, reasonContains string) {
	t.Helper()
	if got.Result != wantResult {
		t.Fatalf("probe result = %q, want %q (reason: %s)", got.Result, wantResult, got.Reason)
	}
	if reasonContains != "" && !strings.Contains(got.Reason, reasonContains) {
		t.Fatalf("probe reason = %q, want substring %q", got.Reason, reasonContains)
	}
}

func defaultRulesetForTest(t *testing.T) Ruleset {
	t.Helper()
	rs, err := DefaultRuleset()
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
