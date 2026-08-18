package conformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/vincentsch/rungrad/manifest"
)

// manifestEndpointArg is the single, flag-free argument shape of the hidden
// rungrad manifest command (p08-001 sets DisableFlagParsing on it).
const manifestEndpointArg = "__rungrad_manifest"

const (
	reasonDiscoveryDisabled = "manifest discovery disabled"
	reasonDiscoveryNotRun   = "manifest discovery was not run"
)

// ManifestSummary is the compact manifest-discovery result embedded in a Score.
// schema_version here is the manifest schema (rungrad-manifest/1), not the scored
// ruleset version. For invalid/unsupported statuses the populated fields are
// diagnostic only and do not mean the manifest was cached or used.
type ManifestSummary struct {
	Status        string `json:"status"`
	SchemaVersion string `json:"schema_version"`
	ToolName      string `json:"tool_name"`
	ToolVersion   string `json:"tool_version"`
	CommandCount  int    `json:"command_count"`
	Reason        string `json:"reason"`
	// UsedRuleCount and UsedRules are filled by Score, not discovery. A rule is
	// listed once it consults a present manifest, even if the manifest pre-check
	// is what makes the rule fail.
	UsedRuleCount int      `json:"used_rule_count"`
	UsedRules     []string `json:"used_rules"`
}

// normalizeManifestMode validates the mode string; empty means auto.
func normalizeManifestMode(mode string) (string, error) {
	switch mode {
	case "", ManifestModeAuto:
		return ManifestModeAuto, nil
	case ManifestModeOff:
		return ManifestModeOff, nil
	case ManifestModeRequired:
		return ManifestModeRequired, nil
	default:
		return "", &TargetError{Message: fmt.Sprintf("unknown manifest mode %q (want auto, off, or required)", mode)}
	}
}

// DiscoverManifest runs the target's manifest endpoint at most once, classifies
// and caches the result, and returns a *TargetError only when the mode is
// required and no supported manifest was found, or when the mode is unknown.
// It is idempotent: repeated calls and the later Score read never re-invoke the
// endpoint.
func (r *Runner) DiscoverManifest() error {
	if !r.manifestDiscovered {
		mode, err := normalizeManifestMode(r.target.ManifestMode)
		if err != nil {
			return err
		}
		r.manifestMode = mode
		r.manifestDiscovered = true
		if mode == ManifestModeOff {
			r.manifestSummary = ManifestSummary{Status: ManifestDisabled, Reason: reasonDiscoveryDisabled}
		} else {
			r.cachedManifest, r.manifestSummary = classifyManifest(r.run([]string{manifestEndpointArg}))
		}
	}
	if r.manifestMode == ManifestModeRequired && r.manifestSummary.Status != ManifestPresent {
		msg := fmt.Sprintf("manifest required but discovery reported status %q", r.manifestSummary.Status)
		if r.manifestSummary.Reason != "" {
			msg += ": " + r.manifestSummary.Reason
		}
		return &TargetError{Message: msg}
	}
	return nil
}

// manifestSummaryForScore returns the cached summary, or the disabled/not-run
// summary when DiscoverManifest was never called. Score embeds this; it never
// invokes the endpoint.
func (r *Runner) manifestSummaryForScore() ManifestSummary {
	if !r.manifestDiscovered {
		return ManifestSummary{Status: ManifestDisabled, Reason: reasonDiscoveryNotRun}
	}
	return r.manifestSummary
}

// classifyManifest applies the ordered discovery procedure to one endpoint
// invocation. It returns the parsed manifest only when the status is present.
func classifyManifest(inv Invocation) (*manifest.Manifest, ManifestSummary) {
	// A timeout also has Exit < 0, so classify it first to keep the reason clear.
	if inv.TimedOut {
		return nil, ManifestSummary{Status: ManifestInvalid, Reason: "manifest endpoint timed out"}
	}
	if inv.Exit < 0 {
		reason := strings.TrimSpace(inv.Err)
		if reason == "" {
			reason = "target exited without an exit code"
		}
		return nil, ManifestSummary{Status: ManifestInvalid, Reason: reason}
	}
	trimmed := strings.TrimSpace(inv.Stdout)
	if inv.Exit != 0 {
		// A target that lacks the hidden endpoint usually exits non-zero with
		// human error text or no output. Treat that as absent, not malformed.
		if trimmed == "" || trimmed[0] != '{' {
			return nil, ManifestSummary{
				Status: ManifestMissing,
				Reason: fmt.Sprintf("manifest endpoint not found: exit %d with no manifest JSON on stdout", inv.Exit),
			}
		}
		// JSON-shaped stdout with a failing exit means the endpoint exists but did
		// not complete successfully. Decode only safe diagnostic fields.
		summary := ManifestSummary{
			Status: ManifestInvalid,
			Reason: fmt.Sprintf("manifest endpoint returned manifest-shaped output but exited %d", inv.Exit),
		}
		var m manifest.Manifest
		if json.Unmarshal([]byte(inv.Stdout), &m) == nil {
			fillManifestSummary(&summary, &m)
		}
		return nil, summary
	}

	// Exit 0 means the endpoint claimed success, so parse and validate the schema
	// before deciding whether the manifest is usable.
	var m manifest.Manifest
	if err := json.Unmarshal([]byte(inv.Stdout), &m); err != nil {
		return nil, ManifestSummary{Status: ManifestInvalid, Reason: "manifest JSON did not parse: " + err.Error()}
	}
	summary := ManifestSummary{}
	fillManifestSummary(&summary, &m)
	if err := manifest.Validate(&m); err != nil {
		var unsupported *manifest.UnsupportedVersionError
		if errors.As(err, &unsupported) {
			summary.Status = ManifestUnsupported
			summary.Reason = unsupported.Error()
			return nil, summary
		}
		summary.Status = ManifestInvalid
		summary.Reason = err.Error()
		return nil, summary
	}
	summary.Status = ManifestPresent
	summary.Reason = ""
	return &m, summary
}

// fillManifestSummary copies the safe, diagnostic fields out of a decoded
// manifest struct. It does not imply caching or validity.
func fillManifestSummary(s *ManifestSummary, m *manifest.Manifest) {
	s.SchemaVersion = m.SchemaVersion
	s.ToolName = m.ToolName
	s.ToolVersion = m.ToolVersion
	s.CommandCount = len(m.Commands)
}
