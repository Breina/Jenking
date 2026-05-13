package action

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// runTests fetches the JUnit test report for a build and writes it to w as
// indented JSON. Errors when the build has no test report attached.
func runTests(ctx context.Context, client apiClient, jobPath string, buildNum int, w io.Writer) error {
	rep, err := client.GetTestReport(ctx, jobPath, buildNum)
	if err != nil {
		return err
	}
	if rep == nil {
		return fmt.Errorf("no test report for %s #%d", jobPath, buildNum)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}
