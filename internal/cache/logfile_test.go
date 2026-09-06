package cache

import (
	"os"
	"strings"
	"testing"
)

func TestSaveBuildLog_RoundTrip(t *testing.T) {
	d := newTestDiskStore(t)

	path, size, err := d.SaveBuildLog("TeamA/service/main", 42, "", "hello log\n")
	if err != nil {
		t.Fatalf("SaveBuildLog: %v", err)
	}
	if size != int64(len("hello log\n")) {
		t.Errorf("size = %d", size)
	}
	if !strings.HasSuffix(path, "-main#42.log") {
		t.Errorf("path %q missing readable suffix", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "hello log\n" {
		t.Errorf("content = %q", got)
	}
}

func TestBuildLogPath_StageAndCollision(t *testing.T) {
	d := newTestDiskStore(t)

	// Distinct job paths sharing a last segment must not collide (hash prefix).
	p1 := d.BuildLogPath("a/main", 1, "")
	p2 := d.BuildLogPath("b/main", 1, "")
	if p1 == p2 {
		t.Errorf("paths for a/main and b/main collided: %s", p1)
	}

	// Stage scoping produces a distinct, slugified name.
	ps := d.BuildLogPath("a/main", 1, "Deploy to Prod")
	if ps == p1 {
		t.Error("stage path should differ from whole-build path")
	}
	if !strings.Contains(ps, "@Deploy-to-Prod") {
		t.Errorf("stage slug missing in %q", ps)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Deploy":          "Deploy",
		"Deploy to Prod":  "Deploy-to-Prod",
		"build/test:unit": "build-test-unit",
		"  ":              "stage",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
