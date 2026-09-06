package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/navmsg"
)

// Poll intervals for Trigger --wait. Package vars so tests can shrink them.
var (
	queuePollInterval = 2 * time.Second
	buildPollInterval = 3 * time.Second
)

// TriggerOptions configures a build trigger.
type TriggerOptions struct {
	Params map[string]string
	// Wait blocks until the build leaves the queue and finishes.
	Wait bool
	// Progress, if set, receives human-readable status lines while waiting
	// ("queued: …", "started …", "build is paused …"). The CLI routes these to
	// stderr; the MCP server routes them to progress notifications.
	Progress func(string)
}

// TriggerResult is the outcome of a trigger. BuildNumber/Status are only set
// when Wait was requested and the build was reached.
type TriggerResult struct {
	JobPath     string
	QueueID     int64
	BuildNumber int
	Status      jmodel.BuildStatus
}

// Trigger starts a build and, when opt.Wait is set, blocks until it finishes.
func (d Deps) Trigger(ctx context.Context, jobPath string, opt TriggerOptions) (TriggerResult, error) {
	queueID, err := d.Client.TriggerBuild(ctx, jobPath, opt.Params)
	if err != nil {
		return TriggerResult{}, err
	}
	res := TriggerResult{JobPath: jobPath, QueueID: queueID}
	if !opt.Wait {
		return res, nil
	}
	if queueID == 0 {
		return res, fmt.Errorf("triggered %s but Jenkins reported no queue id; cannot wait", jobPath)
	}
	return d.waitForBuild(ctx, res, opt.Progress)
}

// waitForBuild polls the queue until the item is assigned a build number, then
// polls that build until it stops running.
func (d Deps) waitForBuild(ctx context.Context, res TriggerResult, progress func(string)) (TriggerResult, error) {
	notify := func(m string) {
		if progress != nil {
			progress(m)
		}
	}

	buildNum := 0
	lastWhy := ""
	for buildNum == 0 {
		item, num, err := d.Client.GetQueueItem(ctx, res.QueueID)
		if err != nil {
			return res, fmt.Errorf("waiting for queue item %d: %w", res.QueueID, err)
		}
		if num > 0 {
			buildNum = num
			break
		}
		if item != nil && item.Why != "" && item.Why != lastWhy {
			lastWhy = item.Why
			notify("queued: " + item.Why)
		}
		if err := sleepCtx(ctx, queuePollInterval); err != nil {
			return res, fmt.Errorf("waiting for queue item %d: %w", res.QueueID, err)
		}
	}
	res.BuildNumber = buildNum
	notify(fmt.Sprintf("started %s #%d", res.JobPath, buildNum))

	inputNotified := false
	for {
		detail, err := d.Client.GetBuild(ctx, res.JobPath, buildNum)
		if err != nil {
			return res, fmt.Errorf("waiting for build %s #%d: %w", res.JobPath, buildNum, err)
		}
		status := detail.Status
		if len(detail.PendingInputs) > 0 && !inputNotified {
			inputNotified = true
			notify("build is paused waiting for input: " + detail.PendingInputs[0].Message)
		}
		if status != jmodel.BuildStatusRunning && status != jmodel.BuildStatusPausedInput {
			res.Status = status
			return res, nil
		}
		if err := sleepCtx(ctx, buildPollInterval); err != nil {
			return res, fmt.Errorf("waiting for build %s #%d: %w", res.JobPath, buildNum, err)
		}
	}
}

// sleepCtx sleeps for dur or until ctx is done, returning ctx.Err() in that case.
func sleepCtx(ctx context.Context, dur time.Duration) error {
	t := time.NewTimer(dur)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Replay reruns a build with an edited pipeline script, queuing a new build.
func (d Deps) Replay(ctx context.Context, jobPath string, buildNum int, script string) error {
	return d.Client.ReplayBuild(ctx, jobPath, buildNum, script)
}

// Cancel aborts a running build. The build number is required by callers; this
// verb never defaults to the latest build.
func (d Deps) Cancel(ctx context.Context, jobPath string, number int) error {
	return d.Client.CancelBuild(ctx, jobPath, number)
}

// Dequeue removes a still-queued build from the Jenkins queue by its queue id.
func (d Deps) Dequeue(ctx context.Context, id int64) error {
	return d.Client.CancelQueueItem(ctx, id)
}

// SetJobEnabled toggles a job's buildable flag.
func (d Deps) SetJobEnabled(ctx context.Context, jobPath string, enabled bool) error {
	return d.Client.SetJobEnabled(ctx, jobPath, enabled)
}

// ApproveInput proceeds a paused pipeline input step. inputID may be empty when
// the build has exactly one pending input; the resolved id is returned.
func (d Deps) ApproveInput(ctx context.Context, jobPath string, buildNum int, inputID string, params map[string]string) (string, error) {
	id, err := d.ResolveInputID(ctx, jobPath, buildNum, inputID)
	if err != nil {
		return "", err
	}
	return id, d.Client.ProceedInput(ctx, jobPath, buildNum, id, params)
}

// RejectInput aborts a paused pipeline input step. inputID may be empty when the
// build has exactly one pending input; the resolved id is returned.
func (d Deps) RejectInput(ctx context.Context, jobPath string, buildNum int, inputID string) (string, error) {
	id, err := d.ResolveInputID(ctx, jobPath, buildNum, inputID)
	if err != nil {
		return "", err
	}
	return id, d.Client.AbortInput(ctx, jobPath, buildNum, id)
}

// Rescan triggers a repository scan ("Scan Repository Now" on a multibranch
// project, "Scan Folder Now" on an organization folder). Both are the container
// enqueueing itself through the same /build endpoint a job uses. Jobs are
// refused when the cache can classify them; a plain folder is not classifiable
// as unscannable here, so Jenkins' own error surfaces for those.
func (d Deps) Rescan(ctx context.Context, folderPath, jobPath string) error {
	if j, ok := cache.LookupJob(d.Store, folderPath, jobPath); ok &&
		j.Type != jmodel.JobTypeMultiBranch && j.Type != jmodel.JobTypeFolder {
		return fmt.Errorf("%s is not a multibranch project or folder; scanning only applies to those", navmsg.DecodePath(jobPath))
	}
	_, err := d.Client.TriggerBuild(ctx, jobPath, nil)
	return err
}

// SetNodeOffline brings a node to the requested temporarily-offline state.
// Idempotent: a node already in the target state is a no-op. Returns an error
// naming the available nodes when name is unknown.
func (d Deps) SetNodeOffline(ctx context.Context, name string, wantOffline bool, reason string) error {
	nodes, err := d.Client.ListNodes(ctx)
	if err != nil {
		return err
	}
	var node *jmodel.Node
	var names []string
	for i := range nodes {
		names = append(names, nodes[i].Name)
		if nodes[i].Name == name {
			node = &nodes[i]
		}
	}
	if node == nil {
		return fmt.Errorf("node %q not found; available: %v", name, names)
	}
	if node.Offline != wantOffline {
		return d.Client.ToggleNodeOffline(ctx, name, reason)
	}
	return nil
}
