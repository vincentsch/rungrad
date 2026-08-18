// Package update keeps an installed rungrad tool current and lets callers check
// for updates without mutating the binary. Evaluation is a pure function over a
// fetched release, so the agent and CI safe "--check" path is fully testable
// offline; the apply path that replaces the executable is injectable.
package update

import (
	"strconv"
	"strings"
)

// Status describes the relationship between the running build and the latest
// release.
type Status string

const (
	StatusUpToDate         Status = "up_to_date"
	StatusUpdateAvailable  Status = "update_available"
	StatusDevelopmentBuild Status = "development_build"
	StatusNewerThanLatest  Status = "newer_than_latest"
	StatusUnknownLatest    Status = "unknown_latest"
)

// Asset is a downloadable artifact attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Release is a published release of a tool.
type Release struct {
	Version string  `json:"version"`
	Assets  []Asset `json:"assets,omitempty"`
}

// Fetcher resolves the latest release of a tool. The default implementation
// reads GitHub releases; tests inject a stub.
type Fetcher interface {
	Latest() (Release, error)
}

// Result is the outcome of an update check.
type Result struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	Status    Status `json:"status"`
}

// Evaluate compares the current version against the latest release and reports
// the relationship. An unparseable current version is treated as a development
// build, which is never told to auto-update.
func Evaluate(current string, latest Release) Result {
	res := Result{Current: current, Latest: latest.Version}
	cur, okCur := parse(current)
	lat, okLat := parse(latest.Version)
	switch {
	case !okCur:
		res.Status = StatusDevelopmentBuild
	case !okLat:
		res.Status = StatusUnknownLatest
	default:
		switch compare(cur, lat) {
		case 0:
			res.Status = StatusUpToDate
		case -1:
			res.Status = StatusUpdateAvailable
			res.Available = true
		default:
			res.Status = StatusNewerThanLatest
		}
	}
	return res
}

type semver struct{ major, minor, patch int }

// parse reads a "vMAJOR.MINOR.PATCH" string, ignoring any pre-release or build
// suffix. It reports ok=false for empty, "dev", or otherwise unparseable input.
func parse(v string) (semver, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" || strings.HasPrefix(v, "dev") {
		return semver{}, false
	}
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return semver{}, false
	}
	nums := make([]int, 3)
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return semver{}, false
		}
		nums[i] = n
	}
	return semver{nums[0], nums[1], nums[2]}, true
}

func compare(a, b semver) int {
	switch {
	case a.major != b.major:
		return sign(a.major - b.major)
	case a.minor != b.minor:
		return sign(a.minor - b.minor)
	case a.patch != b.patch:
		return sign(a.patch - b.patch)
	default:
		return 0
	}
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	if n > 0 {
		return 1
	}
	return 0
}
