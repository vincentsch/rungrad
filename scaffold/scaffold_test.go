package scaffold_test

import (
	"errors"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vincentsch/rungrad/scaffold"
)

func TestGenerateProducesProject(t *testing.T) {
	files, err := scaffold.Generate(scaffold.Options{Name: "mytool"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"go.mod", "main.go", "main_test.go", "README.md"} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing %s", want)
		}
	}
	if !strings.Contains(files["main.go"], "package main") {
		t.Error("main.go does not look like Go")
	}
	if strings.Contains(files["main.go"], "{{.") {
		t.Errorf("unrendered template directive in main.go:\n%s", files["main.go"])
	}
	if !strings.Contains(files["go.mod"], "module example.com/mytool") {
		t.Errorf("unexpected go.mod:\n%s", files["go.mod"])
	}
	if _, err := format.Source([]byte(files["main.go"])); err != nil {
		t.Fatalf("generated main.go is not gofmt-clean Go: %v\n%s", err, files["main.go"])
	}
}

func TestGenerateSanitizesEnvVar(t *testing.T) {
	files, err := scaffold.Generate(scaffold.Options{Name: "my-tool"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(files["main.go"], `EnvVar:  "MY_TOOL_TOKEN"`) {
		t.Fatalf("expected shell-safe env var in main.go:\n%s", files["main.go"])
	}
}

func TestGenerateRejectsInvalidNames(t *testing.T) {
	for _, name := range []string{"", "my tool", "../x", "Mytool", "-bad", "bad_name"} {
		_, err := scaffold.Generate(scaffold.Options{Name: name})
		var validation *scaffold.ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("Generate(%q) error = %T %v, want ValidationError", name, err, err)
		}
	}
}

func TestGenerateRejectsInvalidModule(t *testing.T) {
	_, err := scaffold.Generate(scaffold.Options{Name: "mytool", Module: "example.com/my tool"})
	var validation *scaffold.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("Generate invalid module error = %T %v, want ValidationError", err, err)
	}
}

func TestGenerateEmitsFinalContract(t *testing.T) {
	files, err := scaffold.Generate(scaffold.Options{Name: "mytool"})
	if err != nil {
		t.Fatal(err)
	}
	// These content checks catch template regressions before the slower
	// scaffold-build test compiles the generated project.
	main := files["main.go"]
	for _, want := range []string{"Destructive: true", "ConfirmDestructive", "update.Command(", "update.CommandConfig{"} {
		if !strings.Contains(main, want) {
			t.Errorf("generated main.go missing %q", want)
		}
	}
	if strings.Contains(main, "rungrad/resolve") {
		t.Errorf("generated main.go should not import the resolve package:\n%s", main)
	}
	test := files["main_test.go"]
	for _, want := range []string{"__rungrad_manifest", "manifest.Validate", "testutil.AssertConsistent"} {
		if !strings.Contains(test, want) {
			t.Errorf("generated main_test.go missing %q", want)
		}
	}
}

func TestGenerateUsesGeneratedToolName(t *testing.T) {
	files, err := scaffold.Generate(scaffold.Options{Name: "democtl"})
	if err != nil {
		t.Fatal(err)
	}
	// Use a non-mytool name so the test proves update examples come from the
	// generated project name, not update.Command's generic fallback.
	main := files["main.go"]
	if !strings.Contains(main, `ToolName:       "democtl"`) {
		t.Errorf("generated main.go should set update ToolName to the generated name:\n%s", main)
	}
	test := files["main_test.go"]
	for _, want := range []string{"democtl update --check", "democtl update"} {
		if !strings.Contains(test, want) {
			t.Errorf("generated main_test.go missing templated update example %q", want)
		}
	}
	// The generated tests enforce that the update command has no related version entry.
	if !strings.Contains(test, "no version subcommand") {
		t.Errorf("generated tests should assert the update command has no related version subcommand")
	}
	// The "mytool" fallback in update.CommandConfig must never leak into a
	// non-mytool project.
	for name, file := range map[string]string{"main.go": main, "main_test.go": test} {
		if strings.Contains(file, "mytool") {
			t.Errorf("non-mytool scaffold leaked the mytool fallback in %s:\n%s", name, file)
		}
	}
}

func TestProductProfileGenerate(t *testing.T) {
	files, err := scaffold.Generate(scaffold.Options{
		Name:              "acmectl",
		Module:            "example.com/acme/acmectl",
		ProductProfile:    true,
		EnvPrefix:         "ACME",
		ProductName:       "Acme Control",
		Description:       "Manage Acme services",
		Services:          []string{"api=https://api.example.invalid", "billing-api=https://billing.example.invalid"},
		MetadataNamespace: "example.com/acme",
		Surface:           "host",
		ReleaseOwner:      "example",
		ReleaseRepo:       "acmectl",
		DocsLabel:         "Acme CLI",
		Examples:          []string{"acmectl widget list"},
	})
	if err != nil {
		t.Fatal(err)
	}
	main := files["main.go"]
	for _, want := range []string{
		`EnvVar:  "ACME_TOKEN"`,
		`ProfileEnvVar:  "ACME_PROFILE"`,
		`AuthFileEnvVar: "ACME_AUTH_FILE"`,
		`ConfigEnvVar:   "ACME_CONFIG"`,
		`Name: "api", Flag: "api-url", EnvVar: "ACME_API_URL", ConfigKey: "api_url", Default: "https://api.example.invalid"`,
		`Name: "billing-api", Flag: "billing-api-url", EnvVar: "ACME_BILLING_API_URL", ConfigKey: "billing_api_url", Default: "https://billing.example.invalid"`,
		`SurfaceHostOwned`,
		`"api":`,
		`{Name: "api-url"}`,
		`"billing-api": {Name: "billing-api-url"}`,
		`"example.com/acme":`,
		`Short:   "Acme Control"`,
		`f.Service("api")`,
	} {
		requireContains(t, main, want, "main.go")
	}
	if _, err := format.Source([]byte(main)); err != nil {
		t.Fatalf("generated product main.go is not gofmt-clean Go: %v\n%s", err, main)
	}
	readme := files["README.md"]
	for _, want := range []string{
		"# Acme CLI",
		"| `api` | `--api-url` | `ACME_API_URL` | `api_url` | `https://api.example.invalid` |",
		"| `billing-api` | `--billing-api-url` | `ACME_BILLING_API_URL` | `billing_api_url` | `https://billing.example.invalid` |",
		`update.GitHubFetcher{Owner: "example", Repo:`,
		`"acmectl"}`,
	} {
		requireContains(t, readme, want, "README.md")
	}
	requireContains(t, files["go.mod"], "module example.com/acme/acmectl", "go.mod")
}

func TestProductProfileDefaultsGenerate(t *testing.T) {
	files, err := scaffold.Generate(scaffold.Options{Name: "acmectl", ProductProfile: true})
	if err != nil {
		t.Fatal(err)
	}
	main := files["main.go"]
	for _, want := range []string{
		`EnvVar:  "ACMECTL_TOKEN"`,
		`EnvVar: "ACMECTL_API_URL"`,
		`"example.com/acmectl":`,
		`Owner: "example", Repo: "acmectl"`,
	} {
		requireContains(t, main, want, "main.go")
	}
	if strings.Contains(main, "SurfaceHostOwned") {
		t.Fatalf("default product profile should be rungrad-owned, got host-owned surface:\n%s", main)
	}
	readme := files["README.md"]
	for _, want := range []string{
		"# acmectl CLI",
		`update.GitHubFetcher{Owner: "example", Repo:`,
		`"acmectl"}`,
	} {
		requireContains(t, readme, want, "README.md")
	}
}

func TestProductProfileExamplesRouteToLeaves(t *testing.T) {
	files, err := scaffold.Generate(scaffold.Options{
		Name:           "acmectl",
		ProductProfile: true,
		Examples: []string{
			"acmectl widget list --api-url https://api.example.invalid",
			"acmectl widget create delta --dry-run",
			"acmectl widget delete delta --dry-run",
			"acmectl update --check",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	main := files["main.go"]
	for _, want := range []string{
		`app.Root().Example = "acmectl widget list\nacmectl widget list --json\nacmectl widget create gamma --dry-run\nacmectl widget list --api-url https://api.example.invalid\nacmectl widget create delta --dry-run\nacmectl widget delete delta --dry-run\nacmectl update --check"`,
		`Examples:    []string{"acmectl widget list", "acmectl widget list --json", "acmectl widget list --api-url https://api.example.invalid"}`,
		`Examples:    []string{"acmectl widget create gamma", "acmectl widget create gamma --dry-run", "acmectl widget create delta --dry-run"}`,
		`Examples:    []string{"acmectl widget delete alpha --dry-run", "acmectl widget delete alpha --confirm", "acmectl widget delete delta --dry-run"}`,
		`upd.Examples = append(upd.Examples, "acmectl update --check")`,
	} {
		requireContains(t, main, want, "main.go")
	}
}

func TestProductProfileRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		opts scaffold.Options
	}{
		{name: "bad explicit env prefix lowercase", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, EnvPrefix: "acme"}},
		{name: "bad explicit env prefix trailing underscore", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, EnvPrefix: "ACME_"}},
		{name: "bad explicit env prefix starts digit", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, EnvPrefix: "1ABC"}},
		{name: "bad service URL scheme", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Services: []string{"api=http://api.example.invalid"}}},
		{name: "bad service URL domain", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Services: []string{"api=https://api.example.com"}}},
		{name: "bad service URL userinfo", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Services: []string{"api=https://u@api.example.invalid"}}},
		{name: "duplicate services", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Services: []string{"api=https://api.example.invalid", "api=https://api2.example.invalid"}}},
		{name: "bad metadata namespace", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, MetadataNamespace: "rungrad/bad"}},
		{name: "bad release owner uppercase", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, ReleaseOwner: "Example"}},
		{name: "bad release owner slash", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, ReleaseOwner: "a/b"}},
		{name: "bad release repo scheme", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, ReleaseRepo: "https://x"}},
		{name: "bad release owner secret", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, ReleaseOwner: "ghp_secretlookingtoken"}},
		{name: "bad release repo secret", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, ReleaseRepo: "github_pat_secretlookingtoken"}},
		{name: "bad example binary", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Examples: []string{"other widget list"}}},
		{name: "bad example incomplete widget", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Examples: []string{"acmectl widget"}}},
		{name: "bad example unknown command", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Examples: []string{"acmectl bogus"}}},
		{name: "bad example list positional", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Examples: []string{"acmectl widget list extra"}}},
		{name: "bad example list trailing positional after flag", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Examples: []string{"acmectl widget list --json extra"}}},
		{name: "bad example create no arg", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Examples: []string{"acmectl widget create --json"}}},
		{name: "bad example create trailing positional", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Examples: []string{"acmectl widget create delta extra"}}},
		{name: "bad example delete no arg", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Examples: []string{"acmectl widget delete --dry-run"}}},
		{name: "bad example delete trailing positional", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Examples: []string{"acmectl widget delete delta extra"}}},
		{name: "bad example update positional", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Examples: []string{"acmectl update latest"}}},
		{name: "bad example update trailing positional after flag", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Examples: []string{"acmectl update --check extra"}}},
		{name: "bad surface", opts: scaffold.Options{Name: "acmectl", ProductProfile: true, Surface: "mixed"}},
		{name: "env prefix without profile", opts: scaffold.Options{Name: "acmectl", EnvPrefix: "ACME"}},
		{name: "product name without profile", opts: scaffold.Options{Name: "acmectl", ProductName: "Acme Control"}},
		{name: "description without profile", opts: scaffold.Options{Name: "acmectl", Description: "Manage Acme services"}},
		{name: "docs label without profile", opts: scaffold.Options{Name: "acmectl", DocsLabel: "Acme CLI"}},
		{name: "service without profile", opts: scaffold.Options{Name: "acmectl", Services: []string{"api=https://api.example.invalid"}}},
		{name: "metadata namespace without profile", opts: scaffold.Options{Name: "acmectl", MetadataNamespace: "example.com/acme"}},
		{name: "surface without profile", opts: scaffold.Options{Name: "acmectl", Surface: "host"}},
		{name: "release owner without profile", opts: scaffold.Options{Name: "acmectl", ReleaseOwner: "example"}},
		{name: "release repo without profile", opts: scaffold.Options{Name: "acmectl", ReleaseRepo: "acmectl"}},
		{name: "example without profile", opts: scaffold.Options{Name: "acmectl", Examples: []string{"acmectl widget list"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := scaffold.Generate(tt.opts)
			var validation *scaffold.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Generate error = %T %v, want ValidationError", err, err)
			}
		})
	}
}

func TestProductProfileNoStalePlaceholders(t *testing.T) {
	files, err := scaffold.Generate(scaffold.Options{
		Name:           "explicitctl",
		ProductProfile: true,
		EnvPrefix:      "ACME",
		ProductName:    "Acme Control",
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if strings.Contains(content, "mytool") {
			t.Fatalf("%s leaked generic mytool placeholder:\n%s", name, content)
		}
		if strings.Contains(content, "explicitctl CLI") {
			t.Fatalf("%s leaked generic default label:\n%s", name, content)
		}
		if strings.Contains(content, "EXPLICITCTL") {
			t.Fatalf("%s leaked derived env prefix despite explicit override:\n%s", name, content)
		}
		if strings.Contains(content, "ghp_") || strings.Contains(content, "github_pat_") || strings.Contains(content, "sk_") {
			t.Fatalf("%s contains unexpected secret-looking token:\n%s", name, content)
		}
	}
	for _, name := range []string{"main.go", "README.md"} {
		if !strings.Contains(files[name], "Acme Control") {
			t.Fatalf("%s missing supplied product name:\n%s", name, files[name])
		}
		if !strings.Contains(files[name], "ACME") {
			t.Fatalf("%s missing supplied env prefix:\n%s", name, files[name])
		}
	}
	for _, line := range strings.Split(files["main.go"], "\n") {
		if strings.Contains(line, "update.GitHubFetcher") && !strings.HasPrefix(strings.TrimSpace(line), "//") {
			t.Fatalf("main.go has uncommented update.GitHubFetcher line: %q", line)
		}
	}
}

// TestScaffoldedProjectBuildsAndTests proves the generated project compiles and
// its own tests pass, using a local replace to the rungrad module under test.
func TestScaffoldedProjectBuildsAndTests(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	root, err := scaffold.Write(t.TempDir(), scaffold.Options{Name: "mytool", RungradReplace: repoRoot})
	if err != nil {
		t.Fatal(err)
	}
	env := prepareGeneratedModule(t, root)

	gofmt := exec.Command("gofmt", "-l", ".")
	gofmt.Dir = root
	if out, err := gofmt.CombinedOutput(); err != nil {
		t.Fatalf("gofmt failed: %v\n%s", err, out)
	} else if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("generated project is not gofmt-clean:\n%s", out)
	}

	test := exec.Command("go", "test", "./...")
	test.Dir = root
	test.Env = env
	if out, err := test.CombinedOutput(); err != nil {
		t.Fatalf("scaffolded project tests failed: %v\n%s", err, out)
	}
}

func prepareGeneratedModule(t *testing.T, dir string) []string {
	t.Helper()
	runGo(t, dir, onlineGoEnv(), "mod", "tidy")
	env := offlineGoEnv()
	runGo(t, dir, env, "mod", "tidy")
	return env
}

func runGo(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

func onlineGoEnv() []string {
	return append(os.Environ(), "GOSUMDB=off", "GOFLAGS=-mod=mod")
}

func offlineGoEnv() []string {
	return append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOFLAGS=-mod=mod")
}

func requireContains(t *testing.T, got, want, label string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("%s missing %q:\n%s", label, want, got)
	}
}
