package cache

import (
	"path/filepath"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// LivePoll is the result of one Jenkins live poll (running builds + queue),
// published to the shared cache dir so sibling Jenking processes on the same
// machine reuse it instead of each hitting the controller on their own tick.
// A disk read is far cheaper than the round trip it replaces.
//
// Exported fields so it can be gob-encoded.
type LivePoll struct {
	Stamp   time.Time // when this round was claimed or published
	PID     int       // process that owns/owned the round
	Running []jmodel.UserBuild
	Queue   []jmodel.QueueItem
	QueueOK bool // false when the queue fetch failed (consumers skip the sample)
}

func (d *DiskStore) livePollPath() string { return filepath.Join(d.dir, "livepoll.gob") }

// ClaimPoll reports whether another process's live poll is still hot. When it
// is, that snapshot is returned with hot=true and the caller must serve from it
// rather than calling Jenkins.
//
// When it is stale, absent, or was published by this process, ClaimPoll claims
// the round for us — bumping the stamp under the cross-process lock, so other
// processes back off for one cycle — and returns hot=false, meaning the caller
// owns this round trip and should PublishPoll its result. Steady state is one
// poller: whoever publishes keeps claiming (its own stamp never gates it), and
// the others idle until that process exits or stalls for longer than maxAge.
//
// Best effort: any disk error yields hot=false, so the caller falls back to a
// direct fetch.
func (d *DiskStore) ClaimPoll(maxAge time.Duration) (LivePoll, bool) {
	var lp LivePoll
	hot := false
	_ = d.withLock(func() error {
		existing, err := readGob[LivePoll](d.livePollPath())
		if err == nil && existing.PID != d.owner {
			// A negative age means the clock jumped backwards; treat the stamp as
			// unusable and reclaim rather than idling until it catches up.
			if age := time.Since(existing.Stamp); age >= 0 && age < maxAge {
				lp, hot = existing, true
				return nil
			}
		}
		// Claim the round. The previous payload is kept so a sibling ticking
		// between our claim and our publish still serves the last known state
		// instead of an empty one — and so a crash mid-round costs one cycle.
		existing.Stamp = time.Now()
		existing.PID = d.owner
		return writeGob(d.livePollPath(), existing)
	})
	return lp, hot
}

// PublishPoll writes the result of a round this process claimed, for sibling
// processes to consume via ClaimPoll.
func (d *DiskStore) PublishPoll(running []jmodel.UserBuild, queue []jmodel.QueueItem, queueOK bool) error {
	return d.withLock(func() error {
		return writeGob(d.livePollPath(), LivePoll{
			Stamp:   time.Now(),
			PID:     d.owner,
			Running: running,
			Queue:   queue,
			QueueOK: queueOK,
		})
	})
}
