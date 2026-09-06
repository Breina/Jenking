package cache

import (
	"fmt"
	"hash/fnv"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// logsDir is the sub-directory under the cache root where build console logs
// are materialized for the MCP get_logs file handoff.
func (d *DiskStore) logsDir() string { return filepath.Join(d.dir, "logs") }

// BuildLogPath returns the on-disk path a build's log is written to. The name
// hashes the job path (so nested folders can't collide or escape the dir) and
// keeps the human-readable last path segment plus build number; a non-empty
// stage is appended as a slug.
func (d *DiskStore) BuildLogPath(jobPath string, num int, stage string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(jobPath))
	last := path.Base(jobPath)
	if last == "." || last == "/" {
		last = "job"
	}
	name := fmt.Sprintf("%08x-%s#%d", h.Sum32(), last, num)
	if stage != "" {
		name += "@" + slugify(stage)
	}
	return filepath.Join(d.logsDir(), name+".log")
}

// ScanLogPath is BuildLogPath for a container's scan run. A scan has no build
// number — Jenkins keeps only the latest — so the file is named for the
// container alone and is overwritten on each fetch.
func (d *DiskStore) ScanLogPath(jobPath string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(jobPath))
	last := path.Base(jobPath)
	if last == "." || last == "/" {
		last = "job"
	}
	return filepath.Join(d.logsDir(), fmt.Sprintf("%08x-%s@scan.log", h.Sum32(), last))
}

// SaveScanLog writes a container's scan log to disk and returns its path/size.
func (d *DiskStore) SaveScanLog(jobPath, text string) (string, int64, error) {
	if err := os.MkdirAll(d.logsDir(), 0o700); err != nil {
		return "", 0, err
	}
	p := d.ScanLogPath(jobPath)
	if err := writeFileAtomic(p, text); err != nil {
		return "", 0, err
	}
	return p, int64(len(text)), nil
}

// SaveBuildLog writes text to the build's log file (creating the logs dir) and
// returns the path and byte size. The write is atomic (temp + rename) so a
// reader never sees a partial file.
func (d *DiskStore) SaveBuildLog(jobPath string, num int, stage, text string) (string, int64, error) {
	if err := os.MkdirAll(d.logsDir(), 0o700); err != nil {
		return "", 0, err
	}
	p := d.BuildLogPath(jobPath, num, stage)
	if err := writeFileAtomic(p, text); err != nil {
		return "", 0, err
	}
	return p, int64(len(text)), nil
}

// ArtifactPath returns the on-disk path a build artifact is materialized to.
// It mirrors BuildLogPath: the job path is hashed so nested folders cannot
// collide or escape the dir, and the artifact's archive-relative name is
// slugified (it may itself contain directory separators).
func (d *DiskStore) ArtifactPath(jobPath string, num int, name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(jobPath))
	last := path.Base(jobPath)
	if last == "." || last == "/" {
		last = "job"
	}
	return filepath.Join(d.logsDir(), fmt.Sprintf("%08x-%s#%d@%s", h.Sum32(), last, num, artifactSlug(name)))
}

// artifactSlug makes an archive-relative artifact name filename-safe while
// keeping its extension, so the materialized file still opens with the right
// tooling (report.html stays .html rather than becoming report-html).
func artifactSlug(name string) string {
	ext := path.Ext(name)
	stem := slugify(strings.TrimSuffix(name, ext))
	if ext == "" {
		return stem
	}
	return stem + "." + slugify(ext)
}

// SaveArtifact writes a downloaded artifact to disk and returns its path/size.
func (d *DiskStore) SaveArtifact(jobPath string, num int, name, content string) (string, int64, error) {
	if err := os.MkdirAll(d.logsDir(), 0o700); err != nil {
		return "", 0, err
	}
	p := d.ArtifactPath(jobPath, num, name)
	if err := writeFileAtomic(p, content); err != nil {
		return "", 0, err
	}
	return p, int64(len(content)), nil
}

// writeFileAtomic writes content to path via a pid-suffixed temp file + rename.
func writeFileAtomic(p, content string) error {
	tmp := p + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// slugify reduces a stage name to a filename-safe token.
func slugify(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "stage"
	}
	return out
}
