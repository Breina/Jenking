package usecase

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// FollowPollInterval is how often a follow re-reads the log while waiting for
// its pattern. Console output arrives in bursts, so this is tighter than the
// build-state wait interval.
const FollowPollInterval = 3 * time.Second

// FollowOptions parameterizes following a log until a pattern shows up.
type FollowOptions struct {
	// Pattern is the RE2 regexp the follow waits for; required.
	Pattern *regexp.Regexp
	// Deadline bounds the follow; a zero deadline means "until the log ends".
	Deadline time.Time
	// Poll overrides FollowPollInterval when set.
	Poll time.Duration
	// OnPoll, when set, is called after every poll that did not finish the
	// follow, with the bytes seen so far. It exists for progress reporting.
	OnPoll func(sizeBytes int)
}

// FollowResult is the outcome of following a log. Exactly one of Matched,
// Complete (log ended without a match), or TimedOut is the reason the follow
// returned. File carries the log materialized to disk, as with LogToFile: the
// text itself never travels back to the caller's context.
type FollowResult struct {
	Matched     bool
	MatchText   string
	Line        string
	LineNumber  int
	OffsetBytes int
	Complete    bool
	TimedOut    bool
	File        LogFile
}

// FollowBuildLog follows a build's console log (stage-scoped when stage is
// non-empty) until opt.Pattern matches, the build stops producing output, or
// the deadline passes.
func (d Deps) FollowBuildLog(ctx context.Context, jobPath string, buildNum int, stage string, opt FollowOptions) (FollowResult, error) {
	if d.Store == nil || d.Store.Disk == nil {
		return FollowResult{}, fmt.Errorf("log file handoff requires the disk cache")
	}
	poll := d.stageLogPoller(jobPath, buildNum, stage)
	if stage == "" {
		poll = d.progressiveLogPoller(func(ctx context.Context, start int) (*jmodel.ProgressiveLog, error) {
			return d.Client.GetProgressiveLog(ctx, jobPath, buildNum, start)
		})
	}
	return followLog(ctx, poll, func(text string) (LogFile, error) {
		path, size, err := d.Store.Disk.SaveBuildLog(jobPath, buildNum, stage, text)
		return LogFile{Path: path, Size: size, Text: text}, err
	}, opt)
}

// FollowScanLog follows a container's repository scan log until opt.Pattern
// matches, the scan finishes, or the deadline passes. Jenkins keeps only the
// latest scan per container, so there is no build number.
func (d Deps) FollowScanLog(ctx context.Context, jobPath string, opt FollowOptions) (FollowResult, error) {
	if d.Store == nil || d.Store.Disk == nil {
		return FollowResult{}, fmt.Errorf("log file handoff requires the disk cache")
	}
	poll := d.progressiveLogPoller(func(ctx context.Context, start int) (*jmodel.ProgressiveLog, error) {
		return d.Client.GetScanLogProgressive(ctx, jobPath, start)
	})
	return followLog(ctx, poll, func(text string) (LogFile, error) {
		path, size, err := d.Store.Disk.SaveScanLog(jobPath, text)
		return LogFile{Path: path, Size: size, Text: text}, err
	}, opt)
}

// logPoller returns the whole log seen so far and whether it is finished. Each
// call picks up wherever the previous one stopped.
type logPoller func(ctx context.Context) (text string, complete bool, err error)

// progressiveLogPoller accumulates a progressive (byte-offset) log across
// polls, so each poll transfers only the bytes appended since the last one.
func (d Deps) progressiveLogPoller(fetch func(ctx context.Context, start int) (*jmodel.ProgressiveLog, error)) logPoller {
	var b strings.Builder
	start := 0
	return func(ctx context.Context) (string, bool, error) {
		for {
			pl, err := fetch(ctx, start)
			if err != nil {
				return "", false, err
			}
			b.WriteString(pl.Text)
			if !pl.MoreData {
				return b.String(), true, nil
			}
			// Caught up to a still-writing log: stop this poll, don't spin.
			if pl.NextStart <= start {
				return b.String(), false, nil
			}
			start = pl.NextStart
		}
	}
}

// stageLogPoller re-reads one stage's log in full each poll: stage logs are
// assembled from per-node fetches and offer no incremental offset.
func (d Deps) stageLogPoller(jobPath string, buildNum int, stage string) logPoller {
	return func(ctx context.Context) (string, bool, error) {
		return d.fetchStageLog(ctx, jobPath, buildNum, stage)
	}
}

// followLog is the shared follow loop: poll, scan the accumulated text for the
// pattern, and stop on a match, on the log ending, or on the deadline. The log
// is written to disk on every exit path so the caller always gets a file.
func followLog(ctx context.Context, poll logPoller, save func(text string) (LogFile, error), opt FollowOptions) (FollowResult, error) {
	if opt.Pattern == nil {
		return FollowResult{}, fmt.Errorf("follow requires a pattern")
	}
	interval := opt.Poll
	if interval <= 0 {
		interval = FollowPollInterval
	}
	for {
		text, complete, err := poll(ctx)
		if err != nil {
			return FollowResult{}, err
		}
		res := FollowResult{Complete: complete}
		if loc := opt.Pattern.FindStringIndex(text); loc != nil {
			res.Matched = true
			res.MatchText = text[loc[0]:loc[1]]
			res.OffsetBytes = loc[0]
			res.Line, res.LineNumber = lineAt(text, loc[0])
		}
		timedOut := !opt.Deadline.IsZero() && !time.Now().Before(opt.Deadline)
		if res.Matched || complete || timedOut {
			res.TimedOut = timedOut && !res.Matched && !complete
			file, err := save(text)
			if err != nil {
				return FollowResult{}, err
			}
			file.Complete = complete
			res.File = file
			return res, nil
		}
		if opt.OnPoll != nil {
			opt.OnPoll(len(text))
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return FollowResult{}, ctx.Err()
		case <-timer.C:
		}
	}
}

// lineAt returns the whole line containing byte offset off and its 1-based
// line number, so a match is reported with the context a human reads.
func lineAt(text string, off int) (string, int) {
	start := strings.LastIndexByte(text[:off], '\n') + 1
	end := strings.IndexByte(text[off:], '\n')
	if end < 0 {
		end = len(text)
	} else {
		end += off
	}
	return strings.TrimRight(text[start:end], "\r"), strings.Count(text[:start], "\n") + 1
}
