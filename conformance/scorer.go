package conformance

import (
	"fmt"
	"sort"
	"strings"
)

// RuleScore is the outcome of one rule against a target.
type RuleScore struct {
	ID       string `json:"id"`
	Section  string `json:"section"`
	Severity string `json:"severity"`
	Result   string `json:"result"`
	Reason   string `json:"reason"`
}

// SectionScore aggregates a section's results. Score is the weighted pass ratio
// over applicable rules (not-applicable rules are excluded).
type SectionScore struct {
	Section    string  `json:"section"`
	Passed     int     `json:"passed"`
	Applicable int     `json:"applicable"`
	Score      float64 `json:"score"`

	wPass   float64
	wApplic float64
}

// Score is a full conformance result for a target.
type Score struct {
	SpecVersion string          `json:"spec_version"`
	Overall     float64         `json:"overall"`
	Passed      int             `json:"passed"`
	Applicable  int             `json:"applicable"`
	Total       int             `json:"total"`
	Manifest    ManifestSummary `json:"manifest"`
	Sections    []SectionScore  `json:"sections"`
	Rules       []RuleScore     `json:"rules"`
}

func weightOf(severity string) float64 {
	if severity == "required" {
		return 2
	}
	return 1
}

// Score runs every rule's probe against the target and aggregates the results,
// weighting required rules above recommended ones.
func (r *Runner) Score(rs Ruleset) Score {
	out := Score{SpecVersion: rs.Version, Manifest: r.manifestSummaryForScore()}
	sections := map[string]*SectionScore{}
	var order []string
	var wPass, wApplic float64
	// Keep this non-nil so score JSON reports [] instead of null when no rule
	// consults the manifest.
	usedRules := []string{}

	for _, rule := range rs.Rules {
		out.Total++
		// Probes mark this through manifestActive. Reset before each rule so n/a
		// and black-box-only probes cannot inherit a prior rule's manifest use.
		r.manifestConsulted = false
		var pr ProbeResult
		if probe := probes[rule.Probe]; probe != nil {
			pr = probe(r)
		} else {
			pr = na("no probe implementation: " + rule.Probe)
		}
		if r.manifestConsulted {
			usedRules = append(usedRules, rule.ID)
		}
		out.Rules = append(out.Rules, RuleScore{
			ID: rule.ID, Section: rule.Section, Severity: rule.Severity,
			Result: pr.Result, Reason: pr.Reason,
		})

		s, ok := sections[rule.Section]
		if !ok {
			s = &SectionScore{Section: rule.Section}
			sections[rule.Section] = s
			order = append(order, rule.Section)
		}
		w := weightOf(rule.Severity)
		switch pr.Result {
		case ResultPass:
			out.Passed++
			out.Applicable++
			s.Passed++
			s.Applicable++
			s.wPass += w
			s.wApplic += w
			wPass += w
			wApplic += w
		case ResultFail:
			out.Applicable++
			s.Applicable++
			s.wApplic += w
			wApplic += w
		}
	}

	for _, name := range order {
		s := sections[name]
		if s.wApplic > 0 {
			s.Score = s.wPass / s.wApplic
		} else {
			s.Score = 1
		}
		out.Sections = append(out.Sections, *s)
	}
	if wApplic > 0 {
		out.Overall = wPass / wApplic
	} else {
		out.Overall = 1
	}
	sort.Strings(usedRules)
	out.Manifest.UsedRules = usedRules
	out.Manifest.UsedRuleCount = len(usedRules)
	return out
}

// RequiredFailures returns the rule ids of required rules that failed.
func (s Score) RequiredFailures() []string {
	var ids []string
	for _, r := range s.Rules {
		if r.Severity == "required" && r.Result == ResultFail {
			ids = append(ids, r.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// Report renders a human-readable score grouped by section.
func (s Score) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Conformance against %s\n", s.SpecVersion)
	fmt.Fprintf(&b, "Overall: %.0f%% weighted (%d/%d applicable rules passed, %d total rules)\n", s.Overall*100, s.Passed, s.Applicable, s.Total)
	if s.Manifest.Status == ManifestPresent {
		tool := s.Manifest.ToolName
		if s.Manifest.ToolVersion != "" {
			tool += " " + s.Manifest.ToolVersion
		}
		fmt.Fprintf(&b, "Manifest: present (%s, %s, %d commands)\n", s.Manifest.SchemaVersion, tool, s.Manifest.CommandCount)
		fmt.Fprintf(&b, "Manifest-backed: %d rules used manifest data\n", s.Manifest.UsedRuleCount)
	} else {
		fmt.Fprintf(&b, "Manifest: %s (%s)\n", s.Manifest.Status, s.Manifest.Reason)
	}
	b.WriteString("\n")
	for _, sec := range s.Sections {
		if sec.Applicable == 0 {
			fmt.Fprintf(&b, "%s: n/a (0 applicable)\n", sec.Section)
			continue
		}
		fmt.Fprintf(&b, "%s: %.0f%% weighted (%d/%d applicable rules passed)\n", sec.Section, sec.Score*100, sec.Passed, sec.Applicable)
	}
	b.WriteString("\n")
	for _, r := range s.Rules {
		mark := map[string]string{ResultPass: "PASS", ResultFail: "FAIL", ResultNotApplicable: "n/a "}[r.Result]
		fmt.Fprintf(&b, "  [%s] %-28s %s\n", mark, r.ID, r.Reason)
	}
	return b.String()
}
