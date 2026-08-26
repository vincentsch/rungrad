package scaffold

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/vincentsch/rungrad/agentmeta"
	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/output"
)

func TestProductManifestMatchesRuntimeAgentMetadata(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Name:              "acmectl",
		Module:            "example.com/acme/acmectl",
		RungradReplace:    repoRoot,
		ProductProfile:    true,
		Skill:             true,
		EnvPrefix:         "ACME",
		ProductName:       "Acme Control",
		Description:       "Manage Acme services",
		Services:          []string{"api=https://api.example.invalid", "billing-api=https://billing.example.invalid"},
		MetadataNamespace: "example.com/acme",
		Surface:           "host",
		ReleaseOwner:      "example",
		ReleaseRepo:       "acmectl",
		DocsLabel:         "Acme CLI",
		Examples: []string{
			"acmectl widget list",
			"acmectl widget create delta --dry-run",
			"acmectl widget delete delta --dry-run",
			"acmectl update --check",
		},
	}
	root, err := Write(t.TempDir(), opts)
	if err != nil {
		t.Fatal(err)
	}
	runGoInDir(t, root, onlineGoEnvForSkillTest(), "mod", "tidy")
	runGoInDir(t, root, offlineGoEnvForSkillTest(), "mod", "tidy")

	bin := filepath.Join(root, "acmectl")
	runGoInDir(t, root, offlineGoEnvForSkillTest(), "build", "-o", bin, ".")
	cmd := exec.Command(bin, "__rungrad_manifest")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("__rungrad_manifest failed: %v\n%s", err, out)
	}
	var runtimeManifest manifest.Manifest
	if err := json.Unmarshal(out, &runtimeManifest); err != nil {
		t.Fatalf("runtime manifest JSON: %v\n%s", err, out)
	}
	runtimeDoc, err := agentmeta.FromManifest(runtimeManifest)
	if err != nil {
		t.Fatalf("runtime agentmeta.FromManifest() = %v", err)
	}

	data, err := productData(opts)
	if err != nil {
		t.Fatal(err)
	}
	scaffoldManifest, err := productManifest(data)
	if err != nil {
		t.Fatalf("productManifest() = %v", err)
	}
	scaffoldDoc, err := agentmeta.FromManifest(scaffoldManifest)
	if err != nil {
		t.Fatalf("scaffold agentmeta.FromManifest() = %v", err)
	}

	got := normalizeAgentDocument(t, scaffoldDoc)
	want := normalizeAgentDocument(t, runtimeDoc)
	if string(got) != string(want) {
		t.Fatalf("scaffold-time agent metadata differs from runtime manifest projection\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func normalizeAgentDocument(t *testing.T, doc agentmeta.Document) []byte {
	t.Helper()
	out, err := output.StableJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func runGoInDir(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %v failed in %s: %v\n%s", args, dir, err, out)
	}
}

func onlineGoEnvForSkillTest() []string {
	return append(os.Environ(), "GOSUMDB=off", "GOFLAGS=-mod=mod")
}

func offlineGoEnvForSkillTest() []string {
	return append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOFLAGS=-mod=mod")
}
