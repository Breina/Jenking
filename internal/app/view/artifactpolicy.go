package view

import (
	"path"
	"sort"
	"strings"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// defaultTextArtifactExtensions is the canonical allowlist of file extensions
// (lowercase, no leading dot) that open in the in-TUI artifact viewer rather
// than the browser. Single source of truth: the config default written to
// config.yaml and the prefs-dialog fallback both derive from this.
//
// Covers common log/text formats. HTML and image/binary types are deliberately
// absent so they keep launching the browser.
var defaultTextArtifactExtensions = []string{
	"conf", "csv", "err", "gradle", "groovy", "ini", "java",
	"js", "json", "log", "md", "out", "properties", "py",
	"sh", "sql", "toml", "ts", "txt", "xml", "yaml", "yml",
}

// textArtifactExtensions is the active allowlist as a set for O(1) lookup. Set
// once at startup from preferences.text_artifact_extensions (or the default)
// and again when the prefs dialog is confirmed. Read directly by ArtifactView
// and the artifact behavior to avoid threading config through their call sites
// (same rationale as widget.VimPolicy).
var textArtifactExtensions = toExtSet(defaultTextArtifactExtensions)

// toExtSet normalizes a list of extensions into a lookup set.
func toExtSet(exts []string) map[string]bool {
	m := make(map[string]bool, len(exts))
	for _, e := range exts {
		e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
		if e != "" {
			m[e] = true
		}
	}
	return m
}

// DefaultTextArtifactExtensions returns a copy of the built-in allowlist, used
// to seed config.yaml when the key is absent.
func DefaultTextArtifactExtensions() []string {
	out := make([]string, len(defaultTextArtifactExtensions))
	copy(out, defaultTextArtifactExtensions)
	return out
}

// TextArtifactExtensionList returns the active allowlist, sorted, for display
// in the preferences dialog.
func TextArtifactExtensionList() []string {
	out := make([]string, 0, len(textArtifactExtensions))
	for e := range textArtifactExtensions {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// SetTextArtifactExtensions replaces the active allowlist. A nil/empty slice
// restores the built-in default. Safe to call at startup and on prefs change.
func SetTextArtifactExtensions(exts []string) {
	if len(exts) == 0 {
		textArtifactExtensions = toExtSet(defaultTextArtifactExtensions)
		return
	}
	m := toExtSet(exts)
	if len(m) == 0 {
		m = toExtSet(defaultTextArtifactExtensions)
	}
	textArtifactExtensions = m
}

// FindArtifact locates an artifact by its display path, falling back to a
// basename match so callers can pass just the file name of a nested artifact.
func FindArtifact(arts []jmodel.Artifact, name string) (jmodel.Artifact, bool) {
	for _, a := range arts {
		if a.DisplayPath == name {
			return a, true
		}
	}
	for _, a := range arts {
		if path.Base(a.DisplayPath) == name {
			return a, true
		}
	}
	return jmodel.Artifact{}, false
}

// IsTextArtifact reports whether the artifact's display path has an extension in
// the active allowlist. Files without an extension are treated as non-text so
// the gate stays predictable; the in-viewer Content-Type/binary sniff is the
// safety net for anything that slips through.
func IsTextArtifact(displayPath string) bool {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(displayPath), "."))
	if ext == "" {
		return false
	}
	return textArtifactExtensions[ext]
}
