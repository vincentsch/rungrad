package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/config"
	"github.com/vincentsch/rungrad/testutil"
)

func TestItemListIncludeMetaEnvelope(t *testing.T) {
	res := runRgrefWith(t, testutil.Options{LookupEnv: rgrefNoEnv},
		"item", "list", "--include-meta", "--json", "--config", missingConfig(t))
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	var env struct {
		Data []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Size  int64  `json:"size"`
			Label string `json:"label"`
		} `json:"data"`
		Meta struct {
			RequestID  string `json:"request_id"`
			Pagination struct {
				Page       int  `json:"page"`
				PerPage    int  `json:"per_page"`
				TotalPages int  `json:"total_pages"`
				TotalItems int  `json:"total_items"`
				HasMore    bool `json:"has_more"`
			} `json:"pagination"`
			RateLimit struct {
				Limit     int64 `json:"limit"`
				Remaining int64 `json:"remaining"`
				Reset     int64 `json:"reset"`
			} `json:"rate_limit"`
			Retry struct {
				Attempts int `json:"attempts"`
			} `json:"retry"`
			Idem struct {
				Key      string `json:"key"`
				Replayed bool   `json:"replayed"`
			} `json:"idempotency"`
			Extra struct {
				ServiceURL string `json:"service_url"`
			} `json:"extra"`
		} `json:"meta"`
	}
	if err := res.JSON(&env); err != nil {
		t.Fatalf("decode: %v\n%s", err, res.Stdout)
	}
	if len(env.Data) != 3 {
		t.Fatalf("data len = %d, want 3", len(env.Data))
	}
	if got := env.Data[0]; got.ID != "1" || got.Name != "alpha" || got.Size != 9007199254740993 || got.Label != "A&B <demo> café" {
		t.Fatalf("first data item = %+v", got)
	}
	if env.Meta.RequestID != "req_rgref_items_0001" {
		t.Fatalf("request_id = %q", env.Meta.RequestID)
	}
	p := env.Meta.Pagination
	if p.Page != 1 || p.PerPage != 3 || p.TotalPages != 1 || p.TotalItems != 3 || p.HasMore {
		t.Fatalf("pagination = %+v", p)
	}
	rl := env.Meta.RateLimit
	if rl.Limit != 1000 || rl.Remaining != 997 || rl.Reset != 1893456000 {
		t.Fatalf("rate_limit = %+v", rl)
	}
	if env.Meta.Retry.Attempts != 1 {
		t.Fatalf("retry = %+v", env.Meta.Retry)
	}
	if env.Meta.Idem.Key != "rgref-item-list-fixture" || env.Meta.Idem.Replayed {
		t.Fatalf("idempotency = %+v", env.Meta.Idem)
	}
	if env.Meta.Extra.ServiceURL != "https://api.rgref.invalid" {
		t.Fatalf("service_url = %q", env.Meta.Extra.ServiceURL)
	}
	// Decoded bools cannot distinguish "absent" from "false"; keep a raw check
	// for the explicit false fields that metadata pointers are meant to preserve.
	for _, want := range []string{`"has_more": false`, `"replayed": false`} {
		if !strings.Contains(res.Stdout, want) {
			t.Fatalf("stdout missing explicit false field %s:\n%s", want, res.Stdout)
		}
	}
}

func TestItemListJSONWithoutMetaOmitsEnvelope(t *testing.T) {
	res := runRgref(t, "item", "list", "--json")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	var data []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &data); err != nil {
		t.Fatalf("decode array: %v\n%s", err, res.Stdout)
	}
	if len(data) != 3 {
		t.Fatalf("data len = %d, want 3", len(data))
	}
	if strings.Contains(res.Stdout, `"data":`) || strings.Contains(res.Stdout, `"meta":`) {
		t.Fatalf("bare JSON should not be enveloped:\n%s", res.Stdout)
	}
}

func TestItemListIncludeMetaTransformsSeeEnvelope(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"jq request", []string{"item", "list", "--include-meta", "--jq", ".meta.request_id"}, "\"req_rgref_items_0001\"\n"},
		{"jq data", []string{"item", "list", "--include-meta", "--jq", ".data[].name"}, "\"alpha\"\n\"dup\"\n\"dup\"\n"},
		{"template meta", []string{"item", "list", "--include-meta", "--template", "{{.meta.pagination.total_items}}"}, "3\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runRgref(t, tt.args...)
			if res.Exit != rungrad.ExitSuccess {
				t.Fatalf("%v exit %d stderr=%q", tt.args, res.Exit, res.Stderr)
			}
			if res.Stdout != tt.want {
				t.Fatalf("%v stdout = %q, want %q", tt.args, res.Stdout, tt.want)
			}
		})
	}
}

func TestItemListIncludeMetaMachineModesAreDeterministic(t *testing.T) {
	dir := t.TempDir()
	// Reuse the same isolated config home for both runs in each mode. The test
	// compares each output mode only against itself.
	opts := testutil.Options{
		LookupEnv:     rgrefNoEnv,
		UserConfigDir: func() (string, error) { return dir, nil },
	}
	tests := []struct {
		name string
		args []string
	}{
		{"json", []string{"item", "list", "--include-meta", "--json"}},
		{"jq", []string{"item", "list", "--include-meta", "--jq", ".meta.request_id"}},
		{"template", []string{"item", "list", "--include-meta", "--template", "{{.meta.pagination.total_items}}"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first := runRgrefWith(t, opts, tt.args...)
			if first.Exit != rungrad.ExitSuccess {
				t.Fatalf("%v exit %d stderr=%q", tt.args, first.Exit, first.Stderr)
			}
			second := runRgrefWith(t, opts, tt.args...)
			if second.Exit != rungrad.ExitSuccess {
				t.Fatalf("%v second exit %d stderr=%q", tt.args, second.Exit, second.Stderr)
			}
			if first.Stdout != second.Stdout {
				t.Fatalf("%v stdout not deterministic:\n%q\n%q", tt.args, first.Stdout, second.Stdout)
			}
		})
	}
}

func TestItemListIncludeMetaRejections(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no machine mode", []string{"item", "list", "--include-meta"}, "--include-meta requires --json, --jq, or --template"},
		{"with plain", []string{"item", "list", "--include-meta", "--plain"}, "--plain cannot be combined"},
		{"with dry-run", []string{"item", "list", "--include-meta", "--json", "--dry-run"}, "--include-meta cannot be combined with --dry-run"},
		{"unsupported command", []string{"item", "get", "alpha", "--include-meta", "--json"}, "does not support --include-meta"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runRgrefWith(t, testutil.Options{LookupEnv: rgrefNoEnv}, tt.args...)
			assertRgrefExitStdoutEmptyStderrContains(t, res, rungrad.ExitUsage, tt.want)
		})
	}
}

func TestItemListServiceURLPrecedence(t *testing.T) {
	// Decode only the metadata field under test; the framework's resolver owns
	// the precedence logic, while rgref only surfaces the resolved value.
	serviceURL := func(t *testing.T, opts testutil.Options, args ...string) string {
		t.Helper()
		base := []string{"item", "list", "--include-meta", "--json"}
		res := runRgrefWith(t, opts, append(base, args...)...)
		if res.Exit != rungrad.ExitSuccess {
			t.Fatalf("%v exit %d stderr=%q", args, res.Exit, res.Stderr)
		}
		var body struct {
			Meta struct {
				Extra struct {
					ServiceURL string `json:"service_url"`
				} `json:"extra"`
			} `json:"meta"`
		}
		if err := res.JSON(&body); err != nil {
			t.Fatalf("decode: %v\n%s", err, res.Stdout)
		}
		return body.Meta.Extra.ServiceURL
	}
	// Use the real config store so these cases exercise the same service-map
	// shape users would write on disk.
	writeConfig := func(t *testing.T, path string, cfg config.Config) {
		t.Helper()
		if err := (config.Store{Tool: "rgref", Override: path}).SaveConfig(cfg); err != nil {
			t.Fatalf("SaveConfig: %v", err)
		}
	}

	t.Run("flag", func(t *testing.T) {
		got := serviceURL(t, testutil.Options{LookupEnv: rgrefNoEnv},
			"--api-url", "https://flag.rgref.invalid", "--config", missingConfig(t))
		if got != "https://flag.rgref.invalid" {
			t.Fatalf("service_url = %q", got)
		}
	})

	t.Run("env", func(t *testing.T) {
		got := serviceURL(t, testutil.Options{LookupEnv: rgrefLookup(map[string]string{"RGREF_API_URL": "https://env.rgref.invalid"})},
			"--config", missingConfig(t))
		if got != "https://env.rgref.invalid" {
			t.Fatalf("service_url = %q", got)
		}
	})

	t.Run("profile config", func(t *testing.T) {
		cfgPath := filepath.Join(t.TempDir(), "config.yaml")
		writeConfig(t, cfgPath, config.Config{
			Version:        1,
			CurrentProfile: "work",
			Profiles: map[string]config.Profile{
				"work": {Services: map[string]string{"api_url": "https://profile.rgref.invalid"}},
			},
		})
		got := serviceURL(t, testutil.Options{LookupEnv: rgrefNoEnv}, "--config", cfgPath)
		if got != "https://profile.rgref.invalid" {
			t.Fatalf("service_url = %q", got)
		}
	})

	t.Run("global config", func(t *testing.T) {
		cfgPath := filepath.Join(t.TempDir(), "config.yaml")
		writeConfig(t, cfgPath, config.Config{
			Version:  1,
			Services: map[string]string{"api_url": "https://config.rgref.invalid"},
		})
		got := serviceURL(t, testutil.Options{LookupEnv: rgrefNoEnv}, "--config", cfgPath)
		if got != "https://config.rgref.invalid" {
			t.Fatalf("service_url = %q", got)
		}
	})

	t.Run("builtin", func(t *testing.T) {
		got := serviceURL(t, testutil.Options{LookupEnv: rgrefNoEnv}, "--config", missingConfig(t))
		if got != "https://api.rgref.invalid" {
			t.Fatalf("service_url = %q", got)
		}
	})
}

func TestWhoamiAuthFileProfile(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "credentials.json")
	const token = "rgref-work-profile-token-xyz"
	if err := (config.Store{Tool: "rgref", Credentials: authFile}).
		SaveCredential("work", config.Credential{Token: token, DisplayName: "work-user"}); err != nil {
		t.Fatal(err)
	}
	res := runRgrefWith(t, testutil.Options{LookupEnv: rgrefNoEnv},
		"whoami", "--profile", "work", "--auth-file", authFile,
		"--config", filepath.Join(dir, "none.yaml"), "--json")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d stderr=%q", res.Exit, res.Stderr)
	}
	testutil.AssertRedacted(t, res, token)
	var body map[string]string
	if err := res.JSON(&body); err != nil {
		t.Fatalf("decode: %v\n%s", err, res.Stdout)
	}
	if body["token"] != config.Mask(token) {
		t.Fatalf("token = %q, want masked %q", body["token"], config.Mask(token))
	}
}
