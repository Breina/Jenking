package usecase

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/navmsg"
)

// ResolveJob returns the jobs whose cached SCM URL matches scmURL (a git remote
// or web URL), using the warm reverse index in the store — no network scan. The
// query and each cached URL are canonicalized (jmodel.CanonicalSCMURL) so a
// local git remote resolves to the Jenkins branch job(s) for the same repo.
// Results are ranked primary-branch-first (main/master), then by path. Returns
// an empty slice on a cold cache or no match; callers decide the fallback.
func (d Deps) ResolveJob(scmURL string) []jmodel.JobSCMMatch {
	if d.Store == nil || d.Store.RepoURLs == nil {
		return nil
	}
	want := jmodel.CanonicalSCMURL(scmURL)
	if want == "" {
		return nil
	}
	var matches []jmodel.JobSCMMatch
	for jobPath, url := range d.Store.RepoURLs.Snapshot() {
		if url == "" || jmodel.CanonicalSCMURL(url) != want {
			continue
		}
		matches = append(matches, jmodel.JobSCMMatch{
			JobPath: jobPath,
			SCMURL:  url,
			Branch:  navmsg.DecodeName(lastPathSegment(jobPath)),
		})
	}
	sort.Slice(matches, func(i, j int) bool {
		if ri, rj := branchRank(matches[i].Branch), branchRank(matches[j].Branch); ri != rj {
			return ri < rj
		}
		return matches[i].JobPath < matches[j].JobPath
	})
	return matches
}

// branchRank sorts the conventional default branches ahead of the rest.
func branchRank(branch string) int {
	switch strings.ToLower(branch) {
	case "main", "master":
		return 0
	default:
		return 1
	}
}

// lastPathSegment returns the final slash-separated segment of p.
func lastPathSegment(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// ResolveBuild returns n when positive, otherwise the latest build number for
// jobPath. It is the MCP-facing counterpart to ResolveBuildNum (which takes a
// CLI NavBuildRef): MCP tools pass an explicit job path and an optional numeric
// build, 0 meaning "latest".
func (d Deps) ResolveBuild(ctx context.Context, jobPath string, n int) (int, error) {
	if n > 0 {
		return n, nil
	}
	builds, err := d.Client.ListBuilds(ctx, jobPath)
	if err != nil {
		return 0, fmt.Errorf("listing builds for %s: %w", jobPath, err)
	}
	if len(builds) == 0 {
		return 0, fmt.Errorf("no builds found for %s (multibranch projects require the branch in the path)", jobPath)
	}
	return builds[0].Number, nil
}

// WhoAmI returns the authenticated Jenkins user.
func (d Deps) WhoAmI(ctx context.Context) (*jmodel.User, error) {
	return d.Client.WhoAmI(ctx)
}

// CurrentUserID returns the authenticated Jenkins user's id, used to attribute
// builds for the "mine" filter.
func (d Deps) CurrentUserID(ctx context.Context) (string, error) {
	u, err := d.Client.WhoAmI(ctx)
	if err != nil {
		return "", err
	}
	return u.ID, nil
}

// keepMine returns only the builds attributed to uid — triggered by that Jenkins
// userId or carrying one of d.GitUsernames in the trigger cause. base projects
// each element onto its embedded Build.
func keepMine[T any](builds []T, uid string, gitUsernames []string, base func(T) jmodel.Build) []T {
	out := make([]T, 0, len(builds))
	for _, b := range builds {
		if base(b).MatchesUser(uid, gitUsernames) {
			out = append(out, b)
		}
	}
	return out
}

// FilterRunningMine keeps only running builds attributed to the authenticated user.
func (d Deps) FilterRunningMine(ctx context.Context, builds []jmodel.UserBuild) ([]jmodel.UserBuild, error) {
	uid, err := d.CurrentUserID(ctx)
	if err != nil {
		return nil, err
	}
	return keepMine(builds, uid, d.GitUsernames, func(b jmodel.UserBuild) jmodel.Build { return b.Build }), nil
}

// FilterBuildsMine keeps only builds attributed to the authenticated user.
func (d Deps) FilterBuildsMine(ctx context.Context, builds []jmodel.Build) ([]jmodel.Build, error) {
	uid, err := d.CurrentUserID(ctx)
	if err != nil {
		return nil, err
	}
	return keepMine(builds, uid, d.GitUsernames, func(b jmodel.Build) jmodel.Build { return b }), nil
}

// FilterProjectBuildsMine keeps only project builds attributed to the authenticated user.
func (d Deps) FilterProjectBuildsMine(ctx context.Context, builds []jmodel.ProjectBuild) ([]jmodel.ProjectBuild, error) {
	uid, err := d.CurrentUserID(ctx)
	if err != nil {
		return nil, err
	}
	return keepMine(builds, uid, d.GitUsernames, func(b jmodel.ProjectBuild) jmodel.Build { return b.Build }), nil
}

// ListJobs lists jobs and folders directly under folder ("" = root).
func (d Deps) ListJobs(ctx context.Context, folder string) ([]jmodel.Job, error) {
	return d.Client.ListJobs(ctx, folder)
}

// ListViews lists the views on a container ("" = root), followed by the
// authenticated user's personal views when listing the root. A missing
// my-views collection is not an error.
func (d Deps) ListViews(ctx context.Context, folder string) ([]jmodel.JenkinsView, error) {
	views, err := d.Client.ListViews(ctx, folder)
	if err != nil {
		return nil, err
	}
	if folder != "" {
		return views, nil
	}
	user, err := d.Client.WhoAmI(ctx)
	if err != nil {
		return views, nil
	}
	mine, err := d.Client.ListMyViews(ctx, user.ID)
	if err != nil {
		return views, nil
	}
	return append(views, mine...), nil
}

// ListViewJobs lists the jobs a named view shows. The name is resolved against
// the container's views so callers never need to know a view's kind.
func (d Deps) ListViewJobs(ctx context.Context, folder, viewName string) ([]jmodel.Job, error) {
	views, err := d.ListViews(ctx, folder)
	if err != nil {
		return nil, err
	}
	for _, v := range views {
		if v.Name == viewName {
			return d.Client.ListViewJobs(ctx, v)
		}
	}
	for _, v := range views {
		if strings.EqualFold(v.Name, viewName) {
			return d.Client.ListViewJobs(ctx, v)
		}
	}
	return nil, fmt.Errorf("no such view: %s", viewName)
}

// ListBuilds lists a job's builds (newest first).
func (d Deps) ListBuilds(ctx context.Context, jobPath string) ([]jmodel.Build, error) {
	return d.Client.ListBuilds(ctx, jobPath)
}

// GetBuild returns full detail for a specific build.
func (d Deps) GetBuild(ctx context.Context, jobPath string, number int) (*jmodel.BuildDetail, error) {
	return d.Client.GetBuild(ctx, jobPath, number)
}

// GetStages returns a build's pipeline stages.
func (d Deps) GetStages(ctx context.Context, jobPath string, number int) ([]jmodel.Stage, error) {
	return d.Client.ListStages(ctx, jobPath, number)
}

// GetTestReport returns a build's test report.
func (d Deps) GetTestReport(ctx context.Context, jobPath string, number int) (*jmodel.TestReport, error) {
	return d.Client.GetTestReport(ctx, jobPath, number)
}

// ListArtifacts returns a build's artifacts.
func (d Deps) ListArtifacts(ctx context.Context, jobPath string, number int) ([]jmodel.Artifact, error) {
	return d.Client.GetArtifacts(ctx, jobPath, number)
}

// GetParams returns a job's parameter definitions.
func (d Deps) GetParams(ctx context.Context, jobPath string) ([]jmodel.ParameterDefinition, error) {
	return d.Client.GetJobParameters(ctx, jobPath)
}

// GetJobMetadata returns a job's raw Jenkins JSON flattened to path=value rows.
func (d Deps) GetJobMetadata(ctx context.Context, jobPath string, depth int) ([]jmodel.MetaEntry, error) {
	tree, err := d.Client.GetJobMetadata(ctx, jobPath, depth)
	if err != nil {
		return nil, err
	}
	return tree.Flatten(), nil
}

// ListQueue returns the current build queue, every kind included.
func (d Deps) ListQueue(ctx context.Context) ([]jmodel.QueueItem, error) {
	return d.Client.ListQueue(ctx)
}

// ListQueueOfKind returns the queue narrowed to one kind. An empty kind means
// no filtering. Callers that present "the queue" to a user or an agent pass
// QueueKindBuild: branch-indexing scans share the endpoint but are not builds,
// and listing them as such is what made the counts misleading.
func (d Deps) ListQueueOfKind(ctx context.Context, kind jmodel.QueueKind) ([]jmodel.QueueItem, error) {
	items, err := d.Client.ListQueue(ctx)
	if err != nil || kind == "" {
		return items, err
	}
	out := make([]jmodel.QueueItem, 0, len(items))
	for _, it := range items {
		if it.KindOrBuild() == kind {
			out = append(out, it)
		}
	}
	return out, nil
}

// ListNodes returns per-node executor utilization and health.
func (d Deps) ListNodes(ctx context.Context) ([]jmodel.Node, error) {
	return d.Client.ListNodes(ctx)
}

// ListRunning returns the builds currently running. When a registry is present
// (fed by the engine) it answers from that truth — applying the terminal-is-
// sticky and live-confirmation invariants — and reconciles any build that just
// departed the running set. Without a registry it falls back to a live fetch.
func (d Deps) ListRunning(ctx context.Context) ([]jmodel.UserBuild, error) {
	if d.Store != nil && d.Store.Registry != nil {
		return d.Store.Registry.Query(buildregistry.Filter{OnlyRunning: true}), nil
	}
	return d.Client.ListRunningBuilds(ctx)
}

// ListInputs returns a build's pending input steps.
func (d Deps) ListInputs(ctx context.Context, jobPath string, number int) ([]jmodel.PendingInput, error) {
	detail, err := d.Client.GetBuild(ctx, jobPath, number)
	if err != nil {
		return nil, err
	}
	return detail.PendingInputs, nil
}

// GetChanges returns the SCM commits recorded for a build.
func (d Deps) GetChanges(ctx context.Context, jobPath string, number int) ([]jmodel.Change, error) {
	return d.Client.GetChanges(ctx, jobPath, number)
}

// FindCommit scans recent builds of a job for a commit matching commitPrefix.
func (d Deps) FindCommit(ctx context.Context, jobPath, commitPrefix string, maxBuilds int) ([]jmodel.BuildCommitHit, error) {
	return d.Client.FindCommit(ctx, jobPath, commitPrefix, maxBuilds)
}
