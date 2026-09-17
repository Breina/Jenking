package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// Status is a controller health snapshot aggregated from core endpoints.
type Status struct {
	jmodel.ControllerStatus
	User      string
	LatencyMs int64
	Nodes     int
	// OfflineNodes are excluded from Executors/BusyExecutors, which count only
	// capacity that can actually run builds.
	OfflineNodes  int
	Executors     int
	BusyExecutors int
	QueueLength   int
	StuckItems    int
	BlockedItems  int
	// Warnings lists parts of the snapshot that could not be read (e.g. the
	// caller lacks permission), which do not fail the whole check.
	Warnings []string
}

// Status checks controller health. Only an unreachable controller is an error.
func (d Deps) Status(ctx context.Context) (Status, error) {
	start := time.Now()
	cs, err := d.Client.GetControllerStatus(ctx)
	if err != nil {
		return Status{}, err
	}
	st := Status{ControllerStatus: cs, LatencyMs: time.Since(start).Milliseconds()}

	if u, err := d.Client.WhoAmI(ctx); err != nil {
		st.warn("user", err)
	} else {
		st.User = u.ID
	}

	if nodes, err := d.Client.ListNodes(ctx); err != nil {
		st.warn("nodes", err)
	} else {
		st.Nodes = len(nodes)
		for _, n := range nodes {
			if n.Offline {
				st.OfflineNodes++
				continue
			}
			st.Executors += n.NumExecutors
			st.BusyExecutors += n.BusyExecutors
		}
	}

	if queue, err := d.Client.ListQueue(ctx); err != nil {
		st.warn("queue", err)
	} else {
		st.QueueLength = len(queue)
		for _, q := range queue {
			if q.Stuck {
				st.StuckItems++
			}
			if q.Blocked {
				st.BlockedItems++
			}
		}
	}
	return st, nil
}

func (s *Status) warn(part string, err error) {
	s.Warnings = append(s.Warnings, fmt.Sprintf("%s: %v", part, err))
}

// GetQueueItem returns a queue item by id plus its build number once it has
// started (0 while still queued).
func (d Deps) GetQueueItem(ctx context.Context, id int64) (*jmodel.QueueItem, int, error) {
	return d.Client.GetQueueItem(ctx, id)
}

// SCMInfo is a job's SCM web URL and the revisions one of its builds checked out.
type SCMInfo struct {
	SCMURL      string
	BuildNumber int
	Revisions   []jmodel.SCMRevision
}

// GetSCM returns a job's SCM web URL and, when number > 0, that build's revisions.
func (d Deps) GetSCM(ctx context.Context, jobPath string, number int) (SCMInfo, error) {
	scmURL, err := d.Client.GetJobSCMURL(ctx, jobPath)
	if err != nil {
		return SCMInfo{}, err
	}
	info := SCMInfo{SCMURL: scmURL, BuildNumber: number}
	if number > 0 {
		if info.Revisions, err = d.Client.GetBuildSCM(ctx, jobPath, number); err != nil {
			return SCMInfo{}, err
		}
	}
	return info, nil
}

// ResolveJobVisible is ResolveJob for callers that must not learn of jobs they
// cannot read: with SharedIndex set, each match is confirmed through Client.
func (d Deps) ResolveJobVisible(ctx context.Context, scmURL string) []jmodel.JobSCMMatch {
	matches := d.ResolveJob(scmURL)
	if !d.SharedIndex {
		return matches
	}
	visible := matches[:0]
	for _, m := range matches {
		if _, err := d.Client.GetJobSCMURL(ctx, m.JobPath); err == nil {
			visible = append(visible, m)
		}
	}
	return visible
}
