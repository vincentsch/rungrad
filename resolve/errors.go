package resolve

import (
	"fmt"
	"strings"
)

// AmbiguousError reports that a name matched more than one resource. It carries
// the candidate matches so a non-interactive caller can choose deterministically.
// The root package maps it to the usage exit code.
type AmbiguousError struct {
	ResourceType string
	Name         string
	Matches      []Match
}

func (e *AmbiguousError) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ambiguous %s name %q, candidates:", e.ResourceType, e.Name)
	for _, m := range e.Matches {
		b.WriteString("\n  ")
		b.WriteString(m.Name)
		if m.Context != "" {
			b.WriteString(" (" + m.Context + ")")
		}
		if m.ID != "" {
			b.WriteString(" [" + m.ID + "]")
		}
	}
	return b.String()
}

// ExitCode maps ambiguity to the usage/validation exit code.
func (e *AmbiguousError) ExitCode() int { return 1 }

// ErrorDetails returns machine-readable ambiguity details for JSON error bodies.
func (e *AmbiguousError) ErrorDetails() any {
	if e == nil {
		return nil
	}
	return map[string]any{
		"resource_type": e.ResourceType,
		"name":          e.Name,
		"candidates":    e.Matches,
	}
}

// NotFoundError reports that no resource matched a name. It carries the available
// names when known. The root package maps it to the not-found exit code.
type NotFoundError struct {
	ResourceType string
	Name         string
	Available    []string
}

func (e *NotFoundError) Error() string {
	if e == nil {
		return ""
	}
	base := fmt.Sprintf("%s %q not found", e.ResourceType, e.Name)
	if len(e.Available) == 0 {
		return base
	}
	return base + ", available: " + strings.Join(e.Available, ", ")
}

// ExitCode maps a missing resource to the not-found exit code.
func (e *NotFoundError) ExitCode() int { return 5 }

// ErrorDetails returns machine-readable not-found details for JSON error bodies.
func (e *NotFoundError) ErrorDetails() any {
	if e == nil {
		return nil
	}
	return map[string]any{
		"resource_type": e.ResourceType,
		"name":          e.Name,
		"available":     e.Available,
	}
}
