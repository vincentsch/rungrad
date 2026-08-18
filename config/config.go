// Package config loads configuration and credentials with a documented
// precedence and safe on-disk handling. It supports both a simple single
// credential shape and a multi-profile shape, so a tool can start small and grow.
//
// Secrets live in a separate credentials file written atomically with 0600
// permissions and are never displayed in full. Configuration (non-secret) lives
// in a YAML file. Credential precedence is: environment variable, then the
// stored credential for the active profile, then ErrMissingCredential.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ErrMissingCredential is returned when no credential can be found by any means.
// The root package maps it to the auth exit code.
var ErrMissingCredential = errors.New("no credential found")

// Config is the non-secret configuration for a tool. A tool using a single
// profile can ignore Profiles and CurrentProfile and rely on Defaults.
type Config struct {
	Version        int                `yaml:"version"`
	CurrentProfile string             `yaml:"current_profile,omitempty"`
	Profiles       map[string]Profile `yaml:"profiles,omitempty"`
	Defaults       map[string]string  `yaml:"defaults,omitempty"`
	Services       map[string]string  `yaml:"services,omitempty"`
}

// Profile holds per-profile non-secret settings.
type Profile struct {
	BaseURL  string            `yaml:"base_url,omitempty"`
	Defaults map[string]string `yaml:"defaults,omitempty"`
	Services map[string]string `yaml:"services,omitempty"`
}

// Credential is a stored secret plus non-secret metadata about it.
type Credential struct {
	Token       string `json:"token"`
	DisplayName string `json:"display_name,omitempty"`
	ValidatedAt string `json:"validated_at,omitempty"`
}

// credentialsFile is the on-disk shape of the separate, 0600 credentials file.
type credentialsFile struct {
	Version int                   `json:"version"`
	Entries map[string]Credential `json:"entries"`
}

// Store resolves and reads a tool's config and credentials on disk.
type Store struct {
	// Tool is the program name, used to locate the default config directory under
	// os.UserConfigDir (for example ~/.config/<tool> on Linux).
	Tool string
	// Override, when set, is the config file path from a --config flag. The
	// credentials file is kept beside it unless Credentials is set.
	Override string
	// Credentials, when set, is the credentials file path from a --auth-file flag
	// or equivalent resolver. It is independent from Override.
	Credentials string
}

// DefaultDir returns the default configuration directory for a tool.
func DefaultDir(tool string) (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, tool), nil
}

func (s Store) dir() (string, error) {
	if s.Override != "" {
		return filepath.Dir(s.Override), nil
	}
	return DefaultDir(s.Tool)
}

// ConfigPath returns the resolved path of the config file.
func (s Store) ConfigPath() (string, error) {
	if s.Override != "" {
		return s.Override, nil
	}
	dir, err := s.dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// CredentialsPath returns the resolved path of the credentials file.
func (s Store) CredentialsPath() (string, error) {
	if s.Credentials != "" {
		return s.Credentials, nil
	}
	dir, err := s.dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

// LoadConfig reads the config file, returning a zero Config when none exists.
func (s Store) LoadConfig() (Config, error) {
	path, err := s.ConfigPath()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{Version: 1}, nil
	}
	if err != nil {
		return Config{}, err
	}
	if len(data) == 0 {
		return Config{Version: 1}, nil
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return c, nil
}

// SaveConfig writes the config file atomically with 0644 permissions.
func (s Store) SaveConfig(c Config) error {
	path, err := s.ConfigPath()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o644)
}

// LoadToken resolves a credential token with precedence: the environment
// variable envVar (when set and non-empty), then the stored credential for the
// given profile (empty profile means the default entry), then
// ErrMissingCredential. The returned token is never logged by this package.
func (s Store) LoadToken(envVar, profile string) (string, error) {
	token, _, err := s.LoadTokenWithSource(envVar, profile)
	return token, err
}

// LoadTokenWithSource is LoadToken that also reports the origin: "env" when the
// environment variable supplied the token, "file" when the stored credential
// did. The source is empty alongside ErrMissingCredential. The returned token is
// never logged by this package.
func (s Store) LoadTokenWithSource(envVar, profile string) (token, source string, err error) {
	if envVar != "" {
		if v := os.Getenv(envVar); v != "" {
			return v, "env", nil
		}
	}
	cred, err := s.LoadCredential(profile)
	if err != nil {
		return "", "", err
	}
	if cred.Token == "" {
		return "", "", ErrMissingCredential
	}
	return cred.Token, "file", nil
}

// LoadCredential reads the stored credential for a profile (empty profile means
// the "default" entry). It returns ErrMissingCredential when the file or entry
// does not exist.
func (s Store) LoadCredential(profile string) (Credential, error) {
	if profile == "" {
		profile = "default"
	}
	path, err := s.CredentialsPath()
	if err != nil {
		return Credential{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Credential{}, ErrMissingCredential
	}
	if err != nil {
		// The user selected this credentials path, so local read failures are
		// treated as configuration/auth-file faults rather than upstream errors.
		return Credential{}, &Error{
			Kind:   ErrKindMalformedConfig,
			Path:   path,
			Detail: "read credentials",
			Err:    err,
		}
	}
	var f credentialsFile
	if err := json.Unmarshal(data, &f); err != nil {
		// Keep the user-facing message stable and secret-free while preserving
		// the parse error for errors.Is/As callers through Unwrap.
		return Credential{}, &Error{
			Kind:   ErrKindMalformedConfig,
			Path:   path,
			Detail: "malformed credentials",
			Err:    fmt.Errorf("parse credentials: %w", err),
		}
	}
	cred, ok := f.Entries[profile]
	if !ok {
		return Credential{}, ErrMissingCredential
	}
	return cred, nil
}

// SaveCredential stores a credential for a profile (empty profile means the
// "default" entry), writing the credentials file atomically with 0600
// permissions.
func (s Store) SaveCredential(profile string, cred Credential) error {
	if profile == "" {
		profile = "default"
	}
	path, err := s.CredentialsPath()
	if err != nil {
		return err
	}
	f := credentialsFile{Version: 1, Entries: map[string]Credential{}}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &f); err != nil {
			return fmt.Errorf("parse existing credentials: %w", err)
		}
		if f.Entries == nil {
			f.Entries = map[string]Credential{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f.Entries[profile] = cred
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

// Mask returns a display-safe rendering of a secret: the last four characters
// preceded by a fixed prefix, or a constant when the secret is short.
func Mask(secret string) string {
	if len(secret) <= 4 {
		return "****"
	}
	return "****" + secret[len(secret)-4:]
}

// writeFileAtomic writes data to a temporary file in the same directory and
// renames it into place, so a reader never observes a half-written file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
