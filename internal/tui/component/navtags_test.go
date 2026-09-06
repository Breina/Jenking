package component

import (
	"reflect"
	"testing"
)

func TestChainFor(t *testing.T) {
	tests := []struct {
		viewType string
		want     []string
	}{
		// Every trail starts at the views list, which is the root of the
		// navigation: a job list is always the job list *of* some view.
		{"views", []string{"views"}},
		{"jobs", []string{"views", "jobs"}},
		{"builds", []string{"views", "jobs", "builds"}},
		{"stages", []string{"views", "jobs", "builds", "stages"}},
		{"stagelog", []string{"views", "jobs", "builds", "stages", "log"}},
		{"artifacts", []string{"views", "jobs", "builds", "artifacts"}},
		// The artifact file viewer nests one level under the artifacts list,
		// mirroring how stagelog nests under stages.
		{"artifact", []string{"views", "jobs", "builds", "artifacts", "file"}},
		// Unknown / dialog view types fall back to a lone tag of their own name.
		{"script", []string{"script"}},
		{"", []string{"views", "jobs"}},
	}
	for _, tt := range tests {
		if got := chainFor(tt.viewType); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("chainFor(%q) = %v, want %v", tt.viewType, got, tt.want)
		}
	}
}
