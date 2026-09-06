package buildregistry

import (
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

func rec(job string, num int, status jmodel.BuildStatus, terminal bool, updated time.Time) Record {
	return Record{
		JobPath:   job,
		Terminal:  terminal,
		UpdatedAt: updated,
		Build:     jmodel.Build{Number: num, Status: status},
	}
}

func TestMergeRecords(t *testing.T) {
	t0 := time.Unix(1000, 0)
	t1 := time.Unix(2000, 0)

	tests := []struct {
		name       string
		a, b       []Record
		wantNum    int                        // total keys expected
		wantStatus map[int]jmodel.BuildStatus // per build number
	}{
		{
			name:    "disjoint keys unioned",
			a:       []Record{rec("j", 1, jmodel.BuildStatusSuccess, true, t0)},
			b:       []Record{rec("j", 2, jmodel.BuildStatusFailed, true, t0)},
			wantNum: 2,
			wantStatus: map[int]jmodel.BuildStatus{
				1: jmodel.BuildStatusSuccess,
				2: jmodel.BuildStatusFailed,
			},
		},
		{
			name:    "terminal wins over running regardless of time",
			a:       []Record{rec("j", 1, jmodel.BuildStatusSuccess, true, t0)},
			b:       []Record{rec("j", 1, jmodel.BuildStatusRunning, false, t1)}, // newer but not terminal
			wantNum: 1,
			wantStatus: map[int]jmodel.BuildStatus{
				1: jmodel.BuildStatusSuccess,
			},
		},
		{
			name:    "both non-terminal: newer UpdatedAt wins",
			a:       []Record{rec("j", 1, jmodel.BuildStatusRunning, false, t0)},
			b:       []Record{rec("j", 1, jmodel.BuildStatusUnknown, false, t1)},
			wantNum: 1,
			wantStatus: map[int]jmodel.BuildStatus{
				1: jmodel.BuildStatusUnknown,
			},
		},
		{
			name:    "both terminal: newer UpdatedAt wins",
			a:       []Record{rec("j", 1, jmodel.BuildStatusSuccess, true, t0)},
			b:       []Record{rec("j", 1, jmodel.BuildStatusFailed, true, t1)},
			wantNum: 1,
			wantStatus: map[int]jmodel.BuildStatus{
				1: jmodel.BuildStatusFailed,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := MergeRecords(tc.a, tc.b)
			if len(out) != tc.wantNum {
				t.Fatalf("want %d records, got %d: %+v", tc.wantNum, len(out), out)
			}
			for _, r := range out {
				if want, ok := tc.wantStatus[r.Build.Number]; ok && r.Build.Status != want {
					t.Errorf("build #%d: want status %v, got %v", r.Build.Number, want, r.Build.Status)
				}
			}
		})
	}
}

func TestMergeRecords_KeepsLatestRunningConfirmation(t *testing.T) {
	early := time.Unix(1000, 0)
	late := time.Unix(2000, 0)

	a := rec("j", 1, jmodel.BuildStatusSuccess, true, late)
	a.LastSeenRunning = early
	b := rec("j", 1, jmodel.BuildStatusRunning, false, early)
	b.LastSeenRunning = late

	out := MergeRecords([]Record{a}, []Record{b})
	if len(out) != 1 {
		t.Fatalf("want 1 record, got %d", len(out))
	}
	// Terminal (a) wins the status, but the later LastSeenRunning is preserved.
	if out[0].Build.Status != jmodel.BuildStatusSuccess {
		t.Errorf("terminal status should win, got %v", out[0].Build.Status)
	}
	if !out[0].LastSeenRunning.Equal(late) {
		t.Errorf("want LastSeenRunning %v, got %v", late, out[0].LastSeenRunning)
	}
}
