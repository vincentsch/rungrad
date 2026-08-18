package main

import (
	"reflect"
	"testing"
)

// TestReferenceCatalogCoversVisibleCommands pins the reference catalog's exact
// path set and runs whole-surface catalog validation directly, so adding or
// removing a command (or its catalog row) without keeping the two in sync fails
// here with a cmd/rgref-local message instead of only inside the generic
// help/docs/manifest/catalog consistency dump. The exhaustive per-field
// catalog-drift matrix lives in the framework module_test.go and is not repeated.
func TestReferenceCatalogCoversVisibleCommands(t *testing.T) {
	app := newApp()

	got := make([]string, 0, len(app.Catalog()))
	for _, spec := range app.Catalog() {
		got = append(got, spec.Path)
	}
	// app.Catalog() returns rows sorted by Path, so got is already sorted.
	want := []string{"item", "item create", "item delete", "item get", "item list", "update", "whoami"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog paths = %v, want %v", got, want)
	}

	if err := app.ValidateCatalog(); err != nil {
		t.Fatalf("ValidateCatalog() = %v", err)
	}
}
