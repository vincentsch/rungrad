package rungrad

import (
	"errors"

	"github.com/vincentsch/rungrad/config"
	"github.com/vincentsch/rungrad/resolve"
)

// Process exit codes. The values are part of the agent-ready contract: an
// automated caller can branch on them without parsing text. They are stable and
// documented in spec/exit-codes.md.
const (
	ExitSuccess     = 0 // the command did what was asked
	ExitUsage       = 1 // bad invocation: unknown command/flag, ambiguous name, validation
	ExitAPI         = 2 // an upstream/runtime error the user did not cause
	ExitAuth        = 3 // missing or invalid credentials
	ExitForbidden   = 4 // authenticated but not permitted
	ExitNotFound    = 5 // the requested resource does not exist
	ExitRateLimited = 6 // throttled by an upstream service
)

// exitCoder lets a tool's own error type declare its exit code directly.
type exitCoder interface {
	ExitCode() int
}

// usageError marks bad invocations produced by rungrad/Cobra validation, keeping
// them on the usage exit code even when the wrapped error has no explicit code.
type usageError struct {
	err error
}

// flagParseError keeps flag parser failures classified as usage errors while
// letting error rendering know that parsed flag state may not be trustworthy.
type flagParseError struct {
	*usageError
}

// newUsageError wraps an error once with usage classification.
func newUsageError(err error) error {
	if err == nil {
		return nil
	}
	var existing *usageError
	if errors.As(err, &existing) {
		return err
	}
	return &usageError{err: err}
}

// newFlagParseError wraps Cobra flag parse errors without double-wrapping errors
// that already carry either the parse marker or the general usage marker.
func newFlagParseError(err error) error {
	if err == nil {
		return nil
	}
	var existing *flagParseError
	if errors.As(err, &existing) {
		return err
	}
	usage, ok := newUsageError(err).(*usageError)
	if !ok {
		return newUsageError(err)
	}
	return &flagParseError{usageError: usage}
}

func (e *usageError) Error() string { return e.err.Error() }

func (e *usageError) Unwrap() error { return e.err }

func (e *usageError) ExitCode() int { return ExitUsage }

// Error is a convenience error carrying an explicit exit code, for tools that do
// not want to define their own error types.
type Error struct {
	Code int
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Msg != "" {
		return e.Msg
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "error"
}

// Unwrap exposes a wrapped cause for errors.Is/As.
func (e *Error) Unwrap() error { return e.Err }

// ExitCode implements exitCoder.
func (e *Error) ExitCode() int { return e.Code }

// NewError builds an Error with an explicit exit code.
func NewError(code int, msg string) *Error { return &Error{Code: code, Msg: msg} }

// ExitCodeFor maps an error returned by a command to a process exit code.
func ExitCodeFor(err error) int { return classifyExit(err) }

// classifyExit maps an error returned by a command to a process exit code. The
// order matters: an explicit exitCoder wins, then the framework's known typed
// errors, then a general-failure default.
func classifyExit(err error) int {
	if err == nil {
		return ExitSuccess
	}
	var coder exitCoder
	if errors.As(err, &coder) {
		return coder.ExitCode()
	}
	if errors.Is(err, config.ErrMissingCredential) {
		return ExitAuth
	}
	var notFound *resolve.NotFoundError
	if errors.As(err, &notFound) {
		return ExitNotFound
	}
	var ambiguous *resolve.AmbiguousError
	if errors.As(err, &ambiguous) {
		return ExitUsage
	}
	if isUsageError(err) {
		return ExitUsage
	}
	return ExitAPI
}
