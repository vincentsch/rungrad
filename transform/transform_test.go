package transform_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vincentsch/rungrad/transform"
)

var stableJSON = []byte(`{
  "a": 1,
  "b": 2,
  "field": "alpha",
  "nested": {
    "amp": "a&b",
    "z": "last"
  }
}
`)

func TestValidateJQ(t *testing.T) {
	for _, expr := range []string{".field", ""} {
		if err := transform.ValidateJQ(expr); err != nil {
			t.Fatalf("ValidateJQ(%q) = %v", expr, err)
		}
	}
	for _, expr := range []string{".field |", "undefined_func"} {
		err := transform.ValidateJQ(expr)
		if err == nil {
			t.Fatalf("ValidateJQ(%q) succeeded", expr)
		}
		if !strings.HasPrefix(err.Error(), "invalid --jq expression:") {
			t.Fatalf("ValidateJQ(%q) error = %q", expr, err)
		}
		var terr *transform.Error
		if !errors.As(err, &terr) || terr.Stage != "parse" || terr.ExitCode() != 1 {
			t.Fatalf("ValidateJQ(%q) classified as %#v", expr, err)
		}
	}
}

func TestValidateTemplate(t *testing.T) {
	if err := transform.ValidateTemplate("{{.field}}"); err != nil {
		t.Fatalf("ValidateTemplate valid = %v", err)
	}
	err := transform.ValidateTemplate("{{.bad")
	if err == nil {
		t.Fatal("ValidateTemplate invalid succeeded")
	}
	if !strings.HasPrefix(err.Error(), "invalid --template:") {
		t.Fatalf("ValidateTemplate error = %q", err)
	}
	var terr *transform.Error
	if !errors.As(err, &terr) || terr.Stage != "parse" || terr.ExitCode() != 1 {
		t.Fatalf("ValidateTemplate classified as %#v", err)
	}
}

func TestJQSuccessAndDeterminism(t *testing.T) {
	tests := map[string]string{
		".field":                     "\"alpha\"\n",
		".a, .b":                     "1\n2\n",
		"{b:.b,a:.a}":                "{\"a\":1,\"b\":2}\n",
		"[.a,.b]":                    "[1,2]\n",
		"empty":                      "",
		"":                           "{\"a\":1,\"b\":2,\"field\":\"alpha\",\"nested\":{\"amp\":\"a&b\",\"z\":\"last\"}}\n",
		".nested.amp":                "\"a&b\"\n",
		"{nested:.nested,b:.b,a:.a}": "{\"a\":1,\"b\":2,\"nested\":{\"amp\":\"a&b\",\"z\":\"last\"}}\n",
	}
	for expr, want := range tests {
		t.Run(expr, func(t *testing.T) {
			first, err := transform.JQ(context.Background(), stableJSON, expr)
			if err != nil {
				t.Fatalf("JQ(%q) = %v", expr, err)
			}
			second, err := transform.JQ(context.Background(), stableJSON, expr)
			if err != nil {
				t.Fatalf("second JQ(%q) = %v", expr, err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("JQ(%q) not deterministic:\n%s\n---\n%s", expr, first, second)
			}
			if string(first) != want {
				t.Fatalf("JQ(%q) = %q, want %q", expr, first, want)
			}
		})
	}
}

func TestJQPreservesLargeIntegers(t *testing.T) {
	input := []byte(`{"id":9007199254740993}` + "\n")
	out, err := transform.JQ(context.Background(), input, ".id")
	if err != nil {
		t.Fatalf("JQ large integer = %v", err)
	}
	if string(out) != "9007199254740993\n" {
		t.Fatalf("JQ large integer = %q", out)
	}
}

func TestJQFailuresAreBufferedAndRuntimeClassified(t *testing.T) {
	for _, expr := range []string{`.field, error("boom")`, `halt_error(7)`} {
		t.Run(expr, func(t *testing.T) {
			out, err := transform.JQ(context.Background(), stableJSON, expr)
			if err == nil {
				t.Fatalf("JQ(%q) succeeded with %q", expr, out)
			}
			if len(out) != 0 {
				t.Fatalf("JQ(%q) returned partial output %q", expr, out)
			}
			var terr *transform.Error
			if !errors.As(err, &terr) || terr.Stage != "run" || terr.ExitCode() != 2 {
				t.Fatalf("JQ(%q) classified as %#v", expr, err)
			}
		})
	}
}

func TestTemplateSuccessAndNewlineNormalization(t *testing.T) {
	tests := map[string]string{
		"{{.field}}":      "alpha\n",
		"{{.field}}\n\n":  "alpha\n",
		"{{.a}}":          "1\n",
		"{{.nested.amp}}": "a&b\n",
	}
	for text, want := range tests {
		t.Run(text, func(t *testing.T) {
			out, err := transform.Template(stableJSON, text)
			if err != nil {
				t.Fatalf("Template(%q) = %v", text, err)
			}
			if string(out) != want {
				t.Fatalf("Template(%q) = %q, want %q", text, out, want)
			}
		})
	}
}

func TestTemplatePreservesLargeIntegers(t *testing.T) {
	input := []byte(`{"id":9007199254740993}` + "\n")
	out, err := transform.Template(input, "{{.id}}")
	if err != nil {
		t.Fatalf("Template large integer = %v", err)
	}
	if string(out) != "9007199254740993\n" {
		t.Fatalf("Template large integer = %q", out)
	}
}

func TestTemplateFailureIsBufferedAndRuntimeClassified(t *testing.T) {
	out, err := transform.Template(stableJSON, "{{.missing}}")
	if err == nil {
		t.Fatalf("Template succeeded with %q", out)
	}
	if len(out) != 0 {
		t.Fatalf("Template returned partial output %q", out)
	}
	var terr *transform.Error
	if !errors.As(err, &terr) || terr.Stage != "run" || terr.ExitCode() != 2 {
		t.Fatalf("Template classified as %#v", err)
	}
}

func TestErrorExitCode(t *testing.T) {
	if got := (&transform.Error{Stage: "parse"}).ExitCode(); got != 1 {
		t.Fatalf("parse ExitCode = %d, want 1", got)
	}
	if got := (&transform.Error{Stage: "run"}).ExitCode(); got != 2 {
		t.Fatalf("run ExitCode = %d, want 2", got)
	}
}
