package testutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// goldenDrift is the generic-file equivalent of docsgen.CheckResult. It keeps
// the reusable golden helper independent from docsgen's app-driven API.
type goldenDrift struct {
	Missing  []string
	Stale    []string
	Orphaned []string
}

func (d goldenDrift) ok() bool {
	return len(d.Missing) == 0 && len(d.Stale) == 0 && len(d.Orphaned) == 0
}

// AssertGoldenDir compares the want map (relative filename -> content) against
// the committed files under dir. When update is true it writes every want file
// (sorted, creating parents) and prunes any file under dir not in want;
// otherwise it fails with grouped missing, stale, and orphaned files plus a
// regenerate hint. dir is expected to contain only golden files of this set.
func AssertGoldenDir(t *testing.T, update bool, dir string, want map[string]string) {
	t.Helper()
	if update {
		if err := writeGoldenDir(dir, want); err != nil {
			t.Fatalf("write golden files in %s: %v", dir, err)
		}
		return
	}
	drift, err := checkGoldenDir(dir, want)
	if err != nil {
		t.Fatalf("check golden files in %s: %v", dir, err)
	}
	if !drift.ok() {
		t.Fatalf("golden files in %s are out of sync; regenerate with this package's -update test flag.\n%s",
			dir, formatPathBuckets(drift.Missing, drift.Stale, drift.Orphaned))
	}
}

// writeGoldenDir materializes a generic golden map on disk and removes files
// this map no longer owns. It intentionally mirrors docsgen.Write so help
// goldens and generated docs have the same drift semantics.
func writeGoldenDir(dir string, want map[string]string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	paths := make([]string, 0, len(want))
	for path := range want {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(want[path]), 0o644); err != nil {
			return err
		}
	}
	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if _, ok := want[filepath.ToSlash(rel)]; !ok {
			return os.Remove(path)
		}
		return nil
	}); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// checkGoldenDir compares a generic golden map to committed files without
// failing the caller's test. AssertGoldenDir owns the fatal wrapper; tests drive
// this function directly to cover failure buckets.
func checkGoldenDir(dir string, want map[string]string) (goldenDrift, error) {
	var drift goldenDrift
	for path, content := range want {
		got, err := os.ReadFile(filepath.Join(dir, path))
		switch {
		case errors.Is(err, os.ErrNotExist):
			drift.Missing = append(drift.Missing, path)
		case err != nil:
			return goldenDrift{}, err
		case string(got) != content:
			drift.Stale = append(drift.Stale, path)
		}
	}
	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if _, ok := want[rel]; !ok {
			drift.Orphaned = append(drift.Orphaned, rel)
		}
		return nil
	}); err != nil && !errors.Is(err, os.ErrNotExist) {
		return goldenDrift{}, err
	}
	sort.Strings(drift.Missing)
	sort.Strings(drift.Stale)
	sort.Strings(drift.Orphaned)
	return drift, nil
}

// formatPathBuckets prints only the non-empty drift classes so failure messages
// stay short while still naming the exact class of drift.
func formatPathBuckets(missing, stale, orphaned []string) string {
	var lines []string
	if len(missing) > 0 {
		lines = append(lines, fmt.Sprintf("missing: %s", strings.Join(missing, ", ")))
	}
	if len(stale) > 0 {
		lines = append(lines, fmt.Sprintf("stale: %s", strings.Join(stale, ", ")))
	}
	if len(orphaned) > 0 {
		lines = append(lines, fmt.Sprintf("orphaned: %s", strings.Join(orphaned, ", ")))
	}
	return strings.Join(lines, "\n")
}
