package conformance

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vincentsch/rungrad/manifest"
)

// Target describes the executable to score and which of its commands exercise
// each probe. Command fields that are nil make their probes not-applicable, so a
// tool is never penalized for a capability it does not have.
type Target struct {
	Path string

	// Read is a read-only command that lists or shows data, e.g. ["item","list"].
	Read []string
	// Mutate is a state-changing command, run with --dry-run, e.g.
	// ["item","create","demo"].
	Mutate []string
	// Auth is a command that requires a credential, e.g. ["whoami"].
	Auth []string
	// Ambiguous is a resolution command plus a name that matches more than one
	// resource, e.g. ["item","get","dup"].
	Ambiguous []string
	// NotFound is a command plus an argument that names a missing resource, e.g.
	// ["item","get","ghost"].
	NotFound []string
	// APIError is a command plus an argument that triggers an upstream or runtime
	// error the user did not cause, e.g. ["item","get","broken"].
	APIError []string
	// Forbidden is a command plus an argument refused because the caller is
	// authenticated but not permitted, e.g. ["item","get","forbidden"].
	Forbidden []string
	// RateLimited is a command plus an argument throttled by an upstream service,
	// e.g. ["item","get","throttled"].
	RateLimited []string
	// Destructive is a destructive command exercised only through its dry-run and
	// refused-confirmation paths. The scorer never passes its confirmation flag, so
	// supply a documented safe/stub command, e.g. ["item","delete","alpha"]. Nil
	// makes the destructive probes not-applicable.
	Destructive []string
	// HasUpdate reports whether the target has an `update` command.
	HasUpdate bool
	// Secret is a credential-handling command, run with SecretEnv set to a
	// sentinel, e.g. ["whoami"].
	Secret []string
	// SecretEnv is the environment variable that carries the credential.
	SecretEnv string
	// ManifestMode controls manifest discovery: "" or "auto" (default) tries
	// discovery and falls back to black-box scoring; "off" skips discovery;
	// "required" makes a missing/invalid/unsupported manifest a usage error.
	ManifestMode string
}

// TargetError is a usage error in the score command's target configuration.
type TargetError struct {
	Message string
}

func (e *TargetError) Error() string { return e.Message }

// ExitCode maps target configuration errors to usage.
func (e *TargetError) ExitCode() int { return 1 }

// Runner executes a Target's probes in a controlled environment.
type Runner struct {
	target   Target
	confHome string
	timeout  time.Duration

	// Manifest discovery is explicit and cached. Score reads these fields but
	// does not run the hidden endpoint itself.
	manifestDiscovered bool
	manifestMode       string
	// cachedManifest holds the validated document when manifestSummary.Status is
	// ManifestPresent; probes consume it through manifestActive.
	cachedManifest  *manifest.Manifest
	manifestSummary ManifestSummary
	// manifestConsulted is set by manifestActive when a probe reads the cached
	// manifest. Score resets it per rule and records used rules from it.
	manifestConsulted bool
}

// NewRunner prepares a runner with an empty, isolated config home so the target
// finds no real credentials. Call Close to remove it.
func NewRunner(target Target) (*Runner, error) {
	path, err := resolveTargetPath(target.Path)
	if err != nil {
		return nil, err
	}
	target.Path = path
	dir, err := os.MkdirTemp("", "rungrad-conformance-*")
	if err != nil {
		return nil, err
	}
	return &Runner{target: target, confHome: dir, timeout: 15 * time.Second}, nil
}

// Close removes the runner's temporary config home.
func (r *Runner) Close() error { return os.RemoveAll(r.confHome) }

// Invocation captures the result of running the target once.
type Invocation struct {
	Stdout string
	Stderr string
	Exit   int
	Err    string
	// TimedOut is true when the runner killed the target after its timeout.
	TimedOut bool
}

func resolveTargetPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", &TargetError{Message: "target path is required"}
	}
	if filepath.Base(path) == path {
		found, err := exec.LookPath(path)
		if err != nil {
			return "", &TargetError{Message: "target is not executable: " + path}
		}
		path = found
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", &TargetError{Message: "resolve target path: " + err.Error()}
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", &TargetError{Message: "target is not executable: " + path}
	}
	if info.IsDir() {
		return "", &TargetError{Message: "target is a directory: " + path}
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", &TargetError{Message: "target is not executable: " + path}
	}
	return abs, nil
}

// exec runs the target with args and an optional sentinel secret in the
// environment, in the isolated config home and with empty stdin so a stray
// prompt cannot block.
func (r *Runner) exec(args []string, secretEnv, secretVal string) Invocation {
	return r.execWithStdin(args, secretEnv, secretVal, strings.NewReader(""))
}

// execWithStdin is the common subprocess runner. Most probes pass empty stdin;
// destructive refusal probes pass a blocking pipe so an unexpected stdin read is
// observable as a timeout.
func (r *Runner) execWithStdin(args []string, secretEnv, secretVal string, stdin io.Reader) Invocation {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.target.Path, args...)
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + r.confHome,
		"XDG_CONFIG_HOME=" + r.confHome,
		"TMPDIR=" + os.TempDir(),
		"TEMP=" + os.TempDir(),
		"TMP=" + os.TempDir(),
	}
	if runtime.GOOS == "windows" {
		env = append(env,
			"APPDATA="+r.confHome,
			"LOCALAPPDATA="+r.confHome,
			"SystemRoot="+os.Getenv("SystemRoot"),
			"SYSTEMROOT="+os.Getenv("SYSTEMROOT"),
		)
	}
	if secretEnv != "" {
		env = append(env, secretEnv+"="+secretVal)
	}
	cmd.Env = env
	cmd.Stdin = stdin

	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()

	exit := 0
	errText := ""
	timedOut := false
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			exit = -1
			timedOut = true
			errText = "target timed out"
		} else if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			exit = -1
			errText = err.Error()
		}
	}
	if timedOut && errText == "" {
		errText = fmt.Sprintf("target timed out after %s", r.timeout)
	}
	return Invocation{Stdout: out.String(), Stderr: errb.String(), Exit: exit, Err: errText, TimedOut: timedOut}
}

func (r *Runner) run(args []string) Invocation { return r.exec(args, "", "") }

// runWithBlockingStdin keeps the write end of a pipe open while the target runs.
// A well-behaved non-interactive command exits without reading; a command that
// tries to read waits until the context timeout and the probe can fail it.
func (r *Runner) runWithBlockingStdin(args []string) Invocation {
	reader, writer, err := os.Pipe()
	if err != nil {
		return Invocation{Exit: -1, Err: "create blocking stdin pipe: " + err.Error()}
	}
	defer reader.Close()
	defer writer.Close()
	return r.execWithStdin(args, "", "", reader)
}
