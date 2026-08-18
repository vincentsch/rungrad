package conformance

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/spec"
)

const validManifestJSON = `{
  "schema_version": "rungrad-manifest/1",
  "spec_version": "rungrad-spec/1",
  "tool_name": "demo",
  "tool_version": "v1.2.3",
  "global_flags": [],
  "commands": [
    {"path": [], "use": "demo", "short": "", "examples": [], "related": [], "output_modes": [], "requires_auth": false, "mutates": false, "supports_dry_run": false, "destructive": false, "requires_confirmation": false, "local_flags": []},
    {"path": ["item","list"], "use": "list", "short": "", "examples": [], "related": [], "output_modes": [], "requires_auth": false, "mutates": false, "supports_dry_run": false, "destructive": false, "requires_confirmation": false, "local_flags": []}
  ]
}`

const unsupportedManifestJSON = `{"schema_version":"rungrad-manifest/2","tool_name":"demo","global_flags":[],"commands":[{"path":[],"use":"demo","examples":[],"related":[],"output_modes":[],"local_flags":[]}]}`

const validExtensionFreeManifestJSON = `{
  "schema_version": "rungrad-manifest/1",
  "spec_version": "rungrad-spec/1",
  "tool_name": "demo",
  "tool_version": "v1.2.3",
  "global_flags": [{"name":"config","shorthand":"","usage":"config file","default":"","type":"string","required":false}],
  "commands": [
    {"path": [], "use": "demo", "short": "", "examples": [], "related": [], "output_modes": [], "requires_auth": false, "mutates": false, "supports_dry_run": false, "destructive": false, "requires_confirmation": false, "local_flags": []},
    {"path": ["read"], "use": "read", "short": "Read data", "examples": ["demo read","demo read --json"], "related": ["demo other"], "output_modes": ["table","json"], "requires_auth": false, "mutates": false, "supports_dry_run": false, "destructive": false, "requires_confirmation": false, "local_flags": []}
  ]
}`

const validExtensionManifestJSON = `{
  "schema_version": "rungrad-manifest/1",
  "spec_version": "rungrad-spec/1",
  "tool_name": "demo",
  "tool_version": "v1.2.3",
  "global_flags": [{"name":"config","shorthand":"","usage":"config file","default":"","type":"string","required":false}],
  "commands": [
    {"path": [], "use": "demo", "short": "", "examples": [], "related": [], "output_modes": [], "requires_auth": false, "mutates": false, "supports_dry_run": false, "destructive": false, "requires_confirmation": false, "local_flags": []},
    {"path": ["read"], "use": "read", "short": "Read data", "examples": ["demo read","demo read --json"], "related": ["demo other"], "output_modes": ["table","json"], "requires_auth": false, "mutates": false, "supports_dry_run": false, "destructive": false, "requires_confirmation": false, "local_flags": [], "extensions": {"example.com/product": {"owner": "platform", "status": "beta", "docs_path": "docs/read.md"}}}
  ]
}`

const invalidExtensionManifestJSON = `{
  "schema_version": "rungrad-manifest/1",
  "spec_version": "rungrad-spec/1",
  "tool_name": "demo",
  "tool_version": "v1.2.3",
  "global_flags": [],
  "commands": [
    {"path": [], "use": "demo", "examples": [], "related": [], "output_modes": [], "local_flags": []},
    {"path": ["read"], "use": "read", "examples": [], "related": [], "output_modes": [], "local_flags": [], "extensions": {"Example.com/product": {"owner": "platform"}}}
  ]
}`

const invalidCoreExtensionManifestJSON = `{
  "schema_version": "rungrad-manifest/1",
  "spec_version": "rungrad-spec/1",
  "tool_name": "demo",
  "tool_version": "v1.2.3",
  "global_flags": [],
  "commands": [
    {"path": [], "use": "demo", "examples": [], "related": [], "output_modes": [], "local_flags": []},
    {"path": ["read"], "use": "read", "examples": [], "related": [], "output_modes": [], "local_flags": [], "extensions": {"example.com/product": {"supports_dry_run": true}}}
  ]
}`

const invalidDuplicateExtensionManifestJSON = `{
  "schema_version": "rungrad-manifest/1",
  "spec_version": "rungrad-spec/1",
  "tool_name": "demo",
  "tool_version": "v1.2.3",
  "global_flags": [],
  "commands": [
    {"path": [], "use": "demo", "examples": [], "related": [], "output_modes": [], "local_flags": []},
    {"path": ["read"], "use": "read", "examples": [], "related": [], "output_modes": [], "local_flags": [], "extensions": {"example.com/product": {"owner": "a", "owner": "b"}}}
  ]
}`

const scoreableNoManifestSrc = `package main

import "os"

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "__rungrad_manifest" {
		os.Exit(1)
	}
	if len(args) > 0 && args[0] == "read" {
		for _, a := range args[1:] {
			if a == "--json" {
				os.Stdout.WriteString("[\"alpha\",\"beta\"]\n")
				os.Exit(0)
			}
		}
		os.Stdout.WriteString("alpha\nbeta\n")
		os.Exit(0)
	}
	os.Exit(1)
}
`

// TestDefaultRulesetLoads checks the embedded ruleset parses and is versioned.
func TestDefaultRulesetLoads(t *testing.T) {
	rs, err := DefaultRuleset()
	if err != nil {
		t.Fatalf("DefaultRuleset: %v", err)
	}
	if rs.Version != spec.Version {
		t.Fatalf("ruleset version %q != spec.Version %q", rs.Version, spec.Version)
	}
	if len(rs.Rules) == 0 {
		t.Fatalf("ruleset has no rules")
	}
}

// TestEveryRuleHasProbe is the spec/ruleset consistency check: every rule names
// a probe that is implemented, and every rule has a known severity.
func TestEveryRuleHasProbe(t *testing.T) {
	rs, err := DefaultRuleset()
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range rs.Rules {
		if probes[rule.Probe] == nil {
			t.Errorf("rule %s references unimplemented probe %q", rule.ID, rule.Probe)
		}
		if rule.Severity != "required" && rule.Severity != "recommended" {
			t.Errorf("rule %s has invalid severity %q", rule.ID, rule.Severity)
		}
	}
}

// TestSpecSectionsAndRulesAgree checks every spec section has at least one rule
// and every rule belongs to a known spec section.
func TestSpecSectionsAndRulesAgree(t *testing.T) {
	rs, err := DefaultRuleset()
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, s := range spec.Sections {
		known[s] = false
	}
	for _, rule := range rs.Rules {
		seen, ok := known[rule.Section]
		if !ok {
			t.Errorf("rule %s has unknown section %q", rule.ID, rule.Section)
			continue
		}
		_ = seen
		known[rule.Section] = true
	}
	for section, hasRule := range known {
		if !hasRule {
			t.Errorf("spec section %q has no rule", section)
		}
	}
}

func TestSpecProseAndRulesetIDsAgree(t *testing.T) {
	rs, err := DefaultRuleset()
	if err != nil {
		t.Fatal(err)
	}
	ruleIDs := map[string]bool{}
	for _, rule := range rs.Rules {
		ruleIDs[rule.ID] = true
	}

	proseIDs := map[string]bool{}
	prefixes := ruleIDPrefixes(ruleIDs)
	for _, section := range spec.Sections {
		data, err := os.ReadFile(filepath.Join("..", "spec", section+".md"))
		if err != nil {
			t.Fatalf("read spec section %s: %v", section, err)
		}
		for id := range proseRuleIDs(string(data), prefixes) {
			proseIDs[id] = true
		}
	}

	if got, want := sortedSet(proseIDs), sortedSet(ruleIDs); !equalStrings(got, want) {
		t.Fatalf("prose/ruleset ids differ\nprose: %v\nrules: %v", got, want)
	}
}

func TestProseRuleIDsIgnoresDottedNonRuleTokens(t *testing.T) {
	ids := proseRuleIDs("See `ruleset.yaml`, `output.json-parseable`, and `foo.bar`.", map[string]bool{"output": true})
	if !ids["output.json-parseable"] {
		t.Fatalf("expected real rule id to be collected: %v", ids)
	}
	if ids["ruleset.yaml"] || ids["foo.bar"] {
		t.Fatalf("collected non-rule dotted token: %v", ids)
	}
}

func ruleIDPrefixes(ruleIDs map[string]bool) map[string]bool {
	out := map[string]bool{}
	for id := range ruleIDs {
		if i := strings.IndexByte(id, '.'); i > 0 {
			out[id[:i]] = true
		}
	}
	return out
}

var proseIDPattern = regexp.MustCompile("`([a-z]+\\.[a-z0-9.-]+)`")

func proseRuleIDs(text string, prefixes map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, match := range proseIDPattern.FindAllStringSubmatch(text, -1) {
		id := match[1]
		prefix, _, ok := strings.Cut(id, ".")
		if ok && prefixes[prefix] {
			out[id] = true
		}
	}
	return out
}

func TestNewRunnerRejectsMissingTarget(t *testing.T) {
	_, err := NewRunner(Target{Path: filepath.Join(t.TempDir(), "missing")})
	var targetErr *TargetError
	if err == nil {
		t.Fatalf("expected missing target error")
	}
	if _, ok := err.(*TargetError); !ok {
		t.Fatalf("expected TargetError, got %T %v", err, err)
	}
	targetErr = err.(*TargetError)
	if targetErr.ExitCode() != 1 {
		t.Fatalf("target error exit code = %d, want 1", targetErr.ExitCode())
	}
}

func TestRepeatableProbeFailsWhenCommandCannotRun(t *testing.T) {
	r := &Runner{
		target:  Target{Path: filepath.Join(t.TempDir(), "missing"), Read: []string{"list"}},
		timeout: time.Second,
	}
	result := probeRepeatable(r)
	if result.Result != ResultFail {
		t.Fatalf("repeatable probe = %s, want fail (%s)", result.Result, result.Reason)
	}
}

func TestDestructiveRequiresConfirmFailsWhenNonInteractiveReadsStdin(t *testing.T) {
	// The first positional arg selects which refusal path is intentionally broken.
	// Each subtest lets the earlier probe paths pass, then verifies the targeted
	// path fails because it reads from blocking stdin.
	bin := buildConformanceFixture(t, `package main

import (
	"fmt"
	"os"
)

func readThenExit() {
	var input string
	fmt.Fscanln(os.Stdin, &input)
	os.Exit(1)
}

func main() {
	mode := ""
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	for _, arg := range os.Args[2:] {
		switch arg {
		case "--no-prompt":
			if mode == "no-prompt" {
				readThenExit()
			}
			os.Exit(1)
		case "--json":
			if mode == "json" {
				readThenExit()
			}
			fmt.Fprintln(os.Stderr, "{\"exit_code\":1}")
			os.Exit(1)
		}
	}
	if mode == "bare" {
		readThenExit()
	}
	os.Exit(1)
}
`)
	cases := map[string]string{
		"bare":      "read stdin with no terminal",
		"no-prompt": "read stdin under --no-prompt",
		"json":      "read stdin under --json",
	}
	for mode, reason := range cases {
		t.Run(mode, func(t *testing.T) {
			runner, err := NewRunner(Target{Path: bin, Destructive: []string{mode}})
			if err != nil {
				t.Fatal(err)
			}
			defer runner.Close()
			runner.timeout = 100 * time.Millisecond

			result := probeDestructiveRequiresConfirm(runner)
			if result.Result != ResultFail {
				t.Fatalf("probe result = %s, want fail (%s)", result.Result, result.Reason)
			}
			if !strings.Contains(result.Reason, reason) {
				t.Fatalf("probe failed for the wrong reason: %s", result.Reason)
			}
		})
	}
}

func TestDiscoverManifestMissing(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		exit   int
	}{
		{name: "empty exit 1", stdout: "", exit: 1},
		{name: "unknown command", stdout: "Error: unknown command\n", exit: 1},
		{name: "empty exit 127", stdout: "", exit: 127},
		{name: "arbitrary exit 2", stdout: "command failed\n", exit: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bin := buildManifestEndpointFixture(t, tc.stdout, tc.exit)
			runner, err := NewRunner(Target{Path: bin})
			if err != nil {
				t.Fatal(err)
			}
			defer runner.Close()

			if err := runner.DiscoverManifest(); err != nil {
				t.Fatalf("DiscoverManifest: %v", err)
			}
			if runner.manifestSummary.Status != ManifestMissing {
				t.Fatalf("status = %q, want %q", runner.manifestSummary.Status, ManifestMissing)
			}
			if !strings.Contains(runner.manifestSummary.Reason, fmt.Sprint(tc.exit)) {
				t.Fatalf("reason %q does not name exit %d", runner.manifestSummary.Reason, tc.exit)
			}
		})
	}
}

func TestDiscoverManifestPresent(t *testing.T) {
	bin := buildManifestEndpointFixture(t, validManifestJSON, 0)
	runner, err := NewRunner(Target{Path: bin})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	if err := runner.DiscoverManifest(); err != nil {
		t.Fatalf("DiscoverManifest: %v", err)
	}
	summary := runner.manifestSummary
	if summary.Status != ManifestPresent {
		t.Fatalf("status = %q, want %q", summary.Status, ManifestPresent)
	}
	if summary.SchemaVersion != manifest.SchemaVersion {
		t.Fatalf("schema_version = %q, want %q", summary.SchemaVersion, manifest.SchemaVersion)
	}
	if summary.ToolName != "demo" || summary.ToolVersion != "v1.2.3" {
		t.Fatalf("tool fields = %q %q, want demo v1.2.3", summary.ToolName, summary.ToolVersion)
	}
	if summary.CommandCount != 2 {
		t.Fatalf("command_count = %d, want 2", summary.CommandCount)
	}
	if summary.Reason != "" {
		t.Fatalf("reason = %q, want empty", summary.Reason)
	}
	if runner.cachedManifest == nil {
		t.Fatal("cachedManifest is nil")
	}
}

func TestDiscoverManifestInvalidJSON(t *testing.T) {
	bin := buildManifestEndpointFixture(t, "{ not json", 0)
	runner, err := NewRunner(Target{Path: bin})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	if err := runner.DiscoverManifest(); err != nil {
		t.Fatalf("DiscoverManifest: %v", err)
	}
	if runner.manifestSummary.Status != ManifestInvalid {
		t.Fatalf("status = %q, want %q", runner.manifestSummary.Status, ManifestInvalid)
	}
	if runner.manifestSummary.Reason == "" {
		t.Fatal("invalid manifest reason is empty")
	}
	if runner.cachedManifest != nil {
		t.Fatal("invalid manifest was cached")
	}
}

func TestDiscoverManifestInvalidExtensions(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "invalid namespace", body: invalidExtensionManifestJSON, want: "invalid namespace"},
		{name: "core field contradiction", body: invalidCoreExtensionManifestJSON, want: "core field contradiction"},
		{name: "duplicate key", body: invalidDuplicateExtensionManifestJSON, want: "duplicate key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := buildManifestEndpointFixture(t, tt.body, 0)
			runner, err := NewRunner(Target{Path: bin})
			if err != nil {
				t.Fatal(err)
			}
			defer runner.Close()

			if err := runner.DiscoverManifest(); err != nil {
				t.Fatalf("DiscoverManifest: %v", err)
			}
			if runner.manifestSummary.Status != ManifestInvalid {
				t.Fatalf("status = %q, want %q", runner.manifestSummary.Status, ManifestInvalid)
			}
			if !strings.Contains(runner.manifestSummary.Reason, tt.want) || !strings.Contains(runner.manifestSummary.Reason, "extension") {
				t.Fatalf("reason = %q, want substrings %q and extension", runner.manifestSummary.Reason, tt.want)
			}
		})
	}
}

func TestDiscoverManifestUnsupportedVersion(t *testing.T) {
	bin := buildManifestEndpointFixture(t, unsupportedManifestJSON, 0)
	runner, err := NewRunner(Target{Path: bin})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	if err := runner.DiscoverManifest(); err != nil {
		t.Fatalf("DiscoverManifest: %v", err)
	}
	summary := runner.manifestSummary
	if summary.Status != ManifestUnsupported {
		t.Fatalf("status = %q, want %q", summary.Status, ManifestUnsupported)
	}
	if !strings.Contains(summary.Reason, "rungrad-manifest/2") {
		t.Fatalf("reason %q does not name unsupported version", summary.Reason)
	}
	if summary.SchemaVersion != "rungrad-manifest/2" {
		t.Fatalf("schema_version = %q, want rungrad-manifest/2", summary.SchemaVersion)
	}
	if runner.cachedManifest != nil {
		t.Fatal("unsupported manifest was cached")
	}
}

func TestDiscoverManifestNonZeroManifestShaped(t *testing.T) {
	bin := buildManifestEndpointFixture(t, `{"schema_version":"rungrad-manifest/1","tool_name":"demo","commands":[]}`, 2)
	runner, err := NewRunner(Target{Path: bin})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	if err := runner.DiscoverManifest(); err != nil {
		t.Fatalf("DiscoverManifest: %v", err)
	}
	summary := runner.manifestSummary
	if summary.Status != ManifestInvalid {
		t.Fatalf("status = %q, want %q", summary.Status, ManifestInvalid)
	}
	if !strings.Contains(summary.Reason, "2") {
		t.Fatalf("reason %q does not name exit 2", summary.Reason)
	}
	if summary.ToolName != "demo" {
		t.Fatalf("tool_name = %q, want demo", summary.ToolName)
	}
	if runner.cachedManifest != nil {
		t.Fatal("non-zero manifest-shaped output was cached")
	}
}

func TestDiscoverManifestTimeout(t *testing.T) {
	bin := buildBlockingManifestFixture(t)
	runner, err := NewRunner(Target{Path: bin})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	runner.timeout = 100 * time.Millisecond

	if err := runner.DiscoverManifest(); err != nil {
		t.Fatalf("DiscoverManifest: %v", err)
	}
	if runner.manifestSummary.Status != ManifestInvalid {
		t.Fatalf("status = %q, want %q", runner.manifestSummary.Status, ManifestInvalid)
	}
}

func TestDiscoverManifestOffDoesNotInvoke(t *testing.T) {
	bin := buildMarkerManifestFixture(t, validManifestJSON, 0)
	runner, err := NewRunner(Target{Path: bin, ManifestMode: ManifestModeOff})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	if err := runner.DiscoverManifest(); err != nil {
		t.Fatalf("DiscoverManifest: %v", err)
	}
	if runner.manifestSummary.Status != ManifestDisabled {
		t.Fatalf("status = %q, want %q", runner.manifestSummary.Status, ManifestDisabled)
	}
	if runner.manifestSummary.Reason != reasonDiscoveryDisabled {
		t.Fatalf("reason = %q, want %q", runner.manifestSummary.Reason, reasonDiscoveryDisabled)
	}
	if _, err := os.Stat(filepath.Join(runner.confHome, "marker")); !os.IsNotExist(err) {
		t.Fatalf("marker stat error = %v, want not exist", err)
	}
}

func TestDiscoverManifestRequiredFailsWhenAbsent(t *testing.T) {
	bin := buildManifestEndpointFixture(t, "", 1)
	runner, err := NewRunner(Target{Path: bin, ManifestMode: ManifestModeRequired})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	err = runner.DiscoverManifest()
	if err == nil {
		t.Fatal("DiscoverManifest succeeded, want required-mode failure")
	}
	var targetErr *TargetError
	if !errors.As(err, &targetErr) {
		t.Fatalf("error = %T %v, want TargetError", err, err)
	}
	if targetErr.ExitCode() != 1 {
		t.Fatalf("ExitCode = %d, want 1", targetErr.ExitCode())
	}
}

func TestDiscoverManifestUnknownMode(t *testing.T) {
	bin := buildManifestEndpointFixture(t, "", 1)
	runner, err := NewRunner(Target{Path: bin, ManifestMode: "bogus"})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	err = runner.DiscoverManifest()
	var targetErr *TargetError
	if err == nil || !errors.As(err, &targetErr) {
		t.Fatalf("error = %T %v, want TargetError", err, err)
	}
}

func TestScoreManifestDisabledWithoutDiscovery(t *testing.T) {
	bin := buildManifestEndpointFixture(t, validManifestJSON, 0)
	runner, err := NewRunner(Target{Path: bin})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	rs, err := DefaultRuleset()
	if err != nil {
		t.Fatal(err)
	}

	score := runner.Score(rs)
	if score.Manifest.Status != ManifestDisabled {
		t.Fatalf("manifest status = %q, want %q", score.Manifest.Status, ManifestDisabled)
	}
	if score.Manifest.Reason != reasonDiscoveryNotRun {
		t.Fatalf("manifest reason = %q, want %q", score.Manifest.Reason, reasonDiscoveryNotRun)
	}
	if score.Manifest.UsedRuleCount != 0 || len(score.Manifest.UsedRules) != 0 {
		t.Fatalf("manifest used rules = %d %v, want none", score.Manifest.UsedRuleCount, score.Manifest.UsedRules)
	}
}

func TestScoreAutoFallbackMissingKeepsBlackBox(t *testing.T) {
	bin := buildConformanceFixture(t, scoreableNoManifestSrc)
	runner, err := NewRunner(Target{Path: bin, Read: []string{"read"}})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	if err := runner.DiscoverManifest(); err != nil {
		t.Fatalf("DiscoverManifest: %v", err)
	}
	rs, err := DefaultRuleset()
	if err != nil {
		t.Fatal(err)
	}
	score := runner.Score(rs)
	if score.Manifest.Status != ManifestMissing {
		t.Fatalf("manifest status = %q, want %q", score.Manifest.Status, ManifestMissing)
	}
	if got := ruleResult(score, "output.json-parseable"); got != ResultPass {
		t.Fatalf("output.json-parseable result = %q, want %q", got, ResultPass)
	}
	if score.Manifest.UsedRuleCount != 0 || len(score.Manifest.UsedRules) != 0 {
		t.Fatalf("manifest used rules = %d %v, want none", score.Manifest.UsedRuleCount, score.Manifest.UsedRules)
	}
}

func TestScoreManifestValidExtensionsStayPresent(t *testing.T) {
	extensionScore := scoreManifestPayload(t, validExtensionManifestJSON)
	plainScore := scoreManifestPayload(t, validExtensionFreeManifestJSON)
	if extensionScore.Manifest.Status != ManifestPresent {
		t.Fatalf("manifest status = %q, want %q", extensionScore.Manifest.Status, ManifestPresent)
	}
	if extensionScore.Manifest.UsedRuleCount == 0 {
		t.Fatal("valid extension manifest used no manifest-backed rules")
	}
	for _, id := range []string{"output.json-parseable", "output.dual-form"} {
		if got := ruleResult(extensionScore, id); got != ResultPass {
			t.Fatalf("%s = %q, want %q", id, got, ResultPass)
		}
	}
	plainByID := map[string]string{}
	for _, rule := range plainScore.Rules {
		plainByID[rule.ID] = rule.Result
	}
	for _, rule := range extensionScore.Rules {
		if plainByID[rule.ID] != rule.Result {
			t.Fatalf("rule %s result with extensions = %q, without = %q", rule.ID, rule.Result, plainByID[rule.ID])
		}
	}
}

func TestScoreAutoFallbackMissingEndpointStaysBlackBoxWithInvisibleExtensions(t *testing.T) {
	bin := buildScorableNoManifestFixture(t)
	runner, err := NewRunner(Target{Path: bin, Read: []string{"read"}})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	if err := runner.DiscoverManifest(); err != nil {
		t.Fatalf("DiscoverManifest: %v", err)
	}
	score := runner.Score(defaultRulesetForTest(t))
	if score.Manifest.Status != ManifestMissing {
		t.Fatalf("manifest status = %q, want %q", score.Manifest.Status, ManifestMissing)
	}
	if got := ruleResult(score, "output.json-parseable"); got != ResultPass {
		t.Fatalf("output.json-parseable result = %q, want %q", got, ResultPass)
	}
	if score.Manifest.UsedRuleCount != 0 || len(score.Manifest.UsedRules) != 0 {
		t.Fatalf("manifest used rules = %d %v, want none", score.Manifest.UsedRuleCount, score.Manifest.UsedRules)
	}
}

func TestDiscoverManifestIsCachedAndIdempotent(t *testing.T) {
	bin := buildMarkerManifestFixture(t, validManifestJSON, 0)
	runner, err := NewRunner(Target{Path: bin})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	if err := runner.DiscoverManifest(); err != nil {
		t.Fatalf("first DiscoverManifest: %v", err)
	}
	if err := runner.DiscoverManifest(); err != nil {
		t.Fatalf("second DiscoverManifest: %v", err)
	}
	rs, err := DefaultRuleset()
	if err != nil {
		t.Fatal(err)
	}
	score := runner.Score(rs)
	data, err := os.ReadFile(filepath.Join(runner.confHome, "marker"))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("marker length = %d, want 1", len(data))
	}
	if score.Manifest.Status != ManifestPresent {
		t.Fatalf("manifest status = %q, want %q", score.Manifest.Status, ManifestPresent)
	}
}

func TestReportManifestLine(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		bin := buildManifestEndpointFixture(t, validManifestJSON, 0)
		runner, err := NewRunner(Target{Path: bin})
		if err != nil {
			t.Fatal(err)
		}
		defer runner.Close()
		if err := runner.DiscoverManifest(); err != nil {
			t.Fatalf("DiscoverManifest: %v", err)
		}
		rs, err := DefaultRuleset()
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(runner.Score(rs).Report(), "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, "Overall:") {
				if i+1 >= len(lines) {
					t.Fatal("Overall line has no following manifest line")
				}
				if !strings.HasPrefix(lines[i+1], "Manifest: present (") {
					t.Fatalf("line after Overall = %q, want present manifest line", lines[i+1])
				}
				return
			}
		}
		t.Fatalf("Overall line not found in report:\n%s", runner.Score(rs).Report())
	})

	t.Run("disabled", func(t *testing.T) {
		bin := buildManifestEndpointFixture(t, validManifestJSON, 0)
		runner, err := NewRunner(Target{Path: bin})
		if err != nil {
			t.Fatal(err)
		}
		defer runner.Close()
		rs, err := DefaultRuleset()
		if err != nil {
			t.Fatal(err)
		}
		report := runner.Score(rs).Report()
		if !strings.Contains(report, "Manifest: disabled (") {
			t.Fatalf("disabled manifest line missing:\n%s", report)
		}
	})

	t.Run("missing", func(t *testing.T) {
		bin := buildManifestEndpointFixture(t, "", 1)
		runner, err := NewRunner(Target{Path: bin})
		if err != nil {
			t.Fatal(err)
		}
		defer runner.Close()
		if err := runner.DiscoverManifest(); err != nil {
			t.Fatalf("DiscoverManifest: %v", err)
		}
		rs, err := DefaultRuleset()
		if err != nil {
			t.Fatal(err)
		}
		report := runner.Score(rs).Report()
		if !strings.Contains(report, "Manifest: missing (") {
			t.Fatalf("missing manifest line absent:\n%s", report)
		}
	})
}

// buildManifestEndpointFixture compiles a CLI whose __rungrad_manifest endpoint
// writes stdout and exits with code; every other argv exits 1.
func buildManifestEndpointFixture(t *testing.T, stdout string, exit int) string {
	t.Helper()
	src := fmt.Sprintf(`package main

import "os"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__rungrad_manifest" {
		os.Stdout.WriteString(%q)
		os.Exit(%d)
	}
	os.Exit(1)
}
`, stdout, exit)
	return buildConformanceFixture(t, src)
}

// buildBlockingManifestFixture simulates a manifest endpoint that hangs until
// the runner timeout kills it.
func buildBlockingManifestFixture(t *testing.T) string {
	t.Helper()
	return buildConformanceFixture(t, `package main

import (
	"os"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__rungrad_manifest" {
		time.Sleep(time.Hour)
	}
	os.Exit(1)
}
`)
}

// buildMarkerManifestFixture records each manifest endpoint invocation under the
// runner's isolated HOME so tests can prove discovery does not run twice.
func buildMarkerManifestFixture(t *testing.T, stdout string, exit int) string {
	t.Helper()
	src := fmt.Sprintf(`package main

import "os"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__rungrad_manifest" {
		f, _ := os.OpenFile(os.Getenv("HOME")+"/marker", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		f.WriteString("x")
		f.Close()
		os.Stdout.WriteString(%q)
		os.Exit(%d)
	}
	os.Exit(0)
}
`, stdout, exit)
	return buildConformanceFixture(t, src)
}

func buildScorableNoManifestFixture(t *testing.T) string {
	t.Helper()
	return buildConformanceFixture(t, scoreableNoManifestSrc)
}

func scoreManifestPayload(t *testing.T, payload string) Score {
	t.Helper()
	bin := buildManifestScoreFixture(t, payload)
	runner, err := NewRunner(Target{Path: bin, Read: []string{"read"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if err := runner.DiscoverManifest(); err != nil {
		t.Fatalf("DiscoverManifest: %v", err)
	}
	return runner.Score(defaultRulesetForTest(t))
}

// buildConformanceFixture compiles a small throwaway CLI so probe tests exercise
// the same subprocess path used by real scoring.
func buildConformanceFixture(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fixture")
	cmd := exec.Command("go", "build", "-o", bin, path)
	cmd.Env = append(os.Environ(), "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, out)
	}
	return bin
}

// ruleResult returns an empty string when the rule is absent, which makes a
// missing-rule test failure read like any other wrong result.
func ruleResult(score Score, id string) string {
	for _, rule := range score.Rules {
		if rule.ID == id {
			return rule.Result
		}
	}
	return ""
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
