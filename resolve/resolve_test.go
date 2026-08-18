package resolve

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func lookupFixed(matches ...Match) Lookup {
	return func(string) ([]Match, error) { return matches, nil }
}

func TestResolveUniqueMatch(t *testing.T) {
	id, err := Resolve("alpha", lookupFixed(Match{ID: "1", Name: "alpha"}), Options{ResourceType: "project"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "1" {
		t.Fatalf("got %q, want 1", id)
	}
}

func TestResolveIDShortCircuit(t *testing.T) {
	called := false
	lookup := func(string) ([]Match, error) { called = true; return nil, nil }
	id, err := Resolve("42", lookup, Options{IsID: IsNumericID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "42" {
		t.Fatalf("got %q, want 42", id)
	}
	if called {
		t.Fatalf("lookup should be skipped when input is an ID")
	}
}

func TestResolveNotFound(t *testing.T) {
	_, err := Resolve("ghost", lookupFixed(), Options{ResourceType: "project"})
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if nf.ExitCode() != 5 {
		t.Fatalf("not-found exit code = %d, want 5", nf.ExitCode())
	}
}

func TestResolveAmbiguousNonInteractiveReturnsError(t *testing.T) {
	_, err := Resolve("dup", lookupFixed(
		Match{ID: "2", Name: "dup", Context: "team b"},
		Match{ID: "1", Name: "dup", Context: "team a"},
	), Options{ResourceType: "project", AllowPrompt: false})
	var amb *AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("expected AmbiguousError, got %v", err)
	}
	if len(amb.Matches) != 2 {
		t.Fatalf("ambiguous error should carry candidates, got %d", len(amb.Matches))
	}
	if amb.Matches[0].ID != "1" || amb.Matches[1].ID != "2" {
		t.Fatalf("ambiguous matches not sorted by stable key: %+v", amb.Matches)
	}
	if amb.ExitCode() != 1 {
		t.Fatalf("ambiguous exit code = %d, want 1", amb.ExitCode())
	}
}

func TestResolveAmbiguousInteractiveChooses(t *testing.T) {
	prompter := StdPrompter{In: strings.NewReader("2\n"), Out: &bytes.Buffer{}}
	id, err := Resolve("dup", lookupFixed(
		Match{ID: "1", Name: "dup"},
		Match{ID: "2", Name: "dup"},
	), Options{ResourceType: "project", AllowPrompt: true, Prompt: prompter})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "2" {
		t.Fatalf("got %q, want 2", id)
	}
}

func TestStdPrompterRejectsBadSelection(t *testing.T) {
	prompter := StdPrompter{In: strings.NewReader("9\n"), Out: &bytes.Buffer{}}
	_, err := prompter.Choose("project", "dup", []Match{{ID: "1", Name: "dup"}, {ID: "2", Name: "dup"}})
	if err == nil {
		t.Fatalf("expected error for out-of-range selection")
	}
}

func TestStdPrompterTransformRedactsPrompt(t *testing.T) {
	const secret = "secret-token"
	var out bytes.Buffer
	prompter := StdPrompter{
		In:  strings.NewReader("1\n"),
		Out: &out,
		Transform: func(b []byte) []byte {
			return bytes.ReplaceAll(b, []byte(secret), []byte("[REDACTED]"))
		},
	}
	_, err := prompter.Choose("project", "dup-"+secret, []Match{
		{ID: "id-" + secret, Name: "name-" + secret, Context: "ctx-" + secret},
	})
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if strings.Contains(out.String(), secret) {
		t.Fatalf("prompt leaked secret: %q", out.String())
	}
	if !strings.Contains(out.String(), "[REDACTED]") {
		t.Fatalf("prompt missing replacement: %q", out.String())
	}
}
