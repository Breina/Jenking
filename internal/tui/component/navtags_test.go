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
		{"jobs", []string{"jobs"}},
		{"builds", []string{"jobs", "builds"}},
		{"stages", []string{"jobs", "builds", "stages"}},
		{"stagelog", []string{"jobs", "builds", "stages", "log"}},
		{"artifacts", []string{"jobs", "builds", "artifacts"}},
		// The artifact file viewer nests one level under the artifacts list,
		// mirroring how stagelog nests under stages.
		{"artifact", []string{"jobs", "builds", "artifacts", "file"}},
		// Unknown / dialog view types fall back to a lone tag of their own name.
		{"script", []string{"script"}},
		{"", []string{"jobs"}},
	}
	for _, tt := range tests {
		if got := chainFor(tt.viewType); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("chainFor(%q) = %v, want %v", tt.viewType, got, tt.want)
		}
	}
}
