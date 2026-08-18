package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/redact"
	"github.com/vincentsch/rungrad/testutil"
)

const leakToken = "rgleak-raw-token-77777"

// redactionLeakApp is a throwaway app whose authenticated commands deliberately
// write the raw f.Token, proving the framework auto-registers the credential and
// scrubs it at the boundary. Kept in test-only code so adopters never copy it.
func redactionLeakApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{
		Name:           "rgleak",
		Short:          "redaction proof",
		EnvVar:         "RGLEAK_TOKEN",
		AdvancedOutput: true,
	})
	app.AddCommand(
		&rungrad.Command{
			Use:          "leak",
			Short:        "leak the raw auth token in success modes",
			OutputModes:  []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON, rungrad.OutputModeJQ, rungrad.OutputModeTemplate},
			RequiresAuth: true,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return f.WriteResult(map[string]string{"token": f.Token}, func(w io.Writer) {
					fmt.Fprintf(w, "raw token %s\n", f.Token)
				})
			},
		},
		&rungrad.Command{
			Use:          "boom",
			Short:        "leak the raw auth token through an error",
			OutputModes:  []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
			RequiresAuth: true,
			Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
				return rungrad.NewError(rungrad.ExitAPI, "backend rejected "+f.Token)
			},
		},
	)
	return app
}

// leakEnv is intentionally minimal: the leak app has no resolution config, so
// the default credential resolver only asks for RGLEAK_TOKEN in these tests.
func leakEnv(string) (string, bool) { return leakToken, true }

func TestAutoRegisteredAuthTokenRedactedAcrossBoundaries(t *testing.T) {
	t.Run("human stdout", func(t *testing.T) {
		res := testutil.RunWith(redactionLeakApp(), testutil.Options{LookupEnv: leakEnv}, "leak")
		if res.Exit != rungrad.ExitSuccess {
			t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
		}
		testutil.AssertRedacted(t, res, leakToken)
		if !strings.Contains(res.Stdout, redact.Token) {
			t.Fatalf("stdout missing redaction token: %q", res.Stdout)
		}
	})

	t.Run("json stdout", func(t *testing.T) {
		res := testutil.RunWith(redactionLeakApp(), testutil.Options{LookupEnv: leakEnv}, "leak", "--json")
		if res.Exit != rungrad.ExitSuccess {
			t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
		}
		testutil.AssertRedacted(t, res, leakToken)
		var body map[string]string
		if err := res.JSON(&body); err != nil {
			t.Fatalf("decode: %v\n%s", err, res.Stdout)
		}
		if body["token"] != redact.Token {
			t.Fatalf("token = %q, want %q", body["token"], redact.Token)
		}
	})

	t.Run("jq stdout", func(t *testing.T) {
		res := testutil.RunWith(redactionLeakApp(), testutil.Options{LookupEnv: leakEnv}, "leak", "--jq", ".token")
		if res.Exit != rungrad.ExitSuccess {
			t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
		}
		testutil.AssertRedacted(t, res, leakToken)
		if res.Stdout != `"[REDACTED]"`+"\n" {
			t.Fatalf("stdout = %q", res.Stdout)
		}
	})

	t.Run("template stdout", func(t *testing.T) {
		res := testutil.RunWith(redactionLeakApp(), testutil.Options{LookupEnv: leakEnv}, "leak", "--template", "{{.token}}")
		if res.Exit != rungrad.ExitSuccess {
			t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
		}
		testutil.AssertRedacted(t, res, leakToken)
		if res.Stdout != redact.Token+"\n" {
			t.Fatalf("stdout = %q", res.Stdout)
		}
	})

	t.Run("text error", func(t *testing.T) {
		res := testutil.RunWith(redactionLeakApp(), testutil.Options{LookupEnv: leakEnv}, "boom")
		if res.Exit != rungrad.ExitAPI {
			t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
		}
		if res.Stdout != "" {
			t.Fatalf("stdout = %q, want empty", res.Stdout)
		}
		testutil.AssertRedacted(t, res, leakToken)
		if !strings.Contains(res.Stderr, redact.Token) {
			t.Fatalf("stderr missing redaction token: %q", res.Stderr)
		}
	})

	t.Run("json error", func(t *testing.T) {
		res := testutil.RunWith(redactionLeakApp(), testutil.Options{LookupEnv: leakEnv}, "boom", "--json")
		if res.Exit != rungrad.ExitAPI {
			t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
		}
		if res.Stdout != "" {
			t.Fatalf("stdout = %q, want empty", res.Stdout)
		}
		testutil.AssertRedacted(t, res, leakToken)
		var body map[string]any
		if err := json.Unmarshal([]byte(res.Stderr), &body); err != nil {
			t.Fatalf("decode error JSON: %v\n%s", err, res.Stderr)
		}
		if !strings.Contains(res.Stderr, redact.Token) {
			t.Fatalf("stderr missing redaction token: %q", res.Stderr)
		}
	})
}
