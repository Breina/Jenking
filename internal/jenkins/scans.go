package jenkins

import (
	"context"
	"fmt"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// StopScan aborts a container's running repository scan. Jenkins exposes this
// as a POST on the computation object; there is no JSON status to check first,
// so callers must already know a scan is running — in practice, because they
// are streaming its log and Jenkins still reports more data to come.
//
// Cancelling a scan that is merely *queued* is a different operation entirely:
// that one is removed by queue id via CancelQueueItem.
func (c *Client) StopScan(ctx context.Context, jobPath string) error {
	if err := c.post(ctx, scanRunURL(jobPath)+"/stop", nil); err != nil {
		return fmt.Errorf("stopping scan of %s: %w", jmodel.JobPathToURL(jobPath), err)
	}
	return nil
}
