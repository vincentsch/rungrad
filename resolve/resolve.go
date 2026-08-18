// Package resolve turns a human-supplied name into the opaque identifier an API
// needs. It supports interactive disambiguation when a name matches more than
// one resource, and a deterministic non-interactive path for scripts and agents.
//
// The package defines two typed errors, AmbiguousError and NotFoundError, that
// the root package maps to the usage and not-found exit codes. Under --no-prompt
// or --json a tool never blocks on a prompt: it returns AmbiguousError with the
// candidates listed so the caller can choose deterministically.
package resolve

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Match is one candidate resource for a name, with optional disambiguating
// context such as a parent or team.
type Match struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Context string `json:"context,omitempty"`
}

// Lookup returns candidate matches for a human-supplied name.
type Lookup func(name string) ([]Match, error)

// Prompter asks a human to choose among ambiguous matches and returns the choice.
type Prompter interface {
	Choose(resourceType, name string, matches []Match) (Match, error)
}

// Options controls a single resolution.
type Options struct {
	// ResourceType labels the resource in error and prompt text, e.g. "project".
	ResourceType string
	// AllowPrompt permits interactive disambiguation. It should be true only on a
	// terminal and when --no-prompt is not set. See IsInteractive.
	AllowPrompt bool
	// IsID, when set and true for the input, short-circuits resolution and returns
	// the input unchanged. Use it when a value already looks like an identifier.
	IsID func(string) bool
	// Prompt overrides the disambiguation prompter. When nil and AllowPrompt is
	// true, a prompter reading os.Stdin and writing os.Stderr is used.
	Prompt Prompter
}

// Resolve maps input to a single identifier using lookup, or returns a typed
// error. With multiple matches it disambiguates interactively when permitted,
// otherwise it returns AmbiguousError.
func Resolve(input string, lookup Lookup, opts Options) (string, error) {
	if opts.IsID != nil && opts.IsID(input) {
		return input, nil
	}
	matches, err := lookup(input)
	if err != nil {
		return "", err
	}
	matches = sortedMatches(matches)
	switch len(matches) {
	case 0:
		return "", &NotFoundError{ResourceType: opts.ResourceType, Name: input}
	case 1:
		return matches[0].ID, nil
	default:
		if !opts.AllowPrompt {
			return "", &AmbiguousError{ResourceType: opts.ResourceType, Name: input, Matches: matches}
		}
		prompter := opts.Prompt
		if prompter == nil {
			prompter = StdPrompter{In: os.Stdin, Out: os.Stderr}
		}
		chosen, err := prompter.Choose(opts.ResourceType, input, matches)
		if err != nil {
			return "", err
		}
		return chosen.ID, nil
	}
}

func sortedMatches(matches []Match) []Match {
	out := append([]Match(nil), matches...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Context < b.Context
	})
	return out
}

// IsNumericID reports whether s is a non-empty run of digits, a common identifier
// shape. Tools with a different identifier format pass their own predicate.
func IsNumericID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// IsInteractive reports whether interactive prompting is appropriate: prompting
// is not disabled and both standard input and standard error are terminals.
func IsInteractive(noPrompt bool) bool {
	if noPrompt {
		return false
	}
	return isTerminal(os.Stdin) && isTerminal(os.Stderr)
}

func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// StdPrompter reads a numeric selection from In and writes the menu to Out.
type StdPrompter struct {
	In  io.Reader
	Out io.Writer
	// Transform rewrites each prompt chunk before it is written. The root
	// Factory uses this to redact registered secrets from framework-owned prompts.
	Transform func([]byte) []byte
}

// Choose presents a numbered menu and returns the selected match.
func (p StdPrompter) Choose(resourceType, name string, matches []Match) (Match, error) {
	p.writef("Multiple %s matches for %q:\n", resourceType, name)
	for i, m := range matches {
		line := fmt.Sprintf("  %d) %s", i+1, m.Name)
		if m.Context != "" {
			line += " (" + m.Context + ")"
		}
		if m.ID != "" {
			line += " [" + m.ID + "]"
		}
		p.writef("%s\n", line)
	}
	p.writef("Select 1-%d: ", len(matches))

	var raw string
	if _, err := fmt.Fscanln(p.In, &raw); err != nil {
		return Match{}, fmt.Errorf("read selection: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 || n > len(matches) {
		return Match{}, fmt.Errorf("invalid selection %q", raw)
	}
	return matches[n-1], nil
}

func (p StdPrompter) writef(format string, args ...any) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, format, args...)
	b := buf.Bytes()
	if p.Transform != nil {
		b = p.Transform(b)
	}
	_, _ = p.Out.Write(b)
}
