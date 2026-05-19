package updater

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	githubOwner = "Breina"
	githubRepo  = "Jenking"
	apiBase     = "https://api.github.com/repos/" + githubOwner + "/" + githubRepo
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// LatestVersion fetches the latest release tag from GitHub (e.g. "v0.2.0").
// Returns an error if there are no releases or the API call fails.
func LatestVersion() (string, error) {
	req, err := http.NewRequest("GET", apiBase+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("no releases published yet")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API: HTTP %d", resp.StatusCode)
	}

	var r release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if r.TagName == "" {
		return "", fmt.Errorf("empty tag in latest release")
	}
	return r.TagName, nil
}

// IsNewer returns true if latest represents a higher semver than current.
// Both values may optionally be prefixed with "v".
func IsNewer(current, latest string) bool {
	c := parseSemver(strings.TrimPrefix(current, "v"))
	l := parseSemver(strings.TrimPrefix(latest, "v"))
	for i := range c {
		if l[i] > c[i] {
			return true
		}
		if l[i] < c[i] {
			return false
		}
	}
	return false
}

func parseSemver(s string) [3]int {
	var v [3]int
	for i, p := range strings.SplitN(s, ".", 3) {
		if i < 3 {
			v[i], _ = strconv.Atoi(p)
		}
	}
	return v
}

// SelfUpdate downloads the release asset for the current OS/arch and atomically
// replaces the running binary. The caller should exit the process after this
// returns nil so the new binary takes effect.
func SelfUpdate(latestTag string) error {
	rel, err := fetchRelease(latestTag)
	if err != nil {
		return err
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH

	url, name := pickAsset(rel.Assets, goos, goarch)
	if url == "" {
		return fmt.Errorf("no release asset found for %s/%s in tag %s", goos, goarch, latestTag)
	}

	tmpPath, err := download(url, name)
	if err != nil {
		return err
	}
	defer os.Remove(tmpPath)

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current binary: %w", err)
	}
	// Resolve any symlinks so we replace the real file.
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}

	// Write a staging file next to the binary so the rename stays on the same
	// filesystem (avoids "invalid cross-device link" when /tmp is a tmpfs).
	stagePath := exe + ".update-tmp"
	if err := copyFile(tmpPath, stagePath, 0755); err != nil {
		os.Remove(stagePath)
		return fmt.Errorf("stage binary: %w", err)
	}
	os.Remove(tmpPath)

	if err := os.Rename(stagePath, exe); err != nil {
		os.Remove(stagePath)
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func fetchRelease(tag string) (*release, error) {
	req, err := http.NewRequest("GET", apiBase+"/releases/tags/"+tag, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch release: HTTP %d", resp.StatusCode)
	}

	var r release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("parse release: %w", err)
	}
	return &r, nil
}

// pickAsset finds the best matching asset for goos/goarch.
// It normalises common alternate names (amd64↔x86_64, arm64↔aarch64).
func pickAsset(assets []asset, goos, goarch string) (url, name string) {
	archAlt := map[string]string{
		"amd64": "x86_64",
		"386":   "i386",
		"arm64": "aarch64",
	}
	alt := archAlt[goarch]

	for _, a := range assets {
		n := strings.ToLower(a.Name)
		// Skip checksums and signatures.
		if strings.HasSuffix(n, ".sha256") || strings.HasSuffix(n, ".sig") {
			continue
		}
		hasOS := strings.Contains(n, goos)
		hasArch := strings.Contains(n, goarch) || (alt != "" && strings.Contains(n, alt))
		if hasOS && hasArch {
			return a.BrowserDownloadURL, a.Name
		}
	}
	return "", ""
}

// download fetches url into a temp file and returns its path.
// For .tar.gz and .tgz assets it extracts the jenking binary from the archive.
func download(url, assetName string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "jenking-update-*")
	if err != nil {
		return "", fmt.Errorf("temp file: %w", err)
	}

	lname := strings.ToLower(assetName)
	if strings.HasSuffix(lname, ".tar.gz") || strings.HasSuffix(lname, ".tgz") {
		if err := extractTarGz(resp.Body, tmp); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return "", err
		}
	} else {
		if _, err := io.Copy(tmp, resp.Body); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return "", fmt.Errorf("write: %w", err)
		}
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("close temp: %w", err)
	}
	return tmp.Name(), nil
}

// extractTarGz searches for the jenking (or jenking.exe) binary inside the
// archive and writes it to dst.
func extractTarGz(r io.Reader, dst *os.File) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		base := filepath.Base(hdr.Name)
		if base == "jenking" || base == "jenking.exe" {
			if _, err := io.Copy(dst, tr); err != nil {
				return fmt.Errorf("extract: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("jenking binary not found in archive")
}
