package config

import "fmt"

// ErrorKind classifies a config or resolution failure.
type ErrorKind string

const (
	ErrKindMalformedConfig ErrorKind = "malformed_config"
	ErrKindInvalidProfile  ErrorKind = "invalid_profile_name"
	ErrKindInvalidService  ErrorKind = "invalid_service"
)

// exitUsage mirrors rungrad.ExitUsage. The config package stays independent of
// the root package, while the integer exit contract is stable and documented.
const exitUsage = 1

// Error is a structured config/resolution failure. It never embeds secret
// values. Current kinds are user/configuration faults and map to usage.
type Error struct {
	Kind    ErrorKind
	Profile string
	Service string
	Path    string
	Detail  string
	Err     error
}

// Error renders a deterministic, secret-free message.
func (e Error) Error() string {
	switch e.Kind {
	case ErrKindInvalidProfile:
		if e.Profile != "" {
			return fmt.Sprintf("invalid profile name %q", e.Profile)
		}
		return "invalid profile name"
	case ErrKindInvalidService:
		msg := "invalid service"
		if e.Service != "" {
			msg = fmt.Sprintf("invalid service %q", e.Service)
		}
		if detail := e.detail(); detail != "" {
			return msg + ": " + detail
		}
		return msg
	case ErrKindMalformedConfig:
		msg := "malformed config"
		if e.Path != "" {
			msg = fmt.Sprintf("malformed config %s", e.Path)
		}
		if detail := e.detail(); detail != "" {
			return msg + ": " + detail
		}
		return msg
	default:
		if detail := e.detail(); detail != "" {
			return detail
		}
		return "config error"
	}
}

func (e Error) detail() string {
	if e.Detail != "" {
		return e.Detail
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return ""
}

// Unwrap exposes the underlying cause.
func (e Error) Unwrap() error { return e.Err }

// ExitCode maps structured resolution errors to usage.
func (e Error) ExitCode() int { return exitUsage }
