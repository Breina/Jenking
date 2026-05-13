package action

import (
	"context"
	"io"
)

// runLogs fetches the full console text for a build and writes it to w.
func runLogs(ctx context.Context, client apiClient, jobPath string, buildNum int, w io.Writer) error {
	text, err := client.GetFullConsoleText(ctx, jobPath, buildNum)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, text)
	return err
}
