package jenkins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Breina/Jenking/internal/jenkins/pipelinesyntax"
)

func TestDumpSyntaxFetch_WritesAllThreeFiles(t *testing.T) {
	// Redirect XDG_CACHE_HOME so we can inspect the dump in an isolated dir.
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	sym := &pipelinesyntax.Symbols{
		Steps:   []pipelinesyntax.Step{{Name: "sh"}, {Name: "junit"}},
		Globals: []pipelinesyntax.GlobalVar{{Name: "env"}},
	}
	dumpSyntaxFetch(
		"https://example.com",
		"Code/foo", 42,
		"/job/Code/job/foo/42/pipeline-syntax/gdsl", []byte("method(name: 'sh')"), nil,
		"/job/Code/job/foo/42/pipeline-syntax/globals", []byte("<html></html>"), nil,
		sym,
	)

	dir := filepath.Join(tmp, "jenking", "syntax-debug")
	for _, name := range []string{"stats.txt", "gdsl.txt", "globals.html"} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("missing %s: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
	stats, _ := os.ReadFile(filepath.Join(dir, "stats.txt"))
	if !strings.Contains(string(stats), "parsed_steps:   2") {
		t.Errorf("stats.txt missing step count, got:\n%s", stats)
	}
}

func TestDumpSyntaxFetch_RecordsErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	dumpSyntaxFetch(
		"https://example.com",
		"Code/foo", 42,
		"/job/Code/job/foo/42/pipeline-syntax/gdsl", nil, os.ErrPermission,
		"/job/Code/job/foo/42/pipeline-syntax/globals", nil, os.ErrNotExist,
		&pipelinesyntax.Symbols{},
	)
	dir := filepath.Join(tmp, "jenking", "syntax-debug")
	stats, err := os.ReadFile(filepath.Join(dir, "stats.txt"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(stats)
	if !strings.Contains(s, "gdsl_error:") || !strings.Contains(s, "globals_err:") {
		t.Errorf("stats.txt missing error lines, got:\n%s", s)
	}
}
