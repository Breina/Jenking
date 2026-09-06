package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxLogWindow caps the inline log peek so a get_logs call can never flood the
// model's context — the file on disk is the real payload.
const maxLogWindow = 16 * 1024

// registerLogTools registers the log file-handoff tool.
func (s *Server) registerLogTools() {
	d := s.deps

	mcp.AddTool(s.srv, &mcp.Tool{
		Name: "get_logs",
		Description: "Write a build's console log to a file and return its path, size, and whether it is complete. " +
			"The log is intentionally NOT returned inline — grep or read the file with your own shell instead of loading it into context. " +
			"Set stage to scope to one pipeline stage. Set max_bytes (<=16384) for a small inline window from offset_bytes when you must peek. " +
			"Omit build_number for the latest build.",
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
		return nil, out, nil
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
