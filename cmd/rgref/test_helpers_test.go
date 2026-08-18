package main

import (
	"path/filepath"
	"testing"

	"github.com/vincentsch/rungrad/testutil"
)

// rgrefNoEnv makes command tests ignore the developer's process environment.
func rgrefNoEnv(string) (string, bool) { return "", false }

// rgrefLookup exposes only the variables a test intends to exercise.
func rgrefLookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := values[name]
		return v, ok
	}
}

// isolatedRgrefOptions gives every in-process rgref command a private config
// home and a controlled env lookup, preventing host config or credentials from
// affecting test output.
func isolatedRgrefOptions(t *testing.T, opts testutil.Options) testutil.Options {
	t.Helper()
	if opts.LookupEnv == nil {
		opts.LookupEnv = rgrefNoEnv
	}
	if opts.UserConfigDir == nil {
		dir := t.TempDir()
		opts.UserConfigDir = func() (string, error) { return dir, nil }
	}
	return opts
}

// runRgref executes rgref with the default hermetic test options.
func runRgref(t *testing.T, args ...string) testutil.Result {
	t.Helper()
	return testutil.RunWith(newApp(), isolatedRgrefOptions(t, testutil.Options{}), args...)
}

// runRgrefWith executes rgref with explicit options layered onto the hermetic
// defaults, so tests can opt into only the hooks they need.
func runRgrefWith(t *testing.T, opts testutil.Options, args ...string) testutil.Result {
	t.Helper()
	return testutil.RunWith(newApp(), isolatedRgrefOptions(t, opts), args...)
}

// missingConfig returns a nonexistent --config path in a fresh temp dir, so
// resolution loads an empty config without touching the user's real config dir.
func missingConfig(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "none.yaml")
}
