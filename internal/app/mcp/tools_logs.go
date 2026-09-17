package mcp

import (
	"context"
	"fmt"
	"regexp"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Breina/Jenking/internal/app/usecase"
)

// maxLogWindow caps the inline log peek so a get_logs call can never flood the
// model's context — the file on disk is the real payload.
const maxLogWindow = 16 * 1024

// Inline line-window and search bounds.
const (
	defaultLogLines   = 200
	maxLogLines       = 2000
	defaultLogMatches = 50
	maxLogMatches     = 500
	maxContextLines   = 10
)

// registerLogTools registers the log file-handoff tool.
func (s *Server) registerLogTools() {
	d := s.deps

	mcp.AddTool(s.srv, &mcp.Tool{
		Name: "get_logs",
		Description: "Write a build's console log to a file and return its path, size, and whether it is complete. " +
			"The log is intentionally NOT returned inline — grep or read the file with your own shell instead of loading it into context. " +
			"Set stage to scope to one pipeline stage. To read part of the log inline, page by lines with start_line/max_lines " +
			"(negative start_line reads the tail) or by bytes with max_bytes (<=16384) from offset_bytes. " +
			"To find specific lines, prefer search_logs. Omit build_number for the latest build.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getLogsIn) (*mcp.CallToolResult, getLogsOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		n, err := d.ResolveBuild(ctx, in.JobPath, in.BuildNumber)
		if err != nil {
			return nil, getLogsOut{}, err
		}
		lf, err := d.LogToFile(ctx, in.JobPath, n, in.Stage)
		if err != nil {
			return nil, getLogsOut{}, err
		}
		out := getLogsOut{BuildNumber: n, Path: lf.Path, SizeBytes: lf.Size, Complete: lf.Complete}
		if in.MaxBytes > 0 {
			out.Window = logWindow(lf.Text, in.OffsetBytes, in.MaxBytes)
		}
		if in.StartLine != 0 {
			out.Lines, out.FirstLine, out.TotalLines = lineWindow(lf.Text, in.StartLine, in.MaxLines)
		}
		return nil, out, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name: "search_logs",
		Description: "Search a build's console log (or one stage's) with a regex on the server and return only the matching lines, " +
			"with line numbers and optional context lines. Use it to find errors or specific output without loading the log, " +
			"and whenever you cannot read the file get_logs returns. Omit build_number for the latest build.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchLogsIn) (*mcp.CallToolResult, searchLogsOut, error) {
		re, err := regexp.Compile(in.Pattern)
		if err != nil {
			return nil, searchLogsOut{}, fmt.Errorf("invalid pattern: %w", err)
		}
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		n, err := d.ResolveBuild(ctx, in.JobPath, in.BuildNumber)
		if err != nil {
			return nil, searchLogsOut{}, err
		}
		lf, err := d.LogToFile(ctx, in.JobPath, n, in.Stage)
		if err != nil {
			return nil, searchLogsOut{}, err
		}
		maxMatches := min(intOr(in.MaxMatches, defaultLogMatches), maxLogMatches)
		matches, total := usecase.SearchText(lf.Text, re, maxMatches, min(max(in.ContextLines, 0), maxContextLines))
		return nil, searchLogsOut{
			BuildNumber: n,
			Matches: mapSlice(matches, func(m usecase.LogMatch) logMatchOut {
				return logMatchOut{LineNumber: m.LineNumber, Line: m.Line, Before: m.Before, After: m.After}
			}),
			TotalMatches: total,
			Truncated:    total > len(matches),
			Complete:     lf.Complete,
			Path:         lf.Path,
			SizeBytes:    lf.Size,
		}, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name: "get_artifact",
		Description: "Download one archived artifact to a file and return its path, size, and content type. " +
			"Always use this instead of fetching the URL from list_artifacts: artifact URLs require Jenkins authentication and a plain fetch returns 403. " +
			"The content is NOT returned inline — read or grep the file with your own shell. " +
			"Set max_bytes (<=16384) for a small inline window from offset_bytes when you must peek. " +
			"Omit build_number for the latest build.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getArtifactIn) (*mcp.CallToolResult, getArtifactOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		n, err := d.ResolveBuild(ctx, in.JobPath, in.BuildNumber)
		if err != nil {
			return nil, getArtifactOut{}, err
		}
		af, err := d.ArtifactToFile(ctx, in.JobPath, n, in.Name)
		if err != nil {
			return nil, getArtifactOut{}, err
		}
		out := getArtifactOut{
			BuildNumber: n,
			Name:        af.Name,
			Path:        af.Path,
			SizeBytes:   af.Size,
			ContentType: af.ContentType,
		}
		if in.MaxBytes > 0 {
			out.Window = logWindow(af.Text, in.OffsetBytes, in.MaxBytes)
		}
		return nil, out, nil
	})

	mcp.AddTool(s.srv, &mcp.Tool{
		Name: "get_scan_log",
		Description: "Write a container's repository scan log to a file and return its path, size, and whether the scan has finished. " +
			"Covers branch indexing on a multibranch project and the computation log of an organization folder. " +
			"Jenkins keeps only the latest scan per container, so there is no build number to pass. " +
			"As with get_logs the content is NOT returned inline — grep the file. Set max_bytes (<=16384) to peek from offset_bytes.",
		Annotations: readOnlyHint(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getScanLogIn) (*mcp.CallToolResult, getLogsOut, error) {
		in.JobPath = d.CanonicalJobPath(ctx, in.JobPath)
		lf, err := d.ScanLogToFile(ctx, in.JobPath)
		if err != nil {
			return nil, getLogsOut{}, err
		}
		out := getLogsOut{Path: lf.Path, SizeBytes: lf.Size, Complete: lf.Complete}
		if in.MaxBytes > 0 {
			out.Window = logWindow(lf.Text, in.OffsetBytes, in.MaxBytes)
		}
		return nil, out, nil
	})
}

// lineWindow returns an inline line window bounded by maxLogLines and, like
// logWindow, the maxLogWindow byte ceiling.
func lineWindow(text string, startLine, maxLines int) (lines string, first, total int) {
	lines, first, total = usecase.LineWindow(text, startLine, min(intOr(maxLines, defaultLogLines), maxLogLines))
	if len(lines) > maxLogWindow {
		lines = lines[:maxLogWindow]
	}
	return lines, first, total
}

// logWindow returns up to max bytes of text starting at offset, clamped to the
// maxLogWindow ceiling and the text bounds.
func logWindow(text string, offset, max int) string {
	if offset < 0 || offset >= len(text) {
		return ""
	}
	if max > maxLogWindow {
		max = maxLogWindow
	}
	end := offset + max
	if end > len(text) {
		end = len(text)
	}
	return text[offset:end]
}
