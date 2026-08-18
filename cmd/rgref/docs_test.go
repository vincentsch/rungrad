package main

import (
	"flag"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/vincentsch/rungrad/docsgen"
	"github.com/vincentsch/rungrad/testutil"
)

// updateGoldens regenerates committed rgref docs and help goldens instead of
// checking them. The flag string is "update" (so the command reads naturally)
// but the variable is not named `update`, which would collide with the imported
// update package in package main.
var updateGoldens = flag.Bool("update", false, "regenerate committed rgref docs and help goldens instead of checking them")

// TestGeneratedDocsInSync gates cmd/rgref/docs/ against the live command tree.
// Regenerate with:
//
//	go test ./cmd/rgref -run 'TestGeneratedDocsInSync|TestHelpGoldensInSync' -update -count=1
func TestGeneratedDocsInSync(t *testing.T) {
	testutil.AssertDocsInSync(t, newApp, *updateGoldens, "docs")
}

func TestHelpGoldensInSync(t *testing.T) {
	testutil.AssertHelpGoldens(t, newApp, *updateGoldens, filepath.Join("testdata", "help"))
}

func TestHelpDocsManifestConsistent(t *testing.T) {
	testutil.AssertConsistent(t, newApp)
}

// TestGeneratedDocsShape pins the exact page set, the metadata each page must
// carry, and the exclusion of hidden/synthetic commands. Combined with the drift
// gate (committed == generated), it proves the committed pages have this shape.
func TestGeneratedDocsShape(t *testing.T) {
	docs := docsgen.Generate(newApp())

	want := []string{
		"index.md",
		"rgref.md",
		"rgref_item.md",
		"rgref_item_create.md",
		"rgref_item_delete.md",
		"rgref_item_get.md",
		"rgref_item_list.md",
		"rgref_update.md",
		"rgref_whoami.md",
	}
	got := make([]string, 0, len(docs))
	for p := range docs {
		got = append(got, p)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("generated doc set = %v, want %v", got, want)
	}

	// No hidden command (the manifest endpoint) leaks into any page body.
	for path, body := range docs {
		if strings.Contains(body, "__rungrad_manifest") {
			t.Errorf("%s references the hidden manifest command:\n%s", path, body)
		}
	}
	// No synthetic help/completion or hidden command appears in the index command list.
	idx := docs["index.md"]
	for _, bad := range []string{"rgref_help.md", "rgref_completion.md", "__rungrad_manifest"} {
		if strings.Contains(idx, bad) {
			t.Errorf("index.md exposes a synthetic/hidden command %q:\n%s", bad, idx)
		}
	}

	// Root page: three root examples from cmd.Example, no related section.
	root := docs["rgref.md"]
	for _, ex := range []string{"rgref item list", "rgref item list --json", "rgref item create gamma --dry-run"} {
		if !strings.Contains(root, ex) {
			t.Errorf("rgref.md missing root example %q:\n%s", ex, root)
		}
	}
	if strings.Contains(root, "## Related commands") {
		t.Errorf("rgref.md should have no related section:\n%s", root)
	}

	// item create: the p09-001 --quiet example.
	if c := docs["rgref_item_create.md"]; !strings.Contains(c, "rgref item create gamma --quiet") {
		t.Errorf("rgref_item_create.md missing --quiet example:\n%s", c)
	}

	// update: rgref examples, no mytool, no related version block.
	upd := docs["rgref_update.md"]
	for _, ex := range []string{"rgref update --check", "rgref update --check --json", "rgref update"} {
		if !strings.Contains(upd, ex) {
			t.Errorf("rgref_update.md missing update example %q:\n%s", ex, upd)
		}
	}
	if strings.Contains(upd, "mytool") {
		t.Errorf("rgref_update.md leaks mytool examples:\n%s", upd)
	}
	if strings.Contains(upd, "## Related commands") {
		t.Errorf("rgref_update.md should not advertise a version related command:\n%s", upd)
	}

	// item delete: destructive section and the local --confirm flag.
	del := docs["rgref_item_delete.md"]
	for _, w := range []string{"## Destructive", "- `--confirm`"} {
		if !strings.Contains(del, w) {
			t.Errorf("rgref_item_delete.md missing %q:\n%s", w, del)
		}
	}

	// whoami: authentication section.
	if w := docs["rgref_whoami.md"]; !strings.Contains(w, "## Authentication") {
		t.Errorf("rgref_whoami.md missing authentication section:\n%s", w)
	}

	// item list: metadata section.
	if l := docs["rgref_item_list.md"]; !strings.Contains(l, "## Metadata") {
		t.Errorf("rgref_item_list.md missing metadata section:\n%s", l)
	}

	// Output modes for read/mutate/update commands.
	for _, page := range []string{
		"rgref_item_list.md",
		"rgref_item_get.md",
		"rgref_item_create.md",
		"rgref_item_delete.md",
		"rgref_whoami.md",
		"rgref_update.md",
	} {
		if !strings.Contains(docs[page], "## Output modes") {
			t.Errorf("%s missing output modes section:\n%s", page, docs[page])
		}
	}

	// index: the advanced global flags.
	if !strings.Contains(idx, "## Global flags") {
		t.Errorf("index.md missing global flags section:\n%s", idx)
	}
	gotFlags := []string{}
	for _, line := range strings.Split(idx, "\n") {
		if !strings.HasPrefix(line, "- `--") {
			continue
		}
		name := strings.TrimPrefix(line, "- `--")
		name = strings.SplitN(name, "`", 2)[0]
		gotFlags = append(gotFlags, "--"+name)
	}
	wantFlags := []string{
		"--api-url", "--auth-file", "--config", "--dry-run", "--include-meta", "--jq", "--json",
		"--no-ansi", "--no-color", "--no-pager", "--no-prompt", "--plain", "--profile", "--quiet", "--template",
	}
	if !reflect.DeepEqual(gotFlags, wantFlags) {
		t.Fatalf("index.md global flags = %v, want %v\n%s", gotFlags, wantFlags, idx)
	}
}
