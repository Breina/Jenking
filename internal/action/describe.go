package action

import (
	"context"
	"io"
)

// runDescribe fetches the build's Jenkinsfile / replay script and writes it
// verbatim to w.
func runDescribe(ctx context.Context, client apiClient, jobPath string, buildNum int, w io.Writer) error {
	script, err := client.GetBuildScript(ctx, jobPath, buildNum)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, script)
	return err
}
