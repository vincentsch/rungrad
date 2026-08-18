// Package transform runs machine-output transforms (jq, Go templates) over the
// exact stable JSON bytes a rungrad command would emit under --json. Results are
// fully buffered, so a failure mid-evaluation leaves stdout untouched.
package transform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/itchyny/gojq"
)

const (
	modeJQ       = "jq"
	modeTemplate = "template"

	stageParse = "parse"
	stageRun   = "run"
)

// Error is a jq/template parse or execution failure. Stage is "parse" for an
// invalid expression/template and "run" for a failure executing on the data.
// ExitCode encodes that split so the rungrad root classifier maps it without
// parsing text.
type Error struct {
	Mode  string
	Stage string
	Err   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	switch {
	case e.Mode == modeJQ && e.Stage == stageParse:
		return fmt.Sprintf("invalid --jq expression: %v", e.Err)
	case e.Mode == modeJQ:
		return fmt.Sprintf("--jq expression failed: %v", e.Err)
	case e.Mode == modeTemplate && e.Stage == stageParse:
		return fmt.Sprintf("invalid --template: %v", e.Err)
	case e.Mode == modeTemplate:
		return fmt.Sprintf("--template rendering failed: %v", e.Err)
	default:
		if e.Err != nil {
			return e.Err.Error()
		}
		return "transform error"
	}
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ExitCode maps parse errors to usage (1) and execution errors to runtime/API
// failure (2) without importing the root rungrad package.
func (e *Error) ExitCode() int {
	if e != nil && e.Stage == stageParse {
		return 1
	}
	return 2
}

// ValidateJQ parses and compiles expr. Empty expr is the identity expression.
func ValidateJQ(expr string) error {
	_, err := compileJQ(expr)
	return err
}

// ValidateTemplate parses text with the same options used for execution.
func ValidateTemplate(text string) error {
	_, err := parseTemplate(text)
	return err
}

// JQ applies expr to stableJSON and returns compact, newline-delimited JSON
// results. Empty expr is the identity expression.
func JQ(ctx context.Context, stableJSON []byte, expr string) ([]byte, error) {
	value, err := decodeStableJSON(stableJSON, modeJQ)
	if err != nil {
		return nil, err
	}
	code, err := compileJQ(expr)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	iter := code.RunWithContext(ctx, value)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return nil, &Error{Mode: modeJQ, Stage: stageRun, Err: err}
		}
		b, err := gojq.Marshal(v)
		if err != nil {
			return nil, &Error{Mode: modeJQ, Stage: stageRun, Err: err}
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// Template renders stableJSON through a Go text/template and normalizes output
// to exactly one final newline.
func Template(stableJSON []byte, text string) ([]byte, error) {
	value, err := decodeStableJSON(stableJSON, modeTemplate)
	if err != nil {
		return nil, err
	}
	tmpl, err := parseTemplate(text)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, value); err != nil {
		return nil, &Error{Mode: modeTemplate, Stage: stageRun, Err: err}
	}
	out := bytes.TrimRight(buf.Bytes(), "\n")
	out = append(out, '\n')
	return out, nil
}

// decodeStableJSON decodes the exact bytes that --json would write. UseNumber
// keeps large integers intact instead of rounding them through float64 before a
// jq expression or template sees the value.
func decodeStableJSON(stableJSON []byte, mode string) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(stableJSON))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, &Error{Mode: mode, Stage: stageRun, Err: err}
	}
	// StableJSON should contain one top-level JSON value; treat any trailing
	// non-whitespace bytes as execution-time invalid input.
	if rest := strings.TrimSpace(string(stableJSON[dec.InputOffset():])); rest != "" {
		return nil, &Error{Mode: mode, Stage: stageRun, Err: fmt.Errorf("invalid JSON after top-level value")}
	}
	return value, nil
}

func compileJQ(expr string) (*gojq.Code, error) {
	if strings.TrimSpace(expr) == "" {
		expr = "."
	}
	query, err := gojq.Parse(expr)
	if err != nil {
		return nil, &Error{Mode: modeJQ, Stage: stageParse, Err: err}
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return nil, &Error{Mode: modeJQ, Stage: stageParse, Err: err}
	}
	return code, nil
}

func parseTemplate(text string) (*template.Template, error) {
	tmpl, err := template.New("output").Option("missingkey=error").Parse(text)
	if err != nil {
		return nil, &Error{Mode: modeTemplate, Stage: stageParse, Err: err}
	}
	return tmpl, nil
}
