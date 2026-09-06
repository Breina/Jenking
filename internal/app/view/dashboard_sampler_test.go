package view

import (
	"testing"
	"time"
)

func TestTimescaleZoom(t *testing.T) {
	ts := newTimescale()
	if ts.window() != 2*time.Hour {
		t.Fatalf("default window = %v, want 2h", ts.window())
	}
	ts.zoomIn()
	if ts.window() != time.Hour {
		t.Errorf("after zoomIn = %v, want 1h", ts.window())
	}
	ts.zoomIn()
	ts.zoomIn()
	ts.zoomIn()
	if ts.window() != 15*time.Minute {
		t.Errorf("floor window = %v, want 15m", ts.window())
	}
	for i := 0; i < 10; i++ {
		ts.zoomOut()
	}
	if ts.window() != 2*time.Hour {
		t.Errorf("ceiling window = %v, want 2h", ts.window())
	}
	if ts.label() != "2h" {
		t.Errorf("label = %q, want 2h", ts.label())
	}
}
