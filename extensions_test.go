package rungrad_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/manifest"
	"github.com/vincentsch/rungrad/testutil"
)

const rootExtensionNS = "example.com/product"

type extensionModule struct {
	commandExt manifest.ExtensionSet
	specExt    manifest.ExtensionSet
}

func (extensionModule) Groups() []rungrad.Group { return nil }

func (m extensionModule) Commands() []*rungrad.Command {
	return []*rungrad.Command{{
		Use:         "read",
		Short:       "Read data",
		OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
		Extensions:  m.commandExt,
		Run:         noopRootRun,
	}}
}

func (m extensionModule) Catalog() []rungrad.CommandSpec {
	return []rungrad.CommandSpec{{
		Path:        "read",
		Summary:     "Read data",
		OutputModes: []string{rungrad.OutputModeHuman, rungrad.OutputModeJSON},
		Extensions:  m.specExt,
	}}
}

type extensionSpecOnlyModule struct {
	specExt manifest.ExtensionSet
}

func (extensionSpecOnlyModule) Groups() []rungrad.Group      { return nil }
func (extensionSpecOnlyModule) Commands() []*rungrad.Command { return nil }
func (m extensionSpecOnlyModule) Catalog() []rungrad.CommandSpec {
	return []rungrad.CommandSpec{{
		Path:       "update",
		Summary:    "Update metadata",
		Extensions: m.specExt,
	}}
}

func extensionFixtureApp(commandExt, specExt manifest.ExtensionSet) *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{Name: "rgext", Short: "extension test CLI"})
	app.AddModule(extensionModule{commandExt: commandExt, specExt: specExt})
	updateExt := manifest.ExtensionSet{rootExtensionNS: {
		"owner":     "release",
		"status":    "stable",
		"docs_path": "docs/update.md",
		"audit":     []string{"dry-run"},
	}}
	app.AddCommand(&rungrad.Command{
		Use:        "update",
		Short:      "Update metadata",
		Extensions: updateExt,
		Run:        noopRootRun,
	})
	app.AddModule(extensionSpecOnlyModule{specExt: updateExt})
	return app
}

func matchingRootExtensions() manifest.ExtensionSet {
	return manifest.ExtensionSet{rootExtensionNS: {
		"owner":     "platform",
		"status":    "beta",
		"docs_path": "docs/read.md",
		"audit": map[string]any{
			"confirmation": false,
			"notes":        []string{"safe", "reviewed"},
		},
	}}
}

func sharedPrefixRootExtensionSlice() []any {
	s := make([]any, 2)
	s[0] = "x"
	s[1] = s[:1]
	return s
}

func TestCommandExtensionsManifestEndpointAndCatalog(t *testing.T) {
	ext := matchingRootExtensions()
	app := extensionFixtureApp(ext, ext)
	first := testutil.Run(app, "__rungrad_manifest")
	second := testutil.Run(extensionFixtureApp(ext, ext), "__rungrad_manifest")
	if first.Exit != rungrad.ExitSuccess || second.Exit != rungrad.ExitSuccess {
		t.Fatalf("manifest exits = %d/%d stderr=%q/%q", first.Exit, second.Exit, first.Stderr, second.Stderr)
	}
	if first.Stdout != second.Stdout {
		t.Fatalf("manifest output not repeatable:\n%s\n---\n%s", first.Stdout, second.Stdout)
	}
	var m manifest.Manifest
	if err := first.JSON(&m); err != nil {
		t.Fatalf("manifest JSON: %v\n%s", err, first.Stdout)
	}
	if err := manifest.Validate(&m); err != nil {
		t.Fatalf("Validate(manifest) = %v", err)
	}
	read := findManifestCommand(&m, "read")
	if read == nil {
		t.Fatal("manifest missing read command")
	}
	if got := read.Extensions[rootExtensionNS]["owner"]; got != "platform" {
		t.Fatalf("read extension owner = %#v", got)
	}
	if got := read.Extensions[rootExtensionNS]["docs_path"]; got != "docs/read.md" {
		t.Fatalf("read extension docs_path = %#v", got)
	}
	if err := app.ValidateCatalog(); err != nil {
		t.Fatalf("ValidateCatalog() = %v", err)
	}

	doc, err := app.ManifestDocumentChecked()
	if err != nil {
		t.Fatalf("ManifestDocumentChecked() = %v", err)
	}
	checkedRead := findManifestCommand(&doc, "read")
	if checkedRead == nil || checkedRead.Extensions[rootExtensionNS]["status"] != "beta" {
		t.Fatalf("checked manifest read = %+v", checkedRead)
	}
	if built := app.ManifestDocument(); findManifestCommand(&built, "read").Extensions[rootExtensionNS]["owner"] != "platform" {
		t.Fatalf("ManifestDocument() extensions = %+v", built.Commands)
	}
}

func TestCompactManifestOmitsExtensions(t *testing.T) {
	res := testutil.Run(demoApp(), "__rungrad_manifest")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("manifest exit %d stderr=%q", res.Exit, res.Stderr)
	}
	if strings.Contains(res.Stdout, `"extensions"`) {
		t.Fatalf("compact manifest contains extensions:\n%s", res.Stdout)
	}
}

func TestCommandExtensionEmptyArrayRoundTrip(t *testing.T) {
	ext := manifest.ExtensionSet{rootExtensionNS: {
		"owner": "platform",
		"tags":  []string{},
	}}
	app := extensionFixtureApp(ext, ext)
	doc, err := app.ManifestDocumentChecked()
	if err != nil {
		t.Fatalf("ManifestDocumentChecked() = %v", err)
	}
	read := findManifestCommand(&doc, "read")
	if read == nil {
		t.Fatal("manifest missing read command")
	}
	tags, ok := read.Extensions[rootExtensionNS]["tags"].([]any)
	if !ok || tags == nil || len(tags) != 0 {
		t.Fatalf("tags extension = %#v, want decoded non-nil []any{}", read.Extensions[rootExtensionNS]["tags"])
	}
	if err := app.ValidateCatalog(); err != nil {
		t.Fatalf("ValidateCatalog() = %v", err)
	}
}

func TestCommandExtensionCatalogFailures(t *testing.T) {
	match := matchingRootExtensions()
	tests := []struct {
		name       string
		commandExt manifest.ExtensionSet
		specExt    manifest.ExtensionSet
		want       string
	}{
		{
			name:       "drift",
			commandExt: match,
			specExt: manifest.ExtensionSet{rootExtensionNS: {
				"owner":     "platform",
				"status":    "stable",
				"docs_path": "docs/read.md",
				"audit":     map[string]any{"confirmation": false, "notes": []string{"safe", "reviewed"}},
			}},
			want: "extensions",
		},
		{
			name:       "spec only",
			commandExt: nil,
			specExt:    match,
			want:       "extensions",
		},
		{
			name:       "invalid spec",
			commandExt: match,
			specExt:    manifest.ExtensionSet{rootExtensionNS: {"supports_dry_run": true}},
			want:       "has invalid extensions",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := extensionFixtureApp(tt.commandExt, tt.specExt)
			err := app.ValidateCatalog()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateCatalog() = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestCatalogExtensionsAreDeepCopied(t *testing.T) {
	specExt := manifest.ExtensionSet{rootExtensionNS: {
		"tags":        []string{"stable", "owned"},
		"weights":     []int{1, 2},
		"labels":      map[string]string{"tier": "gold"},
		"nested":      map[string][]string{"teams": {"core", "docs"}},
		"mixed":       []any{map[string]any{"name": "alpha"}, []string{"one"}},
		"shared":      sharedPrefixRootExtensionSlice(),
		"scalar_note": "keep",
	}}
	app := extensionFixtureApp(specExt, specExt)
	if err := app.ValidateCatalog(); err != nil {
		t.Fatalf("ValidateCatalog() = %v", err)
	}

	catalog := app.Catalog()
	var readSpec *rungrad.CommandSpec
	for i := range catalog {
		if catalog[i].Path == "read" {
			readSpec = &catalog[i]
		}
	}
	if readSpec == nil {
		t.Fatalf("read spec missing from catalog: %+v", catalog)
	}
	readSpec.Extensions[rootExtensionNS]["tags"].([]string)[0] = "mutated"
	readSpec.Extensions[rootExtensionNS]["weights"].([]int)[0] = 99
	readSpec.Extensions[rootExtensionNS]["labels"].(map[string]string)["tier"] = "mutated"
	readSpec.Extensions[rootExtensionNS]["nested"].(map[string][]string)["teams"][0] = "mutated"
	readSpec.Extensions[rootExtensionNS]["mixed"].([]any)[0].(map[string]any)["name"] = "mutated"
	readSpec.Extensions[rootExtensionNS]["mixed"].([]any)[1].([]string)[0] = "mutated"
	readSpec.Extensions[rootExtensionNS]["shared"].([]any)[0] = "mutated"

	second := app.Catalog()
	var secondRead rungrad.CommandSpec
	for _, spec := range second {
		if spec.Path == "read" {
			secondRead = spec
		}
	}
	got := secondRead.Extensions[rootExtensionNS]
	if got["tags"].([]string)[0] != "stable" ||
		got["weights"].([]int)[0] != 1 ||
		got["labels"].(map[string]string)["tier"] != "gold" ||
		got["nested"].(map[string][]string)["teams"][0] != "core" ||
		got["mixed"].([]any)[0].(map[string]any)["name"] != "alpha" ||
		got["mixed"].([]any)[1].([]string)[0] != "one" ||
		got["shared"].([]any)[0] != "x" ||
		got["shared"].([]any)[1].([]any)[0] != "x" {
		t.Fatalf("Catalog returned mutable extension state: %#v", got)
	}
}

func TestMalformedRawExtensionAnnotationCheckedPaths(t *testing.T) {
	ext := matchingRootExtensions()
	app := extensionFixtureApp(ext, ext)
	read, _, err := app.Root().Find([]string{"read"})
	if err != nil {
		t.Fatalf("find read: %v", err)
	}
	read.Annotations[rungrad.AnnotationExtensions] = `{"Example.com/product":{},"Example.com/product":{}}`

	_, err = app.ManifestDocumentChecked()
	if err == nil || !strings.Contains(err.Error(), `command "read": invalid rungrad.extensions annotation`) || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("ManifestDocumentChecked() = %v", err)
	}
	res := testutil.Run(app, "__rungrad_manifest")
	if res.Exit == rungrad.ExitSuccess || res.Stdout != "" || !strings.Contains(res.Stderr, "invalid rungrad.extensions annotation") {
		t.Fatalf("manifest endpoint result = %#v", res)
	}
	assertPanicContains(t, "invalid rungrad.extensions annotation", func() {
		_ = app.ManifestDocument()
	})
	if err := app.ValidateCatalog(); err == nil || !strings.Contains(err.Error(), "invalid rungrad.extensions annotation") {
		t.Fatalf("ValidateCatalog() = %v", err)
	}
}

func TestExtensionRequireHelpersAgainstManifestDocument(t *testing.T) {
	ext := matchingRootExtensions()
	app := extensionFixtureApp(ext, ext)
	doc := app.ManifestDocument()
	read := findManifestCommand(&doc, "read")
	if read == nil {
		t.Fatal("missing read command")
	}
	if err := manifest.RequireExtensionFields(read.Extensions, rootExtensionNS, "owner", "status", "docs_path", "audit"); err != nil {
		t.Fatalf("RequireExtensionFields() = %v", err)
	}
	if err := manifest.RequireExtensionEnum(read.Extensions, rootExtensionNS, "status", "alpha", "beta", "stable"); err != nil {
		t.Fatalf("RequireExtensionEnum() = %v", err)
	}
	if err := manifest.RequireExtensionDocPath(read.Extensions, rootExtensionNS, "docs_path"); err != nil {
		t.Fatalf("RequireExtensionDocPath() = %v", err)
	}
}

func TestCommandExtensionBuildPanicsOnInvalidTypedValues(t *testing.T) {
	tests := []struct {
		name string
		ext  manifest.ExtensionSet
		want string
	}{
		{
			name: "core field",
			ext:  manifest.ExtensionSet{rootExtensionNS: {"requires_auth": true}},
			want: "invalid extensions",
		},
		{
			name: "raw message",
			ext:  manifest.ExtensionSet{rootExtensionNS: {"raw": json.RawMessage(`null`)}},
			want: "custom JSON marshaler",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := rungrad.New(rungrad.AppConfig{Name: "rgext", Short: "extension test CLI"})
			assertPanicContains(t, tt.want, func() {
				app.AddCommand(&rungrad.Command{
					Use:        "bad",
					Extensions: tt.ext,
					Run:        noopRootRun,
				})
			})
		})
	}
}

func noopRootRun(f *rungrad.Factory, cmd *cobra.Command, args []string) error { return nil }

func TestCanonicalExtensionCatalogComparisonIgnoresDecodedNumberType(t *testing.T) {
	spec := manifest.ExtensionSet{rootExtensionNS: {"large": int64(9007199254740993)}}
	app := extensionFixtureApp(spec, spec)
	doc, err := app.ManifestDocumentChecked()
	if err != nil {
		t.Fatalf("ManifestDocumentChecked() = %v", err)
	}
	read := findManifestCommand(&doc, "read")
	if read == nil {
		t.Fatal("missing read command")
	}
	if reflect.TypeOf(read.Extensions[rootExtensionNS]["large"]).String() != "json.Number" {
		t.Fatalf("manifest large type = %T", read.Extensions[rootExtensionNS]["large"])
	}
	if err := app.ValidateCatalog(); err != nil {
		t.Fatalf("ValidateCatalog() = %v", err)
	}
}
