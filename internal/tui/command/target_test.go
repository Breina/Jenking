package command

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want Target
	}{
		{"empty", nil, Target{}},
		{"project only", []string{"webidm"}, Target{ProjectSuffix: "webidm"}},
		{
			"project + branch",
			[]string{"webidm", "main"},
			Target{ProjectSuffix: "webidm", Branch: "main"},
		},
		{
			"branch with slash",
			[]string{"webidm", "feature/foo"},
			Target{ProjectSuffix: "webidm", Branch: "feature/foo"},
		},
		{
			"with #<n>",
			[]string{"webidm", "main", "#42"},
			Target{ProjectSuffix: "webidm", Branch: "main", Build: BuildRef{Number: 42, Set: true}},
		},
		{
			"with #last",
			[]string{"webidm", "main", "#last"},
			Target{ProjectSuffix: "webidm", Branch: "main", Build: BuildRef{IsLast: true, Set: true}},
		},
		{
			"stage with spaces",
			[]string{"webidm", "main", "#42", ":Build", "&", "Test"},
			Target{
				ProjectSuffix: "webidm", Branch: "main",
				Build: BuildRef{Number: 42, Set: true},
				Stage: "Build & Test",
			},
		},
		{
			"stage attached to colon",
			[]string{"webidm", "main", "#42", ":Deploy"},
			Target{
				ProjectSuffix: "webidm", Branch: "main",
				Build: BuildRef{Number: 42, Set: true},
				Stage: "Deploy",
			},
		},
		{
			"markers reordered (# before positional)",
			[]string{"#42", "webidm", "main"},
			Target{ProjectSuffix: "webidm", Branch: "main", Build: BuildRef{Number: 42, Set: true}},
		},
		{
			"build only (partial target)",
			[]string{"#42"},
			Target{Build: BuildRef{Number: 42, Set: true}},
		},
		{
			"stage only",
			[]string{":Deploy"},
			Target{Stage: "Deploy"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTarget(tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseTarget(%v) = %+v, want %+v", tc.args, got, tc.want)
			}
		})
	}
}

func TestParseTarget_Errors(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		errSubstr string
	}{
		{"non-numeric build", []string{"#abc"}, "invalid build"},
		{"negative build", []string{"#-1"}, "invalid build"},
		{"zero build", []string{"#0"}, "must be positive"},
		{"empty build marker", []string{"#"}, "empty build marker"},
		{"empty stage marker", []string{":"}, "empty stage marker"},
		{"multiple build markers", []string{"#1", "#2"}, "multiple build"},
		{"too many positional", []string{"a", "b", "c"}, "too many positional"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTarget(tc.args)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.errSubstr)
			}
			if !strings.Contains(err.Error(), tc.errSubstr) {
				t.Errorf("error %q does not contain %q", err, tc.errSubstr)
			}
		})
	}
}

func TestTarget_IsEmpty(t *testing.T) {
	if !(Target{}).IsEmpty() {
		t.Error("zero Target should be empty")
	}
	if (Target{ProjectSuffix: "x"}).IsEmpty() {
		t.Error("Target with suffix should not be empty")
	}
	if (Target{Build: BuildRef{Set: true}}).IsEmpty() {
		t.Error("Target with build should not be empty")
	}
}
