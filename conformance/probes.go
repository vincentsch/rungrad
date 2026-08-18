package conformance

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// secretSentinel is a recognizable value injected as a credential for the
// secret-not-printed probe. A conformant tool never echoes it.
const secretSentinel = "SENTINEL-SECRET-do-not-print-7Q2"

// Probe evaluates one assertion against a target.
type Probe func(r *Runner) ProbeResult

// probes maps each probe name in the ruleset to its implementation.
var probes = map[string]Probe{
	"json-parseable":                 probeJSONParseable,
	"dual-form-present":              probeDualForm,
	"success-exit-zero":              probeSuccessExitZero,
	"unknown-subcommand-exit":        probeUnknownSubcommand,
	"missing-credential-exit":        probeMissingCredential,
	"not-found-exit":                 probeNotFound,
	"api-error-exit":                 probeAPIError,
	"forbidden-exit":                 probeForbidden,
	"rate-limited-exit":              probeRateLimited,
	"dry-run-accepted":               probeDryRunAccepted,
	"dry-run-no-side-effects":        probeDryRunNoSideEffects,
	"destructive-dry-run-no-confirm": probeDestructiveDryRunNoConfirm,
	"destructive-requires-confirm":   probeDestructiveRequiresConfirm,
	"repeatable-output":              probeRepeatable,
	"stable-sort":                    probeStableSort,
	"ambiguous-name-no-prompt":       probeAmbiguousNoPrompt,
	"help-has-examples":              probeHelpExamples,
	"help-has-related":               probeHelpRelated,
	"update-check-readonly":          probeUpdateCheckReadonly,
	"update-check-json":              probeUpdateCheckJSON,
	"secret-not-printed":             probeSecretNotPrinted,
	"config-flag-override":           probeConfigFlag,
}

// ProbeNames returns the registered probe names.
func ProbeNames() []string {
	names := make([]string, 0, len(probes))
	for k := range probes {
		names = append(names, k)
	}
	return names
}

func withFlag(args []string, flags ...string) []string {
	out := append([]string(nil), args...)
	return append(out, flags...)
}

func probeJSONParseable(r *Runner) ProbeResult {
	if r.target.Read == nil {
		return na("no read command configured")
	}
	entry, res, stop := r.manifestFixture(r.target.Read)
	if stop {
		return res
	}
	if entry != nil && !hasOutputMode(entry, "json") {
		return fail("manifest says the read command lacks a json output mode")
	}
	inv := r.run(withFlag(r.target.Read, "--json"))
	if inv.Exit != 0 {
		return fail(exitReason(inv, "read --json"))
	}
	if !json.Valid([]byte(inv.Stdout)) {
		return fail("stdout is not valid JSON")
	}
	return pass("read --json emitted valid JSON")
}

func probeDualForm(r *Runner) ProbeResult {
	if r.target.Read == nil {
		return na("no read command configured")
	}
	entry, res, stop := r.manifestFixture(r.target.Read)
	if stop {
		return res
	}
	if entry != nil {
		if !hasOutputMode(entry, "json") {
			return fail("manifest says the read command lacks a json output mode")
		}
		if !hasHumanOutputMode(entry) {
			return fail("manifest lists no human (non-json) output mode for the read command")
		}
	}
	human := r.run(r.target.Read)
	jsonInv := r.run(withFlag(r.target.Read, "--json"))
	if human.Exit != 0 {
		return fail(exitReason(human, "human form"))
	}
	if jsonInv.Exit != 0 {
		return fail(exitReason(jsonInv, "JSON form"))
	}
	if strings.TrimSpace(human.Stdout) == "" || strings.TrimSpace(jsonInv.Stdout) == "" {
		return fail("one of the human/JSON forms produced no output")
	}
	if !json.Valid([]byte(jsonInv.Stdout)) {
		return fail("JSON form did not parse")
	}
	return pass("both human and JSON forms produced output")
}

func probeSuccessExitZero(r *Runner) ProbeResult {
	if r.target.Read == nil {
		return na("no read command configured")
	}
	if _, res, stop := r.manifestFixture(r.target.Read); stop {
		return res
	}
	if inv := r.run(r.target.Read); inv.Exit != 0 {
		return fail(exitReason(inv, "read command"))
	}
	return pass("read command exited 0")
}

func probeUnknownSubcommand(r *Runner) ProbeResult {
	inv := r.run([]string{"this-subcommand-does-not-exist"})
	if inv.Exit != 1 {
		return fail("unknown subcommand exited " + itoa(inv.Exit) + ", want 1")
	}
	return pass("unknown subcommand exited 1")
}

func probeMissingCredential(r *Runner) ProbeResult {
	if r.target.Auth == nil {
		return na("no auth-requiring command configured")
	}
	entry, res, stop := r.manifestFixture(r.target.Auth)
	if stop {
		return res
	}
	if entry != nil && !entry.RequiresAuth {
		return fail("manifest says the configured auth command does not require authentication")
	}
	if inv := r.run(r.target.Auth); inv.Exit != 3 {
		return fail("auth command with no credential exited " + itoa(inv.Exit) + ", want 3")
	}
	return pass("missing credential exited 3")
}

func probeNotFound(r *Runner) ProbeResult {
	if r.target.NotFound == nil {
		return na("no not-found command configured")
	}
	if _, res, stop := r.manifestFixture(r.target.NotFound); stop {
		return res
	}
	if inv := r.run(r.target.NotFound); inv.Exit != 5 {
		return fail("missing resource exited " + itoa(inv.Exit) + ", want 5")
	}
	return pass("missing resource exited 5")
}

// These backend-dependent exit-code probes only run when the caller supplies a
// safe fixture command. Without one, the scorer reports not-applicable instead
// of guessing how to trigger an upstream failure, permission denial, or throttle.
func probeAPIError(r *Runner) ProbeResult {
	if r.target.APIError == nil {
		return na("no api-error command configured")
	}
	if _, res, stop := r.manifestFixture(r.target.APIError); stop {
		return res
	}
	if inv := r.run(r.target.APIError); inv.Exit != 2 {
		return fail("api-error command exited " + itoa(inv.Exit) + ", want 2")
	}
	return pass("api-error command exited 2")
}

func probeForbidden(r *Runner) ProbeResult {
	if r.target.Forbidden == nil {
		return na("no forbidden command configured")
	}
	if _, res, stop := r.manifestFixture(r.target.Forbidden); stop {
		return res
	}
	if inv := r.run(r.target.Forbidden); inv.Exit != 4 {
		return fail("forbidden command exited " + itoa(inv.Exit) + ", want 4")
	}
	return pass("forbidden command exited 4")
}

func probeRateLimited(r *Runner) ProbeResult {
	if r.target.RateLimited == nil {
		return na("no rate-limited command configured")
	}
	if _, res, stop := r.manifestFixture(r.target.RateLimited); stop {
		return res
	}
	if inv := r.run(r.target.RateLimited); inv.Exit != 6 {
		return fail("rate-limited command exited " + itoa(inv.Exit) + ", want 6")
	}
	return pass("rate-limited command exited 6")
}

func probeDryRunAccepted(r *Runner) ProbeResult {
	if r.target.Mutate == nil {
		return na("no mutating command configured")
	}
	entry, res, stop := r.manifestFixture(r.target.Mutate)
	if stop {
		return res
	}
	if entry != nil && (!entry.Mutates || !entry.SupportsDryRun) {
		return fail("manifest says the configured mutate command does not mutate or support --dry-run")
	}
	if inv := r.run(withFlag(r.target.Mutate, "--dry-run")); inv.Exit != 0 {
		return fail(exitReason(inv, "--dry-run"))
	}
	return pass("mutating command accepted --dry-run")
}

func probeDryRunNoSideEffects(r *Runner) ProbeResult {
	if r.target.Mutate == nil {
		return na("no mutating command configured")
	}
	entry, res, stop := r.manifestFixture(r.target.Mutate)
	if stop {
		return res
	}
	if entry != nil && (!entry.Mutates || !entry.SupportsDryRun) {
		return fail("manifest says the configured mutate command does not mutate or support --dry-run")
	}
	inv := r.run(withFlag(r.target.Mutate, "--dry-run"))
	if inv.Exit != 0 {
		return fail(exitReason(inv, "--dry-run"))
	}
	low := strings.ToLower(inv.Stdout)
	for _, done := range []string{"created ", "updated ", "deleted ", "removed ", "successfully"} {
		if strings.Contains(low, done) {
			return fail("--dry-run reported a completed mutation")
		}
	}
	if strings.Contains(low, "dry") || strings.Contains(low, "no change") || strings.Contains(low, "would ") {
		return pass("--dry-run emitted a preview")
	}
	return fail("--dry-run output did not look like a preview")
}

// probeDestructiveDryRunNoConfirm checks the safe preview half of the destructive
// contract. The scorer never passes a confirmation flag; it only verifies that
// dry-run previews without acting or asking for confirmation.
func probeDestructiveDryRunNoConfirm(r *Runner) ProbeResult {
	if r.target.Destructive == nil {
		return na("no destructive command configured")
	}
	entry, res, stop := r.manifestFixture(r.target.Destructive)
	if stop {
		return res
	}
	if entry != nil && (!entry.Destructive || !entry.RequiresConfirmation) {
		return fail("manifest says the configured destructive command is not destructive or does not require confirmation")
	}
	inv := r.run(withFlag(r.target.Destructive, "--dry-run", "--no-prompt"))
	if inv.Exit != 0 {
		return fail(exitReason(inv, "destructive --dry-run"))
	}
	low := strings.ToLower(inv.Stdout)
	for _, done := range []string{"created ", "updated ", "deleted ", "removed ", "successfully"} {
		if strings.Contains(low, done) {
			return fail("destructive --dry-run reported a completed mutation")
		}
	}
	if strings.Contains(low, "dry") || strings.Contains(low, "no change") || strings.Contains(low, "would ") {
		return pass("destructive command previewed under --dry-run without confirmation")
	}
	return fail("destructive --dry-run output did not look like a preview")
}

// probeDestructiveRequiresConfirm checks the refusal half of the destructive
// contract. Each non-interactive mode uses blocking stdin so a target that tries
// to prompt or read from stdin times out instead of passing by receiving EOF.
func probeDestructiveRequiresConfirm(r *Runner) ProbeResult {
	if r.target.Destructive == nil {
		return na("no destructive command configured")
	}
	entry, res, stop := r.manifestFixture(r.target.Destructive)
	if stop {
		return res
	}
	if entry != nil && (!entry.Destructive || !entry.RequiresConfirmation) {
		return fail("manifest says the configured destructive command is not destructive or does not require confirmation")
	}
	// No terminal and no --confirm: must refuse with exit 1 without reading stdin.
	noTerminal := r.runWithBlockingStdin(r.target.Destructive)
	if noTerminal.TimedOut {
		return fail("destructive command without --confirm blocked or read stdin with no terminal")
	}
	if noTerminal.Exit != 1 {
		return fail("destructive command without confirmation with no terminal exited " + itoa(noTerminal.Exit) + ", want 1")
	}
	// --no-prompt: must refuse with exit 1 without reading stdin.
	noPrompt := r.runWithBlockingStdin(withFlag(r.target.Destructive, "--no-prompt"))
	if noPrompt.TimedOut {
		return fail("destructive command without --confirm blocked or read stdin under --no-prompt")
	}
	if noPrompt.Exit != 1 {
		return fail("destructive command without confirmation under --no-prompt exited " + itoa(noPrompt.Exit) + ", want 1")
	}
	// --json: must refuse with exit 1 and a structured error body on stderr,
	// without reading stdin.
	jsonInv := r.runWithBlockingStdin(withFlag(r.target.Destructive, "--json"))
	if jsonInv.TimedOut {
		return fail("destructive command without --confirm blocked or read stdin under --json")
	}
	if jsonInv.Exit != 1 {
		return fail("destructive command without confirmation under --json exited " + itoa(jsonInv.Exit) + ", want 1")
	}
	var body struct {
		ExitCode int `json:"exit_code"`
	}
	if !json.Valid([]byte(jsonInv.Stderr)) || json.Unmarshal([]byte(jsonInv.Stderr), &body) != nil {
		return fail("destructive --json refusal did not emit a JSON error body on stderr")
	}
	if body.ExitCode != 1 {
		return fail("destructive --json refusal body reported exit_code " + itoa(body.ExitCode) + ", want 1")
	}
	return pass("destructive command refused to act without confirmation when non-interactive")
}

func probeRepeatable(r *Runner) ProbeResult {
	if r.target.Read == nil {
		return na("no read command configured")
	}
	if _, res, stop := r.manifestFixture(r.target.Read); stop {
		return res
	}
	a := r.run(withFlag(r.target.Read, "--json"))
	b := r.run(withFlag(r.target.Read, "--json"))
	if a.Exit != 0 || b.Exit != 0 {
		return fail("repeatability command did not exit 0")
	}
	if a.Stdout != b.Stdout {
		return fail("two --json runs were not byte-identical")
	}
	return pass("repeated --json runs were byte-identical")
}

func probeStableSort(r *Runner) ProbeResult {
	if r.target.Read == nil {
		return na("no read command configured")
	}
	if _, res, stop := r.manifestFixture(r.target.Read); stop {
		return res
	}
	a := r.run(r.target.Read)
	b := r.run(r.target.Read)
	if a.Exit != 0 || b.Exit != 0 {
		return fail("list command did not exit 0")
	}
	if a.Stdout != b.Stdout {
		return fail("list order changed across runs")
	}
	return pass("list order stable across runs")
}

func probeAmbiguousNoPrompt(r *Runner) ProbeResult {
	if r.target.Ambiguous == nil {
		return na("no ambiguous-resolution command configured")
	}
	if _, res, stop := r.manifestFixture(r.target.Ambiguous); stop {
		return res
	}
	inv := r.run(withFlag(r.target.Ambiguous, "--no-prompt"))
	if inv.Exit != 1 {
		if inv.Exit == 0 {
			return fail("ambiguous name with --no-prompt unexpectedly succeeded")
		}
		if inv.Exit < 0 {
			return fail("ambiguous name with --no-prompt blocked or crashed")
		}
		return fail("ambiguous name with --no-prompt exited " + itoa(inv.Exit) + ", want 1")
	}
	combined := strings.ToLower(inv.Stdout + "\n" + inv.Stderr)
	if !strings.Contains(combined, "candidate") && !strings.Contains(combined, "candidates") {
		return fail("ambiguous name did not report candidates")
	}
	return pass("ambiguous name with --no-prompt exited 1 and reported candidates")
}

func probeHelpExamples(r *Runner) ProbeResult {
	if r.target.Read == nil {
		return na("no command to inspect help on")
	}
	entry, res, stop := r.manifestFixture(r.target.Read)
	if stop {
		return res
	}
	if entry != nil && len(entry.Examples) == 0 {
		return fail("manifest lists no examples for the read command")
	}
	inv := r.run(withFlag(r.target.Read, "--help"))
	if inv.Exit != 0 {
		return fail(exitReason(inv, "--help"))
	}
	if entry != nil {
		for _, ex := range entry.Examples {
			if strings.Contains(inv.Stdout, ex) {
				return pass("--help contains a manifest example")
			}
		}
		return fail("--help output contains none of the manifest examples")
	}
	if strings.Contains(strings.ToLower(inv.Stdout), "examples:") {
		return pass("--help contains an examples section")
	}
	return fail("--help has no examples section")
}

func probeHelpRelated(r *Runner) ProbeResult {
	if r.target.Read == nil {
		return na("no command to inspect help on")
	}
	entry, res, stop := r.manifestFixture(r.target.Read)
	if stop {
		return res
	}
	if entry != nil && len(entry.Related) == 0 {
		return fail("manifest lists no related commands for the read command")
	}
	inv := r.run(withFlag(r.target.Read, "--help"))
	if inv.Exit != 0 {
		return fail(exitReason(inv, "--help"))
	}
	if entry != nil {
		for _, related := range entry.Related {
			if strings.Contains(inv.Stdout, related) {
				return pass("--help contains a manifest related command")
			}
		}
		return fail("--help output contains none of the manifest related commands")
	}
	if strings.Contains(strings.ToLower(inv.Stdout), "related commands") {
		return pass("--help points to related commands")
	}
	return fail("--help does not point to related commands")
}

func probeUpdateCheckReadonly(r *Runner) ProbeResult {
	if !r.target.HasUpdate {
		return na("no update command")
	}
	if _, res, stop := r.manifestFixture([]string{"update"}); stop {
		return res
	}
	before, err := fileHash(r.target.Path)
	if err != nil {
		return fail("could not hash target before update --check: " + err.Error())
	}
	if inv := r.run([]string{"update", "--check"}); inv.Exit != 0 {
		return fail(exitReason(inv, "update --check"))
	}
	after, err := fileHash(r.target.Path)
	if err != nil {
		return fail("could not hash target after update --check: " + err.Error())
	}
	if before != after {
		return fail("update --check modified the target binary")
	}
	return pass("update --check exited 0 and left target binary unchanged")
}

func probeUpdateCheckJSON(r *Runner) ProbeResult {
	if !r.target.HasUpdate {
		return na("no update command")
	}
	entry, res, stop := r.manifestFixture([]string{"update"})
	if stop {
		return res
	}
	if entry != nil && !hasOutputMode(entry, "json") {
		return fail("manifest says the update command lacks a json output mode")
	}
	inv := r.run([]string{"update", "--check", "--json"})
	if inv.Exit != 0 || !json.Valid([]byte(inv.Stdout)) {
		return fail("update --check --json did not emit valid JSON")
	}
	return pass("update --check --json emitted valid JSON")
}

func probeSecretNotPrinted(r *Runner) ProbeResult {
	if r.target.Secret == nil || r.target.SecretEnv == "" {
		return na("no credential-handling command configured")
	}
	entry, res, stop := r.manifestFixture(r.target.Secret)
	if stop {
		return res
	}
	if entry != nil && !entry.RequiresAuth {
		return fail("manifest says the configured secret command does not require authentication")
	}
	inv := r.exec(r.target.Secret, r.target.SecretEnv, secretSentinel)
	jsonInv := r.exec(withFlag(r.target.Secret, "--json"), r.target.SecretEnv, secretSentinel)
	if strings.Contains(inv.Stdout, secretSentinel) || strings.Contains(inv.Stderr, secretSentinel) {
		return fail("the credential value was printed")
	}
	if strings.Contains(jsonInv.Stdout, secretSentinel) || strings.Contains(jsonInv.Stderr, secretSentinel) {
		return fail("the credential value was printed in JSON mode")
	}
	if inv.Exit != 0 || jsonInv.Exit != 0 {
		return fail("credential command did not exit 0 in both modes")
	}
	return pass("the credential value was not printed in human or JSON mode")
}

func probeConfigFlag(r *Runner) ProbeResult {
	if r.target.Read == nil {
		return na("no read command configured")
	}
	if _, res, stop := r.manifestFixture(r.target.Read); stop {
		return res
	}
	// The config flag is global manifest metadata, so path validation and the
	// global flag lookup are separate checks.
	if present, active := r.manifestGlobalFlag("config"); active && !present {
		return fail("manifest global_flags does not list the config flag")
	}
	alt := filepath.Join(r.confHome, "alt-config.yaml")
	plain := r.run(r.target.Read)
	if plain.Exit != 0 {
		return fail(exitReason(plain, "read command"))
	}
	if inv := r.run(withFlag(r.target.Read, "--config", alt)); inv.Exit != 0 {
		return fail(exitReason(inv, "read with --config"))
	}
	return pass("read accepted an isolated --config path")
}

func fileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func exitReason(inv Invocation, label string) string {
	if inv.Err != "" {
		return label + " exited " + itoa(inv.Exit) + " (" + inv.Err + ")"
	}
	return label + " exited " + itoa(inv.Exit)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
