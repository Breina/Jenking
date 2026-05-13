package action

import (
	"context"
	"fmt"

	"github.com/Breina/Jenking/internal/tui/view"
)

// resolveBuildNumber returns a concrete build number for the given NC build
// reference. When the reference carries an explicit number it's returned
// verbatim. For #last (or no reference at all) the latest build is fetched
// from the API. Returns an error when the job has no builds.
func resolveBuildNumber(ctx context.Context, client apiClient, jobPath string, ref view.NavBuildRef) (int, error) {
	if ref.Number > 0 {
		return ref.Number, nil
	}
	builds, err := client.ListBuilds(ctx, jobPath)
	if err != nil {
		return 0, fmt.Errorf("listing builds for %s: %w", jobPath, err)
	}
	if len(builds) == 0 {
		return 0, fmt.Errorf("no builds found for %s (multibranch projects require a branch)", jobPath)
	}
	// Jenkins returns builds newest-first; ListBuilds preserves that order.
	return builds[0].Number, nil
}
