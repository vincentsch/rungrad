// Package conformance loads the rungrad spec ruleset, drives a target CLI through
// a set of probes, and scores it. It is the public, shareable artifact: a way to
// measure any executable against the agent-ready spec.
//
// The runner executes the target as a subprocess in a controlled environment
// (an empty config home, no real credentials) so probes are reproducible and
// cannot touch a developer's real configuration.
package conformance

import (
	"fmt"

	"github.com/vincentsch/rungrad/spec"
	"gopkg.in/yaml.v3"
)

// Rule is one scored assertion from the ruleset.
type Rule struct {
	ID       string   `yaml:"id" json:"id"`
	Section  string   `yaml:"section" json:"section"`
	Title    string   `yaml:"title" json:"title"`
	Severity string   `yaml:"severity" json:"severity"`
	Probe    string   `yaml:"probe" json:"probe"`
	Args     []string `yaml:"args,omitempty" json:"args,omitempty"`
}

// Ruleset is the parsed ruleset with its spec version.
type Ruleset struct {
	Version string `yaml:"version" json:"version"`
	Rules   []Rule `yaml:"rules" json:"rules"`
}

// LoadRuleset parses a ruleset from YAML.
func LoadRuleset(data []byte) (Ruleset, error) {
	var rs Ruleset
	if err := yaml.Unmarshal(data, &rs); err != nil {
		return Ruleset{}, fmt.Errorf("parse ruleset: %w", err)
	}
	if rs.Version == "" {
		return Ruleset{}, fmt.Errorf("ruleset has no version")
	}
	return rs, nil
}

// DefaultRuleset returns the ruleset embedded in the spec package.
func DefaultRuleset() (Ruleset, error) {
	return LoadRuleset(spec.RulesetYAML)
}

// Result values a probe can return.
const (
	ResultPass          = "pass"
	ResultFail          = "fail"
	ResultNotApplicable = "not_applicable"
)

// Manifest discovery statuses reported in a Score's manifest summary.
const (
	ManifestDisabled    = "disabled"
	ManifestMissing     = "missing"
	ManifestPresent     = "present"
	ManifestInvalid     = "invalid"
	ManifestUnsupported = "unsupported"
)

// Manifest discovery modes carried on Target.ManifestMode (zero value = auto).
const (
	ManifestModeAuto     = "auto"
	ManifestModeOff      = "off"
	ManifestModeRequired = "required"
)

// ProbeResult is the outcome of a single probe.
type ProbeResult struct {
	Result string
	Reason string
}

func pass(reason string) ProbeResult { return ProbeResult{Result: ResultPass, Reason: reason} }
func fail(reason string) ProbeResult { return ProbeResult{Result: ResultFail, Reason: reason} }
func na(reason string) ProbeResult   { return ProbeResult{Result: ResultNotApplicable, Reason: reason} }
