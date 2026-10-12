package conformance

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveTargetPathExplainsMissingTargets(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-tool")
	_, err := resolveTargetPath(missing)
	if err == nil || !strings.Contains(err.Error(), "target does not exist: ") {
		t.Fatalf("missing path error = %v, want a does-not-exist message", err)
	}
	if strings.Contains(err.Error(), "not executable") {
		t.Fatalf("missing path reported as not executable: %v", err)
	}
}

func TestResolveTargetPathExplainsUnknownName(t *testing.T) {
	_, err := resolveTargetPath("rungrad-no-such-command-xyz")
	if err == nil || !strings.Contains(err.Error(), "target is not found on PATH: rungrad-no-such-command-xyz") {
		t.Fatalf("bare name error = %v, want a not-found-on-PATH message", err)
	}
}

func TestResolveTargetPathRejectsDirectoryAndNonExecutable(t *testing.T) {
	dir := t.TempDir()
	if _, err := resolveTargetPath(dir); err == nil || !strings.Contains(err.Error(), "target is a directory") {
		t.Fatalf("directory error = %v", err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("executable bits are not checked on Windows")
	}
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveTargetPath(plain); err == nil || !strings.Contains(err.Error(), "target is not executable") {
		t.Fatalf("non-executable error = %v", err)
	}
}

func TestResolveTargetPathAcceptsExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bits are not checked on Windows")
	}
	exe := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveTargetPath(exe)
	if err != nil {
		t.Fatalf("resolveTargetPath(%q) error = %v", exe, err)
	}
	if got != exe {
		t.Fatalf("resolveTargetPath(%q) = %q", exe, got)
	}
}
