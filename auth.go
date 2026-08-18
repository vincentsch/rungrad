package rungrad

import (
	"context"
	"errors"
	"os"

	"github.com/vincentsch/rungrad/browser"
	"github.com/vincentsch/rungrad/config"
)

// Credential is the resolved runtime credential a RequiresAuth command
// consumes. It is distinct from config.Credential, which is the on-disk record.
type Credential struct {
	Token   string
	Profile string
	Source  string
	Display string
	Extra   any
}

// AuthContext is handed to a CredentialResolver after profile/service/path
// resolution and before the command handler runs.
type AuthContext struct {
	Context        context.Context
	Profile        string
	ConfigPath     string
	AuthFilePath   string
	EnvVar         string
	Store          config.Store
	LookupEnv      func(string) (string, bool)
	RegisterSecret func(string)
	services       map[string]config.ResolvedService
}

// Service returns a resolved service endpoint by name.
func (ac *AuthContext) Service(name string) (config.ResolvedService, bool) {
	if ac == nil || ac.services == nil {
		return config.ResolvedService{}, false
	}
	svc, ok := ac.services[name]
	return svc, ok
}

// CredentialResolver loads and may validate the credential for a RequiresAuth
// command.
type CredentialResolver interface {
	ResolveCredential(ac *AuthContext) (Credential, error)
}

// defaultCredentialResolver preserves the historical auth behavior: env var,
// then stored credential for the profile, then ErrMissingCredential.
type defaultCredentialResolver struct{}

func (defaultCredentialResolver) ResolveCredential(ac *AuthContext) (Credential, error) {
	if ac.EnvVar != "" {
		// The framework path uses the injected lookup so tests and embedders do
		// not have to mutate process-wide environment variables.
		lookup := ac.LookupEnv
		if lookup == nil {
			lookup = os.LookupEnv
		}
		if token, ok := lookup(ac.EnvVar); ok && token != "" {
			return Credential{Token: token, Profile: ac.Profile, Source: "env"}, nil
		}
	}
	stored, err := ac.Store.LoadCredential(ac.Profile)
	if err != nil {
		if errors.Is(err, config.ErrMissingCredential) {
			return Credential{}, err
		}
		// Store.LoadCredential already classifies malformed/unreadable local
		// credential files. Preserve that detail instead of wrapping it again.
		var cfgErr *config.Error
		if errors.As(err, &cfgErr) {
			return Credential{}, err
		}
		return Credential{}, &config.Error{
			Kind:   config.ErrKindMalformedConfig,
			Path:   ac.AuthFilePath,
			Detail: "malformed credentials",
			Err:    err,
		}
	}
	if stored.Token == "" {
		return Credential{}, config.ErrMissingCredential
	}
	return Credential{
		Token:   stored.Token,
		Profile: ac.Profile,
		Source:  "file",
		Display: stored.DisplayName,
	}, nil
}

// OpenBrowser opens url through Factory.BrowserOpener, or browser.Open when no
// opener is injected.
func (f *Factory) OpenBrowser(ctx context.Context, url string) error {
	if f.BrowserOpener != nil {
		return f.BrowserOpener(ctx, url)
	}
	return browser.Open(ctx, url)
}
