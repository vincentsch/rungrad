package main

import (
	"encoding/json"
	"strings"
	"testing"

	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/testutil"
)

func TestItemListJSON(t *testing.T) {
	res := runRgref(t, "item", "list", "--json")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d: %s", res.Exit, res.Stderr)
	}
	var items []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Size  int64  `json:"size"`
		Label string `json:"label"`
	}
	if err := res.JSON(&items); err != nil {
		t.Fatalf("invalid JSON: %v (%s)", err, res.Stdout)
	}
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
}

func TestItemGetUnique(t *testing.T) {
	res := runRgref(t, "item", "get", "alpha")
	if res.Exit != 0 || strings.TrimSpace(res.Stdout) != "1" {
		t.Fatalf("get alpha => exit %d stdout %q", res.Exit, res.Stdout)
	}
}

func TestItemGetAmbiguousNoPrompt(t *testing.T) {
	res := runRgref(t, "item", "get", "dup", "--no-prompt")
	if res.Exit != rungrad.ExitUsage {
		t.Fatalf("ambiguous --no-prompt => exit %d, want %d", res.Exit, rungrad.ExitUsage)
	}
}

func TestItemGetAmbiguousInteractiveStdin(t *testing.T) {
	res := runRgrefWith(t, testutil.Options{
		Stdin:       strings.NewReader("2\n"),
		Terminal:    true,
		TerminalSet: true,
	}, "item", "get", "dup")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("expected interactive success, got %d stderr=%q", res.Exit, res.Stderr)
	}
	if strings.TrimSpace(res.Stdout) != "3" {
		t.Fatalf("expected selection 2 to choose ID 3, got %q", res.Stdout)
	}
}

func TestItemGetAPIError(t *testing.T) {
	res := runRgref(t, "item", "get", "broken")
	if res.Exit != rungrad.ExitAPI {
		t.Fatalf("item get broken => exit %d, want %d (stderr=%q)", res.Exit, rungrad.ExitAPI, res.Stderr)
	}
}

func TestItemGetForbidden(t *testing.T) {
	res := runRgref(t, "item", "get", "forbidden")
	if res.Exit != rungrad.ExitForbidden {
		t.Fatalf("item get forbidden => exit %d, want %d (stderr=%q)", res.Exit, rungrad.ExitForbidden, res.Stderr)
	}
}

func TestItemGetRateLimited(t *testing.T) {
	res := runRgref(t, "item", "get", "throttled")
	if res.Exit != rungrad.ExitRateLimited {
		t.Fatalf("item get throttled => exit %d, want %d (stderr=%q)", res.Exit, rungrad.ExitRateLimited, res.Stderr)
	}
}

func TestWhoamiMissingCredential(t *testing.T) {
	res := runRgref(t, "whoami", "--config", missingConfig(t))
	if res.Exit != rungrad.ExitAuth {
		t.Fatalf("whoami no cred => exit %d, want %d", res.Exit, rungrad.ExitAuth)
	}
}

func TestUpdateHelpUsesToolNameExamples(t *testing.T) {
	res := runRgref(t, "update", "--help")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("update --help => exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "rgref update --check --json") {
		t.Fatalf("update help missing rgref examples:\n%s", res.Stdout)
	}
	if strings.Contains(res.Stdout, "mytool") {
		t.Fatalf("update help leaks mytool:\n%s", res.Stdout)
	}
	if strings.Contains(strings.ToLower(res.Stdout), "related commands:") {
		t.Fatalf("update help should not advertise a version related command:\n%s", res.Stdout)
	}
}

func TestCreateDryRun(t *testing.T) {
	res := runRgref(t, "item", "create", "gamma", "--dry-run")
	if res.Exit != 0 || !strings.Contains(strings.ToLower(res.Stdout), "dry") {
		t.Fatalf("create --dry-run => exit %d stdout %q", res.Exit, res.Stdout)
	}
	if strings.Contains(res.Stdout, "reference-secret") {
		t.Fatalf("dry-run leaked secret: %s", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "***") {
		t.Fatalf("dry-run did not show masked secret marker: %s", res.Stdout)
	}
}

func TestItemCreateEmitsQuietHint(t *testing.T) {
	res := runRgref(t, "item", "create", "gamma")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("create => exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "hint: pass --quiet to hide informational messages") {
		t.Fatalf("create did not emit quiet hint: %q", res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Created item gamma") {
		t.Fatalf("create did not emit mutation summary: %q", res.Stdout)
	}
	if strings.Contains(res.Stdout, "\x1b") {
		t.Fatalf("default stdout emitted ANSI: %q", res.Stdout)
	}
}

func TestItemCreateQuietSuppressesHint(t *testing.T) {
	res := runRgref(t, "item", "create", "gamma", "--quiet")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("create --quiet => exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if strings.Contains(res.Stderr, "hint: pass --quiet to hide informational messages") {
		t.Fatalf("--quiet did not suppress hint: %q", res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Created") {
		t.Fatalf("--quiet suppressed primary stdout: %q", res.Stdout)
	}
}

func TestItemCreateJSONHasNoHintNoANSI(t *testing.T) {
	for _, args := range [][]string{
		{"item", "create", "gamma", "--json"},
		{"item", "create", "gamma", "--json", "--quiet"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			res := runRgref(t, args...)
			if res.Exit != rungrad.ExitSuccess {
				t.Fatalf("%v => exit %d stderr=%q", args, res.Exit, res.Stderr)
			}
			var body map[string]any
			if err := res.JSON(&body); err != nil {
				t.Fatalf("invalid JSON: %v (%s)", err, res.Stdout)
			}
			if strings.Contains(res.Stdout, "\x1b") {
				t.Fatalf("JSON stdout emitted ANSI: %q", res.Stdout)
			}
			if strings.Contains(res.Stderr, "hint: pass --quiet to hide informational messages") {
				t.Fatalf("JSON mode emitted hint: %q", res.Stderr)
			}
		})
	}
}

func TestItemCreateDryRunForcedTerminalColorsLabel(t *testing.T) {
	res := runRgrefWith(t, testutil.Options{
		OutputTerminalSet: true,
		OutputTerminal:    true,
	}, "item", "create", "gamma", "--dry-run")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("create --dry-run forced terminal => exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "\x1b[1;33mDRY RUN\x1b[0m") {
		t.Fatalf("forced terminal dry-run did not color label: %q", res.Stdout)
	}
}

func TestItemCreateForcedTerminalColorsAction(t *testing.T) {
	res := runRgrefWith(t, testutil.Options{
		OutputTerminalSet: true,
		OutputTerminal:    true,
	}, "item", "create", "gamma")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("create forced terminal => exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "\x1b[1;32mCreated\x1b[0m") {
		t.Fatalf("forced terminal create did not color action: %q", res.Stdout)
	}
}

func TestItemDeleteDryRun(t *testing.T) {
	res := runRgref(t, "item", "delete", "alpha", "--dry-run")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("delete --dry-run => exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if !strings.Contains(strings.ToLower(res.Stdout), "dry") {
		t.Fatalf("delete --dry-run not a preview: %q", res.Stdout)
	}
	if strings.Contains(strings.ToLower(res.Stdout), "deleted ") {
		t.Fatalf("delete --dry-run reported a completed mutation: %q", res.Stdout)
	}
}

func TestItemDeleteNoPromptRequiresConfirm(t *testing.T) {
	res := runRgref(t, "item", "delete", "alpha", "--no-prompt")
	if res.Exit != rungrad.ExitUsage {
		t.Fatalf("delete --no-prompt => exit %d, want %d (stderr=%q)", res.Exit, rungrad.ExitUsage, res.Stderr)
	}
}

func TestItemDeleteJSONRequiresConfirm(t *testing.T) {
	res := runRgref(t, "item", "delete", "alpha", "--json")
	if res.Exit != rungrad.ExitUsage {
		t.Fatalf("delete --json => exit %d, want %d", res.Exit, rungrad.ExitUsage)
	}
	if strings.TrimSpace(res.Stdout) != "" {
		t.Fatalf("delete --json refusal should leave stdout empty, got %q", res.Stdout)
	}
	var body struct {
		ExitCode int `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(res.Stderr), &body); err != nil || body.ExitCode != 1 {
		t.Fatalf("delete --json refusal stderr not a JSON error body with exit_code 1: %v\n%s", err, res.Stderr)
	}
}

func TestItemDeleteForcedTerminalColorsAction(t *testing.T) {
	res := runRgrefWith(t, testutil.Options{
		OutputTerminalSet: true,
		OutputTerminal:    true,
	}, "item", "delete", "alpha", "--confirm")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("delete forced terminal => exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "\x1b[1;32mDeleted\x1b[0m") {
		t.Fatalf("forced terminal delete did not color action: %q", res.Stdout)
	}
}

func TestMutationOutputDefaultsEscapeFree(t *testing.T) {
	for _, args := range [][]string{
		{"item", "create", "gamma"},
		{"item", "delete", "alpha", "--confirm"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			res := runRgref(t, args...)
			if res.Exit != rungrad.ExitSuccess {
				t.Fatalf("%v => exit %d stderr=%q", args, res.Exit, res.Stderr)
			}
			if strings.Contains(res.Stdout, "\x1b") {
				t.Fatalf("default stdout emitted ANSI: %q", res.Stdout)
			}
		})
	}
}

func TestItemDeleteConfirmFlag(t *testing.T) {
	res := runRgref(t, "item", "delete", "alpha", "--confirm")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("delete --confirm => exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Deleted") {
		t.Fatalf("delete --confirm did not report the deletion: %q", res.Stdout)
	}
}

func TestItemDeleteDoesNotMutateFixture(t *testing.T) {
	if res := runRgref(t, "item", "delete", "alpha", "--confirm"); res.Exit != 0 {
		t.Fatalf("first delete failed: %s", res.Stderr)
	}
	res := runRgref(t, "item", "list", "--json")
	if res.Exit != 0 {
		t.Fatalf("list after delete failed: %s", res.Stderr)
	}
	if !strings.Contains(res.Stdout, "alpha") {
		t.Fatalf("confirmed delete mutated the package-level fixture; alpha missing: %q", res.Stdout)
	}
}

func TestItemDeleteInteractiveConfirm(t *testing.T) {
	res := runRgrefWith(t, testutil.Options{
		Stdin:       strings.NewReader("y\n"),
		Terminal:    true,
		TerminalSet: true,
	}, "item", "delete", "alpha")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("interactive delete => exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Deleted") {
		t.Fatalf("interactive delete did not report the deletion: %q", res.Stdout)
	}
}

func TestItemDeleteChainedAmbiguousThenConfirm(t *testing.T) {
	// "dup" is ambiguous: the selection prompt consumes "2", then the destructive
	// confirmation prompt consumes "y", proving the two prompts read stdin in order.
	res := runRgrefWith(t, testutil.Options{
		Stdin:       strings.NewReader("2\ny\n"),
		Terminal:    true,
		TerminalSet: true,
	}, "item", "delete", "dup")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("chained ambiguous+confirm delete => exit %d stderr=%q", res.Exit, res.Stderr)
	}
}

func TestWhoamiNeverPrintsRawToken(t *testing.T) {
	const token = "super-secret-raw-token"
	res := runRgrefWith(t, testutil.Options{
		LookupEnv: rgrefLookup(map[string]string{"RGREF_TOKEN": token}),
	}, "whoami")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("whoami => exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if strings.Contains(res.Stdout, token) {
		t.Fatalf("raw token leaked: %s", res.Stdout)
	}
}
