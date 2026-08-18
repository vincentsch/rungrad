package output

import (
	"fmt"
	"io"
	"strings"
)

// maskedValue is what a secret field serializes to. Real secret values never
// reach stdout or a JSON body through a DryRunPreview.
const maskedValue = "***"

// Field is one named value in a previewed request. Setting Secret masks the
// value in every rendering, so a dry run can show the shape of a write without
// leaking credentials or tokens.
type Field struct {
	Name   string
	Value  string
	Secret bool
}

func (f Field) display() string {
	if f.Secret {
		return maskedValue
	}
	return f.Value
}

// DryRunPreview describes a write that would happen, without performing it. A
// command builds one of these under --dry-run and the framework renders it,
// then returns without contacting any backend.
type DryRunPreview struct {
	Method      string
	Path        string
	Query       []Field
	Body        []Field
	Idempotency string
}

// previewJSON is the masked, serializable projection of a DryRunPreview.
type previewJSON struct {
	DryRun      bool              `json:"dry_run"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Query       map[string]string `json:"query,omitempty"`
	Body        map[string]string `json:"body,omitempty"`
	Idempotency string            `json:"idempotency,omitempty"`
}

func (p DryRunPreview) marshalShape() previewJSON {
	out := previewJSON{DryRun: true, Method: p.Method, Path: p.Path, Idempotency: p.Idempotency}
	if len(p.Query) > 0 {
		out.Query = make(map[string]string, len(p.Query))
		for _, f := range p.Query {
			out.Query[f.Name] = f.display()
		}
	}
	if len(p.Body) > 0 {
		out.Body = make(map[string]string, len(p.Body))
		for _, f := range p.Body {
			out.Body[f.Name] = f.display()
		}
	}
	return out
}

// MarshalJSON masks secret field values before encoding.
func (p DryRunPreview) MarshalJSON() ([]byte, error) {
	return StableJSON(p.marshalShape())
}

// JSON returns the stable, secret-masked JSON form of the preview.
func (p DryRunPreview) JSON() ([]byte, error) {
	return StableJSON(p.marshalShape())
}

// Render writes a plain human view of the preview, masking secret values.
func (p DryRunPreview) Render(w io.Writer) { p.RenderMode(w, TerminalMode{}) }

// RenderMode writes a human view of the preview in the given terminal mode,
// masking secret values. Only the DRY RUN label is colored, and only when the
// mode is styled; the colon and everything after it stay plain.
func (p DryRunPreview) RenderMode(w io.Writer, mode TerminalMode) {
	label := "DRY RUN"
	if mode.styled() {
		label = ansiDryRun + label + ansiReset
	}
	fmt.Fprintf(w, "%s: would %s %s\n", label, strings.ToUpper(p.Method), p.Path)
	if len(p.Query) > 0 {
		fmt.Fprintln(w, "  query:")
		for _, f := range p.Query {
			fmt.Fprintf(w, "    %s = %s\n", f.Name, f.display())
		}
	}
	if len(p.Body) > 0 {
		fmt.Fprintln(w, "  body:")
		for _, f := range p.Body {
			fmt.Fprintf(w, "    %s = %s\n", f.Name, f.display())
		}
	}
	if p.Idempotency != "" {
		fmt.Fprintf(w, "  idempotency: %s\n", p.Idempotency)
	}
	fmt.Fprintln(w, "  no changes were made")
}
