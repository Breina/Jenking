package usecase

import (
	"context"
	"maps"
	"regexp"
	"slices"
	"testing"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

type editFake struct {
	jmodel.JenkinsClient
	build       jmodel.BuildDetail
	descSet     *string
	configured  []string
	buildParams map[string]string
	defs        []jmodel.ParameterDefinition
	triggered   map[string]string
}

func (f *editFake) GetBuild(context.Context, string, int) (*jmodel.BuildDetail, error) {
	b := f.build
	return &b, nil
}

func (f *editFake) SetBuildDescription(_ context.Context, _ string, _ int, desc string) error {
	f.descSet = &desc
	f.build.Description = desc
	return nil
}

func (f *editFake) ConfigureBuild(_ context.Context, _ string, _ int, name, desc string) error {
	f.configured = []string{name, desc}
	f.build.Name, f.build.Description = name, desc
	return nil
}

func (f *editFake) GetBuildParameters(context.Context, string, int) (map[string]string, error) {
	return maps.Clone(f.buildParams), nil
}

func (f *editFake) GetJobParameters(context.Context, string) ([]jmodel.ParameterDefinition, error) {
	return f.defs, nil
}

func (f *editFake) TriggerBuild(_ context.Context, _ string, params map[string]string) (int64, error) {
	f.triggered = params
	return 9, nil
}

func TestUpdateBuildDescriptionOnly(t *testing.T) {
	f := &editFake{build: jmodel.BuildDetail{Build: jmodel.Build{Description: "old"}}}
	desc := "new"
	b, err := Deps{Client: f}.UpdateBuild(t.Context(), "a", 1, nil, &desc)
	if err != nil {
		t.Fatal(err)
	}
	if f.configured != nil || f.descSet == nil || b.Description != "new" {
		t.Errorf("configured=%v descSet=%v build=%+v", f.configured, f.descSet, b)
	}
}

func TestUpdateBuildDisplayNameKeepsDescription(t *testing.T) {
	f := &editFake{build: jmodel.BuildDetail{Build: jmodel.Build{Description: "keep"}}}
	name := "v1"
	if _, err := (Deps{Client: f}).UpdateBuild(t.Context(), "a", 1, &name, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.configured, []string{"v1", "keep"}) {
		t.Errorf("configured = %v", f.configured)
	}
}

func TestUpdateBuildRejectsEmptyRequest(t *testing.T) {
	empty := ""
	if _, err := (Deps{Client: &editFake{}}).UpdateBuild(t.Context(), "a", 1, nil, nil); err == nil {
		t.Error("expected error with no fields")
	}
	if _, err := (Deps{Client: &editFake{}}).UpdateBuild(t.Context(), "a", 1, &empty, nil); err == nil {
		t.Error("expected error for empty display name")
	}
}

func TestRebuildReusesParamsAndDropsPasswords(t *testing.T) {
	f := &editFake{
		buildParams: map[string]string{"BRANCH": "main", "SECRET": "", "TOKEN": "", "ENV": "dev"},
		defs: []jmodel.ParameterDefinition{
			{Name: "SECRET", Type: jmodel.ParamTypePassword},
			{Name: "TOKEN", Type: jmodel.ParamTypePassword},
			{Name: "ENV", Type: jmodel.ParamTypeString},
		},
	}
	res, err := Deps{Client: f}.Rebuild(t.Context(), "a", 4, TriggerOptions{Params: map[string]string{"ENV": "prod", "TOKEN": "t0k"}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"BRANCH": "main", "ENV": "prod", "TOKEN": "t0k"}
	if !maps.Equal(f.triggered, want) {
		t.Errorf("triggered = %v, want %v", f.triggered, want)
	}
	if !slices.Equal(res.Dropped, []string{"SECRET"}) || res.QueueID != 9 {
		t.Errorf("result = %+v", res)
	}
}

func TestSearchText(t *testing.T) {
	text := "a\r\nerror one\nb\nc\nerror two\nd\n"
	m, total := SearchText(text, regexp.MustCompile("error"), 1, 1)
	if total != 2 || len(m) != 1 {
		t.Fatalf("total=%d matches=%d", total, len(m))
	}
	if m[0].LineNumber != 2 || !slices.Equal(m[0].Before, []string{"a"}) || !slices.Equal(m[0].After, []string{"b"}) {
		t.Errorf("match = %+v", m[0])
	}
}

func TestLogLinesStripConsoleAnnotations(t *testing.T) {
	// Jenkins embeds base64 metadata in ANSI hidden blocks; it must not reach
	// callers nor produce regex matches of its own.
	noise := "\x1b[8mha:////4NpJfailERROR+base64\x1b[0m"
	text := noise + "[Pipeline] echo\n" + "\x1b[31mBuild step ok\x1b[0m\n"
	lines := splitLines(text)
	if !slices.Equal(lines, []string{"[Pipeline] echo", "Build step ok"}) {
		t.Fatalf("lines = %q", lines)
	}
	if _, total := SearchText(text, regexp.MustCompile("(?i)error"), 10, 0); total != 0 {
		t.Errorf("annotation produced %d false matches", total)
	}
}

func TestLineWindow(t *testing.T) {
	text := "1\n2\n3\n4\n5\n"
	tests := []struct {
		start, max int
		want       string
		first, tot int
	}{
		{2, 2, "2\n3", 2, 5},
		{-2, 10, "4\n5", 4, 5},
		{9, 1, "", 0, 5},
		{-9, 2, "1\n2", 1, 5},
	}
	for _, tt := range tests {
		got, first, total := LineWindow(text, tt.start, tt.max)
		if got != tt.want || first != tt.first || total != tt.tot {
			t.Errorf("LineWindow(%d,%d) = %q,%d,%d", tt.start, tt.max, got, first, total)
		}
	}
}
