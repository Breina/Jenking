package command

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantArgs []string
	}{
		{"quit", "quit", nil},
		{"build param1=val1", "build", []string{"param1=val1"}},
		{"  quit  ", "quit", nil},
		{"", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Parse(tt.input)
			if got.Name != tt.wantName {
				t.Errorf("Parse(%q).Name = %q, want %q", tt.input, got.Name, tt.wantName)
			}
			if len(got.Args) != len(tt.wantArgs) {
				t.Errorf("Parse(%q).Args = %v, want %v", tt.input, got.Args, tt.wantArgs)
			}
		})
	}
}
