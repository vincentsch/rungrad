package adopterfixture

import (
	"encoding/json"
	"strings"
	"testing"

	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/testutil"
)

func TestAdopterHostOwnedGlobalsDriveMachineOutput(t *testing.T) {
	res := testutil.Run(NewApp(), "widget", "list", "--json")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("exit %d: %s", res.Exit, res.Stderr)
	}
	var got []widget
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, res.Stdout)
	}
	if len(got) == 0 {
		t.Fatalf("widget list returned no fixtures")
	}
}

func TestAdopterErrorPolicyOwnsRenderingAndExit(t *testing.T) {
	human := testutil.Run(NewApp(), "widget", "get", "locked")
	if human.Exit != rungrad.ExitForbidden {
		t.Fatalf("human exit = %d, want %d (stderr=%q)", human.Exit, rungrad.ExitForbidden, human.Stderr)
	}
	if human.Stdout != "" {
		t.Fatalf("human stdout = %q, want empty", human.Stdout)
	}
	if human.Stderr != "acmectl: widget locked\n" {
		t.Fatalf("human stderr = %q", human.Stderr)
	}

	machine := testutil.Run(NewApp(), "widget", "get", "locked", "--json")
	if machine.Exit != rungrad.ExitForbidden {
		t.Fatalf("machine exit = %d, want %d (stderr=%q)", machine.Exit, rungrad.ExitForbidden, machine.Stderr)
	}
	if machine.Stdout != "" {
		t.Fatalf("machine stdout = %q, want empty", machine.Stdout)
	}
	var body struct {
		Error    string `json:"error"`
		ExitCode int    `json:"exit_code"`
		Message  string `json:"message"`
		Status   int    `json:"status"`
	}
	if err := json.Unmarshal([]byte(machine.Stderr), &body); err != nil {
		t.Fatalf("machine stderr is not valid JSON: %v\n%s", err, machine.Stderr)
	}
	if body.Error != "widget locked" || body.Message != "widget locked" ||
		body.ExitCode != rungrad.ExitForbidden || body.Status != rungrad.ExitForbidden {
		t.Fatalf("machine error body = %+v", body)
	}
}

func TestAdopterCommandExtensionsAdvertised(t *testing.T) {
	var entry *manifest.Command
	doc := NewApp().ManifestDocument()
	for i := range doc.Commands {
		if strings.Join(doc.Commands[i].Path, " ") == "widget list" {
			entry = &doc.Commands[i]
			break
		}
	}
	if entry == nil {
		t.Fatalf("manifest missing widget list command")
	}
	if err := manifest.RequireExtensionFields(entry.Extensions, extNamespace, "owner", "status", "docs_path"); err != nil {
		t.Fatal(err)
	}
	if err := manifest.RequireExtensionEnum(entry.Extensions, extNamespace, "status", "alpha", "beta", "stable"); err != nil {
		t.Fatal(err)
	}
	if err := manifest.RequireExtensionDocPath(entry.Extensions, extNamespace, "docs_path"); err != nil {
		t.Fatal(err)
	}
	if err := NewApp().ValidateCatalog(); err != nil {
		t.Fatalf("ValidateCatalog: %v", err)
	}
}

func TestAdopterDocsHelpManifestConsistent(t *testing.T) {
	testutil.AssertConsistent(t, NewApp)
}

func TestAdopterRenamedMachineJSONUnknownCommandErrorsJSON(t *testing.T) {
	machine := testutil.Run(NewMachineJSONApp(), "definitely-not-a-command", "--machine-json")
	if machine.Exit == rungrad.ExitSuccess {
		t.Fatalf("unknown command unexpectedly succeeded")
	}
	if machine.Stdout != "" {
		t.Fatalf("machine stdout = %q, want empty", machine.Stdout)
	}
	var body struct {
		Error    string `json:"error"`
		ExitCode int    `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(machine.Stderr), &body); err != nil {
		t.Fatalf("machine stderr is not valid JSON: %v\n%s", err, machine.Stderr)
	}
	if body.Error == "" || body.ExitCode == 0 {
		t.Fatalf("machine error body = %+v", body)
	}

	human := testutil.Run(NewMachineJSONApp(), "definitely-not-a-command")
	if human.Exit == rungrad.ExitSuccess {
		t.Fatalf("unknown command without machine flag unexpectedly succeeded")
	}
	if json.Valid([]byte(human.Stderr)) {
		t.Fatalf("human stderr unexpectedly JSON: %s", human.Stderr)
	}
	if !strings.Contains(human.Stderr, "unknown command") {
		t.Fatalf("human stderr does not mention unknown command: %q", human.Stderr)
	}
}
