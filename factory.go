package rungrad

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/vincentsch/rungrad/config"
	"github.com/vincentsch/rungrad/output"
	"github.com/vincentsch/rungrad/redact"
	"github.com/vincentsch/rungrad/resolve"
	"github.com/vincentsch/rungrad/transform"
)

// Pager runs a pager process over already-rendered human output. Implementations
// should return an error only when they did not display the content; writeResolved
// falls back to direct stdout on any pager error.
type Pager interface {
	Run(args []string, content io.Reader, stdout, stderr io.Writer) error
}

// PagerFunc adapts a function to Pager.
type PagerFunc func(args []string, content io.Reader, stdout, stderr io.Writer) error

// Run calls fn with the supplied pager command and streams.
func (fn PagerFunc) Run(args []string, content io.Reader, stdout, stderr io.Writer) error {
	return fn(args, content, stdout, stderr)
}

// Factory carries the dependencies a command needs to run: the resolved global
// flags, the output writers, the config store, and the credential loaded by the
// auth pre-run hook. Commands receive a *Factory and route all output through it
// so dual rendering and determinism are guaranteed in one place.
type Factory struct {
	Flags  *GlobalFlags
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Store  config.Store
	// Token is the credential resolved by the auth pre-run hook for commands that
	// require authentication. It is empty for commands that do not.
	Token string

	// PromptTerminal overrides terminal detection for tests and embedders. When
	// PromptTerminalSet is false, Factory.Resolve uses real process TTY detection.
	PromptTerminal    bool
	PromptTerminalSet bool

	// OutputTerminal overrides stdout terminal detection for human color styling
	// (tests and embedders). When OutputTerminalSet is false, TerminalMode detects
	// from the active Stdout writer. This is independent of PromptTerminal, which
	// governs stdin/stderr prompting.
	OutputTerminal    bool
	OutputTerminalSet bool

	// LookupEnv, TerminalHeight, Pager, BrowserOpener, and UserConfigDir are
	// injectable hooks for tests and embedders. Nil hooks use process/global
	// defaults.
	LookupEnv      func(string) (string, bool)
	TerminalHeight func() (int, bool)
	Pager          Pager
	PagerEnvVar    string
	BrowserOpener  func(ctx context.Context, url string) error
	UserConfigDir  func() (string, error)

	// Set by App.guardOutputMode for the current run. These are private so
	// commands cannot bypass capability checks by enabling transforms directly.
	jqActive   bool
	jqExpr     string
	tmplActive bool
	tmplText   string
	// metaActive means the guard has accepted --include-meta for this command and
	// output mode. Writers check this private state instead of the raw flag so an
	// invalid invocation can never wrap output by accident.
	metaActive bool
	// storeReady is set when preRunValidateThenAuth assigns Store, marking the
	// point after which resolved profile/config/auth-file/service values are
	// meaningful. ErrorContext uses it to keep earlier failure stages empty.
	storeReady bool
	// advancedOutput is set by App.New. Paging is part of the opt-in advanced
	// output contract, so compact apps never invoke a pager.
	advancedOutput bool
	// defaultProfile is AppConfig.Profile for apps that do not enable runtime
	// resolution.
	defaultProfile string
	// redactor is the per-run secret registry. Commands register values through
	// RegisterSecret; App.resetForRun clears it before each command execution.
	redactor *redact.Registry
	// resolved/resolvedSet hold the per-run profile/service/path resolution.
	resolved    config.Resolved
	resolvedSet bool
	// credential holds the resolved runtime credential for RequiresAuth commands.
	credential Credential
}

// Output describes a command result for the advanced dispatch. Model is the
// stable machine value used for --json, --jq, and --template; it must be non-nil
// for any command that declares json/jq/template modes. Human renders the human
// view; it reads f.TerminalMode() for styling. Plain renders the explicit
// unstyled, copy-safe view for commands that declare OutputModePlain. Meta is
// wrapped as {data, meta} under --include-meta and ignored otherwise.
type Output struct {
	Model any
	Meta  output.Meta
	Human func(w io.Writer)
	Plain func(w io.Writer)
}

// WriteOutput renders a result with an explicit plain renderer for advanced
// output commands.
func (f *Factory) WriteOutput(o Output) error {
	return f.writeResolved(
		func() ([]byte, error) { return output.StableJSON(f.wrapMeta(o.Model, o.Meta)) },
		o.Human,
		o.Plain,
	)
}

// WriteResultWithMeta renders model like WriteResult and, when --include-meta is
// active, wraps the machine value as a {data, meta} envelope. Declare
// Command.SupportsMeta so the guard accepts --include-meta. Human output is the
// supplied renderer and is never wrapped; transforms (--jq/--template) see the
// envelope when --include-meta is active and the plain model otherwise.
func (f *Factory) WriteResultWithMeta(model any, meta output.Meta, human func(w io.Writer)) error {
	return f.writeResolved(
		func() ([]byte, error) { return output.StableJSON(f.wrapMeta(model, meta)) },
		human,
		nil,
	)
}

// WriteResult renders a single result model as stable JSON (under --json) or via
// the supplied human renderer. In advanced-output apps it also serves --jq and
// --template from the same model.
func (f *Factory) WriteResult(model any, human func(w io.Writer)) error {
	return f.writeResolved(
		func() ([]byte, error) { return output.StableJSON(f.wrapMeta(model, output.Meta{})) },
		human,
		nil,
	)
}

// wrapMeta wraps model in a {data, meta} envelope when --include-meta is active
// for this run, and returns model unchanged otherwise. metaActive is set only for
// commands that declared SupportsMeta, so non-metadata commands always pass model
// through and observe no change.
func (f *Factory) wrapMeta(model any, meta output.Meta) any {
	if f.metaActive {
		return output.Envelope{Data: model, Meta: meta}
	}
	return model
}

// WritePreview renders a dry-run preview in the active output mode.
func (f *Factory) WritePreview(p output.DryRunPreview) error {
	return f.writeResolved(
		func() ([]byte, error) { return p.JSON() },
		func(w io.Writer) { p.RenderMode(w, f.TerminalMode()) },
		func(w io.Writer) { p.Render(w) },
	)
}

// writeResolved is the single result-output dispatcher. Transform modes run
// first because they consume the same stable JSON bytes as --json; plain output
// requires an explicit renderer so commands cannot accidentally reuse styled
// human text as copy-safe output.
func (f *Factory) writeResolved(machine func() ([]byte, error), human, plain func(io.Writer)) error {
	if f.jqActive {
		b, err := machine()
		if err != nil {
			return err
		}
		out, err := transform.JQ(context.Background(), b, f.jqExpr)
		if err != nil {
			return err
		}
		out = f.redactJSON(out)
		out = output.SanitizeControlBytes(out)
		_, err = f.Stdout.Write(out)
		return err
	}
	if f.tmplActive {
		b, err := machine()
		if err != nil {
			return err
		}
		out, err := transform.Template(b, f.tmplText)
		if err != nil {
			return err
		}
		out = f.redactText(out)
		out = output.SanitizeControlBytes(out)
		_, err = f.Stdout.Write(out)
		return err
	}
	if f.Flags != nil && f.Flags.JSON {
		b, err := machine()
		if err != nil {
			return err
		}
		_, err = f.Stdout.Write(f.redactJSON(b))
		return err
	}
	if f.Flags != nil && f.Flags.Plain {
		if plain == nil {
			return NewError(ExitAPI, "plain output renderer is not configured")
		}
		var buf bytes.Buffer
		plain(&buf)
		out := f.redactText(buf.Bytes())
		out = output.SanitizeControlBytes(out)
		_, err := f.Stdout.Write(out)
		return err
	}
	rendered := output.RenderHumanWith(f.TerminalMode(), human, f.redactText)
	if len(rendered) == 0 {
		return nil
	}
	if pagerArgs, ok := f.pageArgs(rendered); ok {
		if err := f.runPager(pagerArgs, bytes.NewReader(rendered)); err == nil {
			return nil
		}
	}
	_, err := f.Stdout.Write(rendered)
	return err
}

// RegisterSecret records a secret value so the framework redacts it from every
// framework-owned output boundary for the rest of the run. Empty,
// whitespace-only, and too-short values are ignored.
func (f *Factory) RegisterSecret(value string) {
	if f.redactor == nil {
		f.redactor = redact.NewRegistry()
	}
	f.redactor.Add(value)
}

// redactText is for free-text boundaries where raw replacement cannot corrupt a
// structured document: human output, plain output, templates, prompts, and text
// errors.
func (f *Factory) redactText(b []byte) []byte {
	if f.redactor == nil {
		return b
	}
	return f.redactor.RedactBytes(b)
}

// redactJSON is for machine JSON boundaries. It rewrites only JSON string
// literal contents so encoded output remains parseable.
func (f *Factory) redactJSON(b []byte) []byte {
	if f.redactor == nil {
		return b
	}
	return f.redactor.RedactJSON(b)
}

// redactString applies text redaction to a string before direct stderr writes.
func (f *Factory) redactString(s string) string {
	if f.redactor == nil {
		return s
	}
	return f.redactor.RedactString(s)
}

// resetSecrets clears per-run registrations before auth and handlers discover
// the current run's secrets.
func (f *Factory) resetSecrets() {
	if f.redactor == nil {
		f.redactor = redact.NewRegistry()
		return
	}
	f.redactor.Reset()
}

// machineOutput reports modes intended for scripts or agents. --plain is not
// included: it is unstyled human text, so prompts and Infof can still be useful.
func (f *Factory) machineOutput() bool {
	return f.jqActive || f.tmplActive || (f.Flags != nil && f.Flags.JSON)
}

// TerminalMode chooses the human-output styling mode for the active run. It is
// plain under machine output, --plain, and non-terminal stdout, honors the
// OutputTerminalSet override, and otherwise detects from the active Stdout
// writer using the standard library only. It is safe on a bare Factory.
func (f *Factory) TerminalMode() output.TerminalMode {
	if f.machineOutput() || (f.Flags != nil && f.Flags.Plain) {
		// Machine output and explicit plain output stay escape-free even when a
		// test or embedder forces terminal styling on.
		return output.TerminalMode{}
	}
	if f.Flags != nil && f.Flags.NoANSI {
		return output.TerminalMode{Sanitize: true}
	}
	if !f.stdoutTerminal() {
		return output.TerminalMode{Sanitize: true}
	}
	color := true
	if f.Flags != nil && f.Flags.NoColor {
		color = false
	}
	return output.TerminalMode{ANSI: true, Color: color}
}

// lookupEnv centralizes env lookup so pager policy can be tested without
// mutating process-global environment state.
func (f *Factory) lookupEnv(name string) (string, bool) {
	if f.LookupEnv != nil {
		return f.LookupEnv(name)
	}
	return os.LookupEnv(name)
}

func (f *Factory) userConfigDir() (string, error) {
	if f.UserConfigDir != nil {
		return f.UserConfigDir()
	}
	return os.UserConfigDir()
}

func (f *Factory) setResolved(r config.Resolved) {
	f.resolved = r
	f.resolvedSet = true
}

func (f *Factory) setCredential(c Credential) {
	f.credential = c
}

// Resolved returns the resolution result for the current run.
func (f *Factory) Resolved() (config.Resolved, bool) {
	if f == nil || !f.resolvedSet {
		return config.Resolved{}, false
	}
	return f.resolved, true
}

// Profile returns the resolved active profile.
func (f *Factory) Profile() string {
	if f == nil {
		return ""
	}
	if f.resolvedSet {
		return f.resolved.Profile
	}
	return f.defaultProfile
}

// ConfigPath returns the resolved config path.
func (f *Factory) ConfigPath() string {
	if f == nil {
		return ""
	}
	if f.resolvedSet {
		return f.resolved.ConfigPath
	}
	path, err := f.Store.ConfigPath()
	if err != nil {
		return ""
	}
	return path
}

// AuthFilePath returns the resolved credentials file path.
func (f *Factory) AuthFilePath() string {
	if f == nil {
		return ""
	}
	if f.resolvedSet {
		return f.resolved.AuthFilePath
	}
	path, err := f.Store.CredentialsPath()
	if err != nil {
		return ""
	}
	return path
}

// Service returns a resolved service endpoint by name.
func (f *Factory) Service(name string) (config.ResolvedService, bool) {
	if f == nil || !f.resolvedSet || f.resolved.Services == nil {
		return config.ResolvedService{}, false
	}
	svc, ok := f.resolved.Services[name]
	return svc, ok
}

// Credential returns the runtime credential resolved for this command.
func (f *Factory) Credential() Credential {
	if f == nil {
		return Credential{}
	}
	return f.credential
}

// stdoutTerminal reports whether human stdout should be treated as a terminal.
// It preserves the explicit test/embedder override before falling back to the
// active writer's file mode.
func (f *Factory) stdoutTerminal() bool {
	if f.OutputTerminalSet {
		return f.OutputTerminal
	}
	return outputIsTerminal(f.Stdout)
}

// terminalHeight reports the height used for pager threshold decisions. An
// injected hook wins; otherwise LINES is the only environment fallback.
func (f *Factory) terminalHeight() (int, bool) {
	if f.TerminalHeight != nil {
		return f.TerminalHeight()
	}
	if v, ok := f.lookupEnv("LINES"); ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
}

// pagerCommand resolves the command used to display long human output. Blank
// pager env vars are treated as an explicit "no pager" choice, while an entirely
// absent pager env falls back to less on non-Windows systems.
func (f *Factory) pagerCommand() []string {
	anySet := false
	if f.PagerEnvVar != "" {
		if v, ok := f.lookupEnv(f.PagerEnvVar); ok {
			anySet = true
			if fields := strings.Fields(v); len(fields) > 0 {
				return fields
			}
		}
	}
	if v, ok := f.lookupEnv("PAGER"); ok {
		anySet = true
		if fields := strings.Fields(v); len(fields) > 0 {
			return fields
		}
	}
	if anySet {
		return nil
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	return []string{"less", "-FRX"}
}

// runPager executes the selected pager. The real pager ignores wait errors after
// a successful start so a pager that exits non-zero does not cause rungrad to
// print the same buffer a second time.
func (f *Factory) runPager(args []string, content io.Reader) error {
	if len(args) == 0 {
		return fmt.Errorf("pager command is empty")
	}
	if f.Pager != nil {
		return f.Pager.Run(args, content, f.Stdout, f.Stderr)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = content
	cmd.Stdout = f.Stdout
	cmd.Stderr = f.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Wait()
	return nil
}

// shouldPage reports only the page/no-page decision. pageArgs is used by the
// writer path so command selection happens exactly once per rendered output.
func (f *Factory) shouldPage(content []byte) bool {
	_, ok := f.pageArgs(content)
	return ok
}

// pageArgs applies the pager policy and returns the already-resolved command
// when paging should happen. The advanced-output check stays first so compact
// apps never consult pager env vars or reach the real pager path.
func (f *Factory) pageArgs(content []byte) ([]string, bool) {
	if !f.advancedOutput {
		return nil, false
	}
	if !f.stdoutTerminal() || (f.Flags != nil && (f.Flags.NoPager || f.Flags.NoANSI)) {
		return nil, false
	}
	args := f.pagerCommand()
	if len(args) == 0 {
		return nil, false
	}
	threshold := 40
	if h, ok := f.terminalHeight(); ok && h > 0 {
		threshold = h
	}
	return args, lineCount(content) > threshold
}

// lineCount counts display lines in already-rendered text. A final newline ends
// the current line rather than creating an extra blank one.
func lineCount(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	n := bytes.Count(content, []byte{'\n'})
	if content[len(content)-1] != '\n' {
		n++
	}
	return n
}

// outputIsTerminal reports whether w is a character device (a real terminal).
// A non-*os.File writer, including nil and *bytes.Buffer, is treated as
// non-terminal. This mirrors resolve.isTerminal without exporting that helper or
// adding a dependency.
func outputIsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok || f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// DryRun reports whether --dry-run is set.
func (f *Factory) DryRun() bool { return f.Flags != nil && f.Flags.DryRun }

// ConfirmOptions describes a destructive action awaiting confirmation. Action and
// Target are human-readable text for the interactive prompt (for example
// "delete item" and the resolved id); Confirmed reports whether the command's
// explicit confirmation flag (such as --confirm) was set.
type ConfirmOptions struct {
	Action    string
	Target    string
	Confirmed bool
}

// ConfirmDestructive gates a destructive action behind confirmation, using the
// same prompt gate as Resolve so behavior is consistent across the framework. It
// is safe in every execution mode:
//
//   - under --dry-run it returns nil without prompting and without requiring the
//     confirmation flag, so it is safe regardless of where a command calls it;
//   - when Confirmed is set it returns nil without prompting;
//   - in non-interactive mode (machine output, --no-prompt, or no terminal) it
//     never reads stdin and returns a usage error, so an automated caller can
//     never block;
//   - otherwise it writes a prompt to stderr (not through Infof, so --quiet cannot
//     hide a blocking prompt) and proceeds only on a case-insensitive y or yes.
//
// Decline, empty, EOF, or an unrecognized response returns a usage error.
func (f *Factory) ConfirmDestructive(opts ConfirmOptions) error {
	if f.DryRun() {
		return nil
	}
	if opts.Confirmed {
		return nil
	}
	noPrompt := f.Flags != nil && f.Flags.NoPrompt
	if f.machineOutput() || noPrompt || !f.promptInteractive() {
		return NewError(ExitUsage, "destructive action requires --confirm")
	}
	fmt.Fprint(f.Stderr, f.redactString(fmt.Sprintf("About to %s %s. Confirm? [y/N]: ", opts.Action, opts.Target)))
	line, _ := bufio.NewReader(f.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	default:
		return NewError(ExitUsage, "destructive action declined")
	}
}

// Resolve resolves a name to an identifier using the tool's lookup, wiring the
// interactive/non-interactive decision from the global flags automatically.
func (f *Factory) Resolve(input string, lookup resolve.Lookup, opts resolve.Options) (string, error) {
	if f.Flags != nil {
		// Machine output or --no-prompt callers must never block: disallow prompting.
		opts.AllowPrompt = opts.AllowPrompt && !f.Flags.NoPrompt && !f.machineOutput() && f.promptInteractive()
	} else {
		opts.AllowPrompt = opts.AllowPrompt && !f.machineOutput()
	}
	if opts.AllowPrompt && opts.Prompt == nil {
		opts.Prompt = resolve.StdPrompter{In: f.Stdin, Out: f.Stderr, Transform: f.redactText}
	}
	return resolve.Resolve(input, lookup, opts)
}

func (f *Factory) promptInteractive() bool {
	if f.PromptTerminalSet {
		return f.PromptTerminal
	}
	noPrompt := false
	if f.Flags != nil {
		noPrompt = f.Flags.NoPrompt
	}
	return resolve.IsInteractive(noPrompt)
}

// Infof writes a non-essential informational line to stderr unless --quiet or a
// machine output mode is set. Use it for hints and progress, never for the
// primary result.
func (f *Factory) Infof(format string, args ...any) {
	if f.machineOutput() || (f.Flags != nil && f.Flags.Quiet) {
		return
	}
	fmt.Fprint(f.Stderr, f.redactString(fmt.Sprintf(format+"\n", args...)))
}
