package conformance

import (
	"fmt"
	"strings"

	"github.com/vincentsch/rungrad/manifest"
)

// manifestActive is the single gate every probe-facing manifest read passes
// through. It marks the current rule as manifest-backed when a validated
// manifest is cached.
func (r *Runner) manifestActive() (*manifest.Manifest, bool) {
	if r.manifestSummary.Status != ManifestPresent || r.cachedManifest == nil {
		return nil, false
	}
	r.manifestConsulted = true
	return r.cachedManifest, true
}

// manifestFixture resolves a configured fixture command against a cached,
// validated manifest. When no manifest is active, callers skip pre-checks.
func (r *Runner) manifestFixture(fixture []string) (entry *manifest.Command, res ProbeResult, stop bool) {
	m, ok := r.manifestActive()
	if !ok {
		return nil, ProbeResult{}, false
	}
	c, matched := manifestCommandFor(m, fixture)
	if !matched {
		return nil, fail(fmt.Sprintf("manifest lists no command matching configured fixture %q", strings.Join(fixture, " "))), true
	}
	return c, ProbeResult{}, false
}

// manifestGlobalFlag reports whether the manifest's top-level global_flags
// lists name. active is false when no validated manifest is cached.
func (r *Runner) manifestGlobalFlag(name string) (present, active bool) {
	m, ok := r.manifestActive()
	if !ok {
		return false, false
	}
	return hasGlobalFlag(m, name), true
}

// manifestCommandFor maps a fixture, which may include example args or flags, to
// the manifest command with the longest matching command-path prefix. A
// non-empty fixture never falls back to the root entry.
func manifestCommandFor(m *manifest.Manifest, fixture []string) (*manifest.Command, bool) {
	var best *manifest.Command
	for i := range m.Commands {
		c := &m.Commands[i]
		if len(c.Path) == 0 || len(c.Path) > len(fixture) {
			continue
		}
		if !equalPath(c.Path, fixture[:len(c.Path)]) {
			continue
		}
		if best == nil || len(c.Path) > len(best.Path) {
			best = c
		}
	}
	if best == nil {
		return nil, false
	}
	return best, true
}

func equalPath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hasOutputMode(c *manifest.Command, mode string) bool {
	for _, m := range c.OutputModes {
		if m == mode {
			return true
		}
	}
	return false
}

// hasHumanOutputMode reports whether the command declares any non-json output
// mode.
func hasHumanOutputMode(c *manifest.Command) bool {
	for _, m := range c.OutputModes {
		if m != "json" {
			return true
		}
	}
	return false
}

func hasGlobalFlag(m *manifest.Manifest, name string) bool {
	for _, f := range m.GlobalFlags {
		if f.Name == name {
			return true
		}
	}
	return false
}
