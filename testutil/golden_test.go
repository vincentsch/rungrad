package testutil

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCheckGoldenDirBuckets(t *testing.T) {
	want := map[string]string{
		"a.txt":        "a\n",
		"b.txt":        "b\n",
		"nested/c.txt": "c\n",
	}
	dir := t.TempDir()
	if err := writeGoldenDir(dir, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	drift, err := checkGoldenDir(dir, want)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !drift.ok() {
		t.Fatalf("write then check drifted: %+v", drift)
	}

	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	drift, err = checkGoldenDir(dir, want)
	if err != nil {
		t.Fatalf("check stale: %v", err)
	}
	if !reflect.DeepEqual(drift.Stale, []string{"b.txt"}) || len(drift.Missing) != 0 || len(drift.Orphaned) != 0 {
		t.Fatalf("stale drift = %+v", drift)
	}

	if err := writeGoldenDir(dir, want); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	for _, path := range []string{"z.txt", "old.txt"} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	drift, err = checkGoldenDir(dir, want)
	if err != nil {
		t.Fatalf("check orphaned: %v", err)
	}
	if !reflect.DeepEqual(drift.Orphaned, []string{"old.txt", "z.txt"}) || len(drift.Missing) != 0 || len(drift.Stale) != 0 {
		t.Fatalf("orphaned drift = %+v", drift)
	}

	if err := writeGoldenDir(dir, want); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	for _, path := range []string{"b.txt", "a.txt"} {
		if err := os.Remove(filepath.Join(dir, path)); err != nil {
			t.Fatal(err)
		}
	}
	drift, err = checkGoldenDir(dir, want)
	if err != nil {
		t.Fatalf("check missing: %v", err)
	}
	if !reflect.DeepEqual(drift.Missing, []string{"a.txt", "b.txt"}) || len(drift.Stale) != 0 || len(drift.Orphaned) != 0 {
		t.Fatalf("missing drift = %+v", drift)
	}
}

func TestWriteGoldenDirPrunes(t *testing.T) {
	dir := t.TempDir()
	want := map[string]string{"fresh.txt": "fresh\n"}
	if err := writeGoldenDir(dir, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	orphan := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(orphan, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeGoldenDir(dir, want); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan stat error = %v, want not exist", err)
	}
}
