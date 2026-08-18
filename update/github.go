package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

// GitHubFetcher resolves the latest release from a public GitHub repository.
type GitHubFetcher struct {
	Owner  string
	Repo   string
	Client *http.Client
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// Latest fetches the newest published release.
func (g GitHubFetcher) Latest() (Release, error) {
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", g.Owner, g.Repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github releases: status %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Release{}, err
	}
	out := Release{Version: rel.TagName}
	for _, a := range rel.Assets {
		out.Assets = append(out.Assets, Asset{Name: a.Name, URL: a.BrowserDownloadURL})
	}
	return out, nil
}

// AssetFor returns the release asset matching the current OS and architecture.
// It ignores common checksum/signature artifacts and accepts common architecture
// aliases such as x86_64 for amd64.
func AssetFor(rel Release) (Asset, bool) {
	wantOS := runtime.GOOS
	archNames := archAliases(runtime.GOARCH)
	for _, a := range rel.Assets {
		name := strings.ToLower(a.Name)
		if isIntegrityArtifact(name) {
			continue
		}
		if !strings.Contains(name, wantOS) {
			continue
		}
		for _, wantArch := range archNames {
			if strings.Contains(name, wantArch) {
				return a, true
			}
		}
	}
	return Asset{}, false
}

func archAliases(goarch string) []string {
	switch goarch {
	case "amd64":
		return []string{"amd64", "x86_64"}
	case "386":
		return []string{"386", "i386", "x86"}
	case "arm64":
		return []string{"arm64", "aarch64"}
	default:
		return []string{goarch}
	}
}

func isIntegrityArtifact(name string) bool {
	for _, suffix := range []string{".sha256", ".sha256sum", ".sig", ".asc", ".pem"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	for _, part := range []string{"checksum", "checksums", "sha256sum"} {
		if strings.Contains(name, part) {
			return true
		}
	}
	return false
}

func downloadAsset(asset Asset, client *http.Client) (io.ReadCloser, error) {
	if client == nil {
		client = defaultHTTPClient
	}
	resp, err := client.Get(asset.URL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download asset: status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// ReplaceExecutable downloads the OS/arch-matched asset and atomically replaces
// the running executable. It is the default Apply implementation for tools that
// distribute a single binary per platform.
func ReplaceExecutable(rel Release) error {
	return ReplaceExecutableWithClient(rel, nil)
}

// ReplaceExecutableWithClient is ReplaceExecutable with an injected HTTP client,
// primarily for tests and callers that need a custom transport.
func ReplaceExecutableWithClient(rel Release, client *http.Client) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("automatic replacement is not supported on windows")
	}
	asset, ok := AssetFor(rel)
	if !ok {
		return fmt.Errorf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return err
	}

	body, err := downloadAsset(asset, client)
	if err != nil {
		return err
	}
	defer body.Close()

	dir := filepath.Dir(self)
	tmp, err := os.CreateTemp(dir, ".update-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	return os.Rename(tmpName, self)
}
