package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// Describe returns a build's Jenkinsfile / replay script verbatim.
func (d Deps) Describe(ctx context.Context, jobPath string, buildNum int) (string, error) {
	return d.Client.GetBuildScript(ctx, jobPath, buildNum)
}

// LogFile is the result of materializing a build's console log to a cache file.
// Text carries the fetched content so callers can serve a small inline window
// without re-reading the file; presentation layers must not emit Text wholesale
// (that is the whole point of the file handoff).
type LogFile struct {
	Path     string
	Size     int64
	Complete bool
	Text     string
}

// LogToFile fetches a build's console log (optionally stage-scoped) and writes
// it to a cache file, returning the path, size, and completeness. It is the
// backbone of the MCP get_logs tool: the log leaves the model's context window
// and lands on disk, where the agent greps it with its own shell.
func (d Deps) LogToFile(ctx context.Context, jobPath string, buildNum int, stage string) (LogFile, error) {
	if d.Store == nil || d.Store.Disk == nil {
		return LogFile{}, fmt.Errorf("log file handoff requires the disk cache")
	}
	text, complete, err := d.FetchLog(ctx, jobPath, buildNum, stage)
	if err != nil {
		return LogFile{}, err
	}
	path, size, err := d.Store.Disk.SaveBuildLog(jobPath, buildNum, stage, text)
	if err != nil {
		return LogFile{}, err
	}
	return LogFile{Path: path, Size: size, Complete: complete, Text: text}, nil
}

// FetchLog returns a build's console text and whether the log is complete (the
// build has finished producing output). When stage is non-empty the log is
// scoped to that pipeline stage. This is the single fetch path shared by the
// CLI `logs` verb and the MCP get_logs file handoff.
//
// complete=false means more output may still appear (the build or stage is
// running); callers targeting a file should re-fetch later to catch up.
func (d Deps) FetchLog(ctx context.Context, jobPath string, buildNum int, stage string) (text string, complete bool, err error) {
	if stage != "" {
		return d.fetchStageLog(ctx, jobPath, buildNum, stage)
	}
	return d.fetchFullLog(ctx, jobPath, buildNum)
}

// ScanLogToFile writes a container's scan log to the cache dir, mirroring
// LogToFile so agents grep a file instead of loading a scan log into context.
func (d Deps) ScanLogToFile(ctx context.Context, jobPath string) (LogFile, error) {
	if d.Store == nil || d.Store.Disk == nil {
		return LogFile{}, fmt.Errorf("log file handoff requires the disk cache")
	}
	text, complete, err := d.FetchScanLog(ctx, jobPath)
	if err != nil {
		return LogFile{}, err
	}
	path, size, err := d.Store.Disk.SaveScanLog(jobPath, text)
	if err != nil {
		return LogFile{}, err
	}
	return LogFile{Path: path, Size: size, Complete: complete, Text: text}, nil
}

// FetchScanLog reads a container's full scan log (branch indexing on a
// multibranch project, or a folder's computation). complete is false while the
// scan is still writing — the only liveness signal Jenkins gives for a scan,
// since it publishes no status object for one.
func (d Deps) FetchScanLog(ctx context.Context, jobPath string) (text string, complete bool, err error) {
	var b strings.Builder
	start := 0
	for {
		chunk, err := d.Client.GetScanLogProgressive(ctx, jobPath, start)
		if err != nil {
			return "", false, err
		}
		b.WriteString(chunk.Text)
		if !chunk.MoreData || chunk.NextStart <= start {
			return b.String(), !chunk.MoreData, nil
		}
		start = chunk.NextStart
	}
}

// fetchFullLog walks the progressive-log endpoint from the start until Jenkins
// reports no more data (build finished) or the stream catches up to a still-
// running build (an empty chunk with MoreData still set). The final MoreData
// flag is inverted into the complete result.
func (d Deps) fetchFullLog(ctx context.Context, jobPath string, buildNum int) (string, bool, error) {
	var b strings.Builder
	start := 0
	for {
		pl, err := d.Client.GetProgressiveLog(ctx, jobPath, buildNum, start)
		if err != nil {
			return "", false, err
		}
		b.WriteString(pl.Text)
		if !pl.MoreData {
			return b.String(), true, nil
		}
		// MoreData is set but the offset did not advance: we've caught up to a
		// running build. Stop with complete=false rather than spinning.
		if pl.NextStart <= start {
			return b.String(), false, nil
		}
		start = pl.NextStart
	}
}

// fetchStageLog concatenates the node logs for a single named stage
// (case-insensitive). complete reflects whether the stage has finished.
func (d Deps) fetchStageLog(ctx context.Context, jobPath string, buildNum int, stage string) (string, bool, error) {
	stages, err := d.Client.ListStages(ctx, jobPath, buildNum)
	if err != nil {
		return "", false, fmt.Errorf("listing stages: %w", err)
	}
	names := make([]string, 0, len(stages))
	for _, s := range stages {
		names = append(names, s.Name)
		if !strings.EqualFold(s.Name, stage) {
			continue
		}
		if len(s.NodeIDs) == 0 {
			return "", false, fmt.Errorf("stage %q has no log (skipped or not yet started)", s.Name)
		}
		var b strings.Builder
		for _, nodeID := range s.NodeIDs {
			part, err := d.Client.GetNodeLog(ctx, jobPath, buildNum, nodeID)
			if err != nil {
				return "", false, fmt.Errorf("fetching log for stage %q (node %d): %w", s.Name, nodeID, err)
			}
			b.WriteString(part)
			if part != "" && !strings.HasSuffix(part, "\n") {
				b.WriteString("\n")
			}
		}
		complete := s.Status != jmodel.BuildStatusRunning && s.Status != jmodel.BuildStatusPausedInput
		return b.String(), complete, nil
	}
	return "", false, fmt.Errorf("stage %q not found; available stages: %s", stage, strings.Join(names, ", "))
}
