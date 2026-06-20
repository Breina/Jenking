package view

import "testing"

func TestParentJobPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Multibranch branch: parent is the project, grandparent the folder.
		{"Omgeving/migratie%2Fjenkins-e2e/main", "Omgeving/migratie%2Fjenkins-e2e"},
		{"Omgeving/migratie%2Fjenkins-e2e", "Omgeving"},
		// Single-segment job has no parent.
		{"Omgeving", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := parentJobPath(tc.in); got != tc.want {
			t.Errorf("parentJobPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
