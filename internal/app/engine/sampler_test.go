package engine

import (
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/cache"
)

var noWR [WaitBinCount][ReasonCount]int

func TestSamplerEviction(t *testing.T) {
	s := NewSampler(10 * time.Minute)
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		s.Add(base.Add(time.Duration(i)*time.Minute), i, 0, noWR)
	}
	// Retain is 10m; newest is at +19m, so anything before +9m is evicted.
	if len(s.buf) == 0 || len(s.buf) > 11 {
		t.Fatalf("unexpected buffer length %d", len(s.buf))
	}
	if s.buf[0].T.Before(base.Add(9 * time.Minute)) {
		t.Errorf("oldest sample %v not evicted", s.buf[0].T)
	}
}

func TestSamplerPointsWindow(t *testing.T) {
	s := NewSampler(time.Hour)
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	s.Add(now.Add(-90*time.Minute), 9, 9, noWR) // outside a 60m window
	s.Add(now.Add(-30*time.Minute), 3, 1, noWR)
	s.Add(now.Add(-5*time.Minute), 5, 2, noWR)
	pts := s.Points(now, time.Hour)
	for _, p := range pts {
		if p.T.Before(now.Add(-time.Hour)) || p.T.After(now) {
			t.Errorf("point %v outside window", p.T)
		}
	}
	if len(pts) == 0 {
		t.Fatalf("expected points within window")
	}
}

func TestSamplerSumWaitReason(t *testing.T) {
	s := NewSampler(time.Hour)
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	var w1, w2 [WaitBinCount][ReasonCount]int
	w1[4][ReasonBlocked] = 1
	w2[4][ReasonBlocked] = 3
	w2[4][ReasonBuildable] = 2
	s.Add(now.Add(-20*time.Minute), 0, 1, w1)
	s.Add(now.Add(-5*time.Minute), 0, 5, w2)

	sum := s.SumWaitReason(now, time.Hour)
	if sum[4][ReasonBlocked] != 4 { // 1 + 3 accumulate over the window
		t.Errorf("sum bin4 blocked = %d, want 4", sum[4][ReasonBlocked])
	}
	if sum[4][ReasonBuildable] != 2 {
		t.Errorf("sum bin4 buildable = %d, want 2", sum[4][ReasonBuildable])
	}
	if sum[0][ReasonStuck] != 0 {
		t.Errorf("empty cell = %d, want 0", sum[0][ReasonStuck])
	}
}

func TestSamplerPersistRoundTrip(t *testing.T) {
	s := NewSampler(time.Hour)
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	s.Add(now.Add(-10*time.Minute), 2, 1, noWR)
	s.Add(now.Add(-1*time.Minute), 4, 0, noWR)

	dumped := s.Dump()
	if len(dumped) != 2 {
		t.Fatalf("dump len = %d, want 2", len(dumped))
	}

	s2 := NewSampler(time.Hour)
	s2.Load(dumped)
	if len(s2.buf) != 2 {
		t.Errorf("loaded len = %d, want 2", len(s2.buf))
	}
	if _, ok := interface{}(dumped[0]).(cache.DashSample); !ok {
		t.Errorf("dump should yield cache.DashSample")
	}
}
