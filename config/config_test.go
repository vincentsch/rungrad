package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	return Store{Tool: "rgtest", Override: filepath.Join(dir, "config.yaml")}
}

func TestLoadTokenPrecedenceEnvWins(t *testing.T) {
	s := tempStore(t)
	if err := s.SaveCredential("default", Credential{Token: "from-file"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	t.Setenv("RGTEST_TOKEN", "from-env")
	tok, err := s.LoadToken("RGTEST_TOKEN", "default")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tok != "from-env" {
		t.Fatalf("env should win, got %q", tok)
	}
}

func TestLoadTokenFallsBackToFile(t *testing.T) {
	s := tempStore(t)
	if err := s.SaveCredential("default", Credential{Token: "from-file"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	tok, err := s.LoadToken("RGTEST_TOKEN_UNSET", "default")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tok != "from-file" {
		t.Fatalf("file token expected, got %q", tok)
	}
}

func TestLoadTokenMissingCredential(t *testing.T) {
	s := tempStore(t)
	_, err := s.LoadToken("RGTEST_TOKEN_UNSET", "default")
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("expected ErrMissingCredential, got %v", err)
	}
}

func TestLoadTokenWithSourceMalformedCredentialsIsConfigError(t *testing.T) {
	s := tempStore(t)
	path, err := s.CredentialsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"entries":`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, source, err := s.LoadTokenWithSource("RGTEST_TOKEN_UNSET", "default")
	if source != "" {
		t.Fatalf("source = %q, want empty", source)
	}
	var cfgErr *Error
	if !errors.As(err, &cfgErr) || cfgErr.Kind != ErrKindMalformedConfig || cfgErr.ExitCode() != 1 {
		t.Fatalf("error = %#v, want malformed config Error", err)
	}
}

func TestSaveCredentialIs0600(t *testing.T) {
	s := tempStore(t)
	if err := s.SaveCredential("default", Credential{Token: "secret"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	path, _ := s.CredentialsPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != fs.FileMode(0o600) {
		t.Fatalf("credentials perm = %o, want 600", perm)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	s := tempStore(t)
	in := Config{Version: 1, CurrentProfile: "work", Profiles: map[string]Profile{
		"work": {BaseURL: "https://api.example.com", Defaults: map[string]string{"team": "core"}, Services: map[string]string{"region": "eu"}},
	}, Services: map[string]string{"status": "https://status.example.com"}}
	if err := s.SaveConfig(in); err != nil {
		t.Fatalf("save config: %v", err)
	}
	out, err := s.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if out.CurrentProfile != "work" || out.Profiles["work"].BaseURL != "https://api.example.com" {
		t.Fatalf("round trip mismatch: %+v", out)
	}
	if out.Services["status"] != "https://status.example.com" || out.Profiles["work"].Services["region"] != "eu" {
		t.Fatalf("services round trip mismatch: %+v", out)
	}
}

func TestMaskHidesSecret(t *testing.T) {
	if got := Mask("super-secret-1234"); got != "****1234" {
		t.Fatalf("Mask = %q", got)
	}
	if got := Mask("abc"); got != "****" {
		t.Fatalf("Mask short = %q", got)
	}
}

func TestSaveCredentialDoesNotOverwriteCorruptFile(t *testing.T) {
	s := tempStore(t)
	path, err := s.CredentialsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := `{"version":1,"entries":`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredential("new", Credential{Token: "secret"}); err == nil {
		t.Fatalf("expected corrupt existing credential file to be preserved with an error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("corrupt credentials were overwritten: %s", got)
	}
}
