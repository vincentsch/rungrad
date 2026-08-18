package rungrad

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vincentsch/rungrad/config"
	"github.com/vincentsch/rungrad/output"
)

// ErrorPolicy lets a host CLI own error rendering and exit classification while
// keeping rungrad's execution, validation, auth/config, redaction, and
// output-mode lifecycle. A nil field falls back to the rungrad default for that
// concern.
type ErrorPolicy struct {
	// Classify maps the underlying error and rungrad's default exit code to the
	// process exit code. A zero or negative return for a non-nil error is ignored
	// and the default code is used. Nil uses rungrad's classifier.
	Classify func(ErrorContext) int
	// Render writes the stderr representation, or deliberately nothing. Return
	// nil to mark the error fully rendered; return non-nil to discard host bytes
	// and fall back to the default renderer. Nil uses rungrad's renderer.
	Render func(ErrorContext) error
}

// ErrorContext is the read-only view handed to ErrorPolicy hooks. It is passed
// by value and its reference fields are cloned per hook call, so hook mutations
// cannot change the next hook or the caller's argv. It deliberately omits the
// *Factory: host renderers write only through Stderr and read only the narrow
// resolved fields below.
type ErrorContext struct {
	Err             error
	DefaultExitCode int
	ExitCode        int
	Command         *cobra.Command
	CommandPath     string
	// Args is the raw argv passed to RunIO, including command tokens and flags.
	// It is not Cobra's resolved positional args.
	Args              []string
	Stderr            io.Writer
	Flags             GlobalFlags
	MachineOutput     bool
	Profile           string
	ConfigPath        string
	AuthFilePath      string
	Services          map[string]string
	CredentialSource  string
	CredentialDisplay string
	RedactString      func(string) string
	RedactText        func([]byte) []byte
	RedactJSON        func([]byte) []byte
}

// machineFlagNames holds the long visible names bound to the canonical
// machine-output flags. Empty fields default to the framework literals.
type machineFlagNames struct {
	JSON     string
	JQ       string
	Template string
}

// machineFlagNames is the only source the raw-argument machine-intent detector
// reads. Active rungrad-owned apps resolve zero fields to the framework
// literals; host-owned apps populate visible long names through BindGlobalFlags.
func (a *App) machineFlagNames() machineFlagNames {
	m := a.machineFlags
	if m.JSON == "" {
		m.JSON = "json"
	}
	if m.JQ == "" {
		m.JQ = "jq"
	}
	if m.Template == "" {
		m.Template = "template"
	}
	return m
}

// detectMachineOutput reports machine-output intent by scanning raw args for
// early error paths where parsed global flags may be unavailable or unreliable.
// It mirrors Factory.machineOutput: the canonical json flag always counts;
// jq/template count only when advanced output is enabled. It respects the "--"
// terminator, explicit false boolean forms (--json=false), and skips the value
// of root-level value-taking long or shorthand flags so a value that spells a
// canonical machine flag, such as "--config --json" or "-c --json", is not
// misread as intent.
func (a *App) detectMachineOutput(args []string) bool {
	if !a.machineFlagsActive {
		return false
	}
	names := a.machineFlagNames()
	pf := a.root.PersistentFlags()
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			// We do not treat short flags as machine-output flags, but a short
			// value flag can consume the next token. Skip that value so
			// "-c --json" is not mistaken for JSON mode.
			if shorthandValueConsumesNext(pf, arg) {
				skipNext = true
			}
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		body := arg[2:]
		name, value, hasValue := body, "", false
		if eq := strings.IndexByte(body, '='); eq >= 0 {
			name, value, hasValue = body[:eq], body[eq+1:], true
		}
		switch name {
		case names.JSON:
			if !(hasValue && isFalseBool(value)) {
				return true
			}
		case names.JQ, names.Template:
			if a.advancedOutput {
				return true
			}
		}
		if !hasValue {
			if f := pf.Lookup(name); f != nil && f.NoOptDefVal == "" {
				skipNext = true
			}
		}
	}
	return false
}

// shorthandValueConsumesNext mirrors the part of pflag shorthand parsing that
// matters for raw-arg machine detection: a value-taking shorthand consumes the
// following argv token only when its value is not attached in the same token.
func shorthandValueConsumesNext(fs *pflag.FlagSet, arg string) bool {
	if fs == nil || len(arg) < 2 || arg[0] != '-' || arg[1] == '-' {
		return false
	}
	body := arg[1:]
	for i := 0; i < len(body); i++ {
		if body[i] == '=' {
			return false
		}
		f := fs.ShorthandLookup(string(body[i]))
		if f == nil {
			return false
		}
		if f.NoOptDefVal == "" {
			return i == len(body)-1
		}
	}
	return false
}

// isFalseBool reports whether v is an explicit boolean false in pflag's
// accepted forms. Non-boolean values are not "false".
func isFalseBool(v string) bool {
	b, err := strconv.ParseBool(v)
	return err == nil && !b
}

// machineOutputForError chooses the source of truth for error rendering mode.
// Before Cobra has reliable parsed flags, raw argv is the only way to honor
// machine-output intent. After parsing succeeds, parsed global flags must win so
// a command-local value such as `--name --json` is treated as a value, not as the
// global JSON flag.
func (a *App) machineOutputForError(args []string, err error) bool {
	if isCommandResolutionError(err) || isFlagParseError(err) {
		return a.detectMachineOutput(args)
	}
	return a.parsedMachineOutput()
}

// isCommandResolutionError recognizes Cobra's bare unknown-command failures.
// Post-parse Args validators can produce the same user-facing text, so wrapped
// usage errors are intentionally excluded and use parsed flags instead.
func isCommandResolutionError(err error) bool {
	if !isUsageError(err) {
		return false
	}
	var usage *usageError
	return !errors.As(err, &usage)
}

// isFlagParseError identifies failures from Cobra's flag parser. Those errors
// can happen before parsed global flags reflect the user's full intent.
func isFlagParseError(err error) bool {
	var parseErr *flagParseError
	return errors.As(err, &parseErr)
}

// parsedMachineOutput mirrors Factory.machineOutput without consulting the
// Factory's guarded per-run state. Error rendering needs the user's parsed flag
// intent even when the command fails before output writers run.
func (a *App) parsedMachineOutput() bool {
	if a.flags == nil {
		return false
	}
	if !a.machineFlagsActive {
		return false
	}
	if a.flags.JSON {
		return true
	}
	if !a.advancedOutput {
		return false
	}
	return a.globalChanged("jq") || a.globalChanged("template")
}

// buildErrorContext captures everything hooks may read. It performs the default
// classification once; handleError may replace ExitCode later with a positive
// host override, but DefaultExitCode remains the framework answer.
func (a *App) buildErrorContext(args []string, cmd *cobra.Command, err error, staged io.Writer) ErrorContext {
	ctx := ErrorContext{
		Err:             err,
		DefaultExitCode: classifyExit(err),
		Command:         cmd,
		Args:            args,
		Stderr:          staged,
		Flags:           *a.flags,
		MachineOutput:   a.machineOutputForError(args, err),
		RedactString:    a.factory.redactString,
		RedactText:      a.factory.redactText,
		RedactJSON:      a.factory.redactJSON,
	}
	ctx.ExitCode = ctx.DefaultExitCode
	if cmd != nil {
		ctx.CommandPath = cmd.CommandPath()
	}
	if a.factory.storeReady {
		// Resolved fields are meaningful only after pre-run resolution has
		// assigned Store. Earlier failures keep these fields empty on purpose.
		ctx.Profile = a.factory.Profile()
		ctx.ConfigPath = a.factory.ConfigPath()
		ctx.AuthFilePath = a.factory.AuthFilePath()
		ctx.Services = errorServices(a.factory)
		ctx.CredentialSource = a.factory.credential.Source
		if tok := a.factory.credential.Token; tok != "" {
			ctx.CredentialDisplay = config.Mask(tok)
		}
	}
	return ctx
}

// errorServices projects resolved service endpoints to a value-only map for the
// read-only context. Compact apps with no service resolution yield nil.
func errorServices(f *Factory) map[string]string {
	if !f.resolvedSet || f.resolved.Services == nil {
		return nil
	}
	out := make(map[string]string, len(f.resolved.Services))
	for name, svc := range f.resolved.Services {
		out[name] = svc.Value
	}
	return out
}

// cloneRefs gives each hook its own mutable copy of reference fields while
// preserving the shared Stderr writer used for staged rendering.
func (ctx ErrorContext) cloneRefs() ErrorContext {
	ctx.Args = append([]string(nil), ctx.Args...)
	ctx.Services = cloneStringMap(ctx.Services)
	return ctx
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// handleError is the single error exit path for RunIO. It stages host-rendered
// bytes so a failed renderer cannot leak partial output before rungrad falls
// back to its default error format.
func (a *App) handleError(args []string, cmd *cobra.Command, err error, realStderr io.Writer) int {
	var staged bytes.Buffer
	ctx := a.buildErrorContext(args, cmd, err, &staged)

	code := ctx.DefaultExitCode
	if p := a.cfg.ErrorPolicy; p != nil && p.Classify != nil {
		if c := p.Classify(ctx.cloneRefs()); c > 0 {
			code = c
		}
	}
	ctx.ExitCode = code

	if p := a.cfg.ErrorPolicy; p != nil && p.Render != nil {
		if rErr := p.Render(ctx.cloneRefs()); rErr == nil {
			_, _ = realStderr.Write(staged.Bytes())
			return code
		} else {
			// Discard staged host bytes on failure; the fallback should be the
			// only stderr representation the caller sees.
			a.defaultRenderError(realStderr, ctx, rErr)
			return code
		}
	}
	a.defaultRenderError(realStderr, ctx, nil)
	return code
}

// defaultRenderError is rungrad's built-in renderer. rendererErr is non-nil
// only when a host Render failed; it adds a redacted renderer-failure detail
// without changing classification. Machine output must always remain one
// parseable JSON object, so a detail-encoding failure retries with the minimal
// safe envelope instead of falling through to text.
func (a *App) defaultRenderError(w io.Writer, ctx ErrorContext, rendererErr error) {
	if ctx.MachineOutput {
		body := map[string]any{"error": ctx.Err.Error(), "exit_code": ctx.ExitCode}
		var detailer errorDetailer
		if errors.As(ctx.Err, &detailer) {
			// Preserve the old JSON error-detail shape for framework errors that
			// expose structured details.
			switch details := detailer.ErrorDetails().(type) {
			case map[string]any:
				for k, v := range details {
					body[k] = v
				}
			default:
				body["details"] = details
			}
		}
		if rendererErr != nil {
			body["renderer_error"] = rendererErr.Error()
		}
		if b, mErr := output.StableJSON(body); mErr == nil {
			_, _ = w.Write(a.factory.redactJSON(b))
			return
		}
		// If details cannot be encoded, retry without them so machine-mode stderr
		// is still a parseable object rather than a text fallback.
		fallback := map[string]any{"error": ctx.Err.Error(), "exit_code": ctx.ExitCode}
		if rendererErr != nil {
			fallback["renderer_error"] = rendererErr.Error()
		}
		if b, mErr := output.StableJSON(fallback); mErr == nil {
			_, _ = w.Write(a.factory.redactJSON(b))
			return
		}
		_, _ = w.Write(a.factory.redactJSON([]byte(
			`{"error":"error rendering failed","exit_code":` + strconv.Itoa(ctx.ExitCode) + "}\n",
		)))
		return
	}
	fmt.Fprintln(w, "Error:", a.factory.redactString(ctx.Err.Error()))
	if rendererErr != nil {
		fmt.Fprintln(w, "Error:", a.factory.redactString("error renderer failed: "+rendererErr.Error()))
	}
}
