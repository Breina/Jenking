package view

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// DeepLinkKind classifies the view a clipboard URL points to.
type DeepLinkKind int

const (
	// DeepLinkBuilds opens a builds list (project or branch scope).
	DeepLinkBuilds DeepLinkKind = iota
	// DeepLinkStages opens the pipeline-graph for a specific build.
	DeepLinkStages
	// DeepLinkLogs opens the console output for a specific build.
	DeepLinkLogs
	// DeepLinkJobs opens the job list for a folder.
	DeepLinkJobs
)

// DeepLink is a parsed Jenkins web URL ready to drive App navigation.
type DeepLink struct {
	Kind  DeepLinkKind
	NC    NavigationContext
	Label string // short header label, e.g. "log #42 · project/MR-3"
}

// ParseJenkinsURL extracts a deeplink from rawURL, rejecting any URL whose
// origin does not match baseURL. The job cache is consulted to split the
// /job/<name>/... chain into folder + project + branch; when the cache is cold
// the presence of a Jenkins /view/<name>/ marker is used as a fallback signal
// that the leaf segment is a multibranch branch rather than the project. The
// marker's name is also captured into NC.ViewName, so pasting a view URL lands
// on the job list filtered by that view.
func ParseJenkinsURL(baseURL, rawURL string, store *cache.Store) (*DeepLink, error) {
	segs, err := jmodel.URLPathSegments(baseURL, rawURL)
	if err != nil {
		return nil, err
	}
	chain := jmodel.WalkJobChain(segs)
	if len(chain.Segs) == 0 {
		// A bare view URL (…/view/Team%20Infra/) names no job, but it does name
		// a job list: the view's own.
		if chain.ViewName != "" {
			return viewDeepLink(chain.ViewName), nil
		}
		return nil, errors.New("URL has no /job/ segments")
	}

	folderPath, projectName, branchName := resolveJobSegs(chain.Segs, store, chain.HadView)
	buildNum, trailingKind := splitTrailing(chain.Trailing)
	nc := buildDeepLinkNC(folderPath, projectName, branchName, buildNum)
	nc.ViewName = chain.ViewName

	kind, kindLabel := classifyDeepLink(buildNum, trailingKind, nc.Level)
	return &DeepLink{
		Kind:  kind,
		NC:    nc,
		Label: formatDeepLinkLabel(kindLabel, buildNum, nc),
	}, nil
}

// viewDeepLink builds the deeplink for a bare Jenkins view URL: the job list
// filtered by that view.
func viewDeepLink(viewName string) *DeepLink {
	return &DeepLink{
		Kind:  DeepLinkJobs,
		NC:    NavigationContext{Level: CtxRoot, ViewName: viewName},
		Label: "jobs · " + viewName,
	}
}

// splitTrailing extracts (build number, view-kind segment) from the part of
// the URL after the /job/ chain. A trailing token is interpreted as a build
// number iff it parses as a positive integer; otherwise it is treated as a
// view-kind hint at the branch level.
func splitTrailing(trailing []string) (buildNum int, trailingKind string) {
	if len(trailing) == 0 {
		return 0, ""
	}
	if n, err := strconv.Atoi(trailing[0]); err == nil && n > 0 {
		if len(trailing) > 1 {
			return n, trailing[1]
		}
		return n, ""
	}
	return 0, trailing[0]
}

// buildDeepLinkNC composes the NavigationContext from resolved path parts,
// picking the deepest implied Level and applying AtBuild when a build number
// was supplied.
func buildDeepLinkNC(folderPath, projectName, branchName string, buildNum int) NavigationContext {
	nc := NavigationContext{
		FolderPath:  folderPath,
		ProjectName: projectName,
		BranchName:  branchName,
	}
	switch {
	case branchName != "":
		nc.Level = CtxBranch
	case projectName != "":
		nc.Level = CtxProject
	case folderPath != "":
		nc.Level = CtxFolder
	default:
		nc.Level = CtxRoot
	}
	if buildNum > 0 {
		nc = nc.AtBuild(buildNum)
	}
	return nc
}

// resolveJobSegs splits the chain of /job/ segments into folder + project +
// branch. When the cache knows the project full path, that boundary is used
// authoritatively; otherwise hadView (the /view/<name>/ Jenkins UI marker)
// disambiguates multibranch leafs.
func resolveJobSegs(segs []string, store *cache.Store, hadView bool) (folderPath, projectName, branchName string) {
	if store != nil {
		if folder, project, branch, ok := matchCachedProject(segs, store); ok {
			return folder, project, branch
		}
	}
	if hadView && len(segs) >= 2 {
		branchName = segs[len(segs)-1]
		segs = segs[:len(segs)-1]
	}
	switch len(segs) {
	case 0:
		return "", "", branchName
	case 1:
		return "", segs[0], branchName
	default:
		return strings.Join(segs[:len(segs)-1], "/"), segs[len(segs)-1], branchName
	}
}

// matchCachedProject finds the longest prefix of segs that equals a known
// project path. When found, the trailing segment (if any) is treated as the
// branch.
func matchCachedProject(segs []string, store *cache.Store) (folder, project, branch string, ok bool) {
	paths := cache.AllProjectPaths(store)
	if len(paths) == 0 {
		return "", "", "", false
	}
	for i := len(segs); i >= 1; i-- {
		candidate := strings.Join(segs[:i], "/")
		if containsString(paths, candidate) {
			if i == 1 {
				project = segs[0]
			} else {
				folder = strings.Join(segs[:i-1], "/")
				project = segs[i-1]
			}
			if i < len(segs) {
				branch = segs[i]
			}
			return folder, project, branch, true
		}
	}
	return "", "", "", false
}

func containsString(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

// classifyDeepLink picks the view kind from the trailing URL segment and the
// resolved navigation level. URLs without a numeric build segment fall back to
// a builds list at the resolved scope.
func classifyDeepLink(buildNum int, trailingKind string, level ContextLevel) (DeepLinkKind, string) {
	switch trailingKind {
	case "console", "consoleText", "consoleFull", "logText":
		return DeepLinkLogs, "log"
	case "pipeline-graph", "execution", "flowGraphTable":
		return DeepLinkStages, "stages"
	}
	if buildNum > 0 {
		return DeepLinkStages, "build"
	}
	if level == CtxFolder {
		return DeepLinkJobs, "jobs"
	}
	return DeepLinkBuilds, "builds"
}

// formatDeepLinkLabel renders a compact header summary like "log #42 ·
// project/MR-3" so the user can recognise the queued deeplink at a glance.
func formatDeepLinkLabel(kindLabel string, buildNum int, nc NavigationContext) string {
	target := deepLinkTarget(nc)
	switch {
	case buildNum > 0 && target != "":
		return fmt.Sprintf("%s #%d · %s", kindLabel, buildNum, target)
	case buildNum > 0:
		return fmt.Sprintf("%s #%d", kindLabel, buildNum)
	case target != "":
		return fmt.Sprintf("%s · %s", kindLabel, target)
	default:
		return kindLabel
	}
}

// deepLinkTarget joins the human-readable parts of an NC for the header
// label. Encoded segments are unescaped and any folder prefix is dropped from
// the project name so the result fits within the header column.
func deepLinkTarget(nc NavigationContext) string {
	var parts []string
	if nc.ProjectName != "" {
		parts = append(parts, shortName(decodeName(nc.ProjectName)))
	}
	if nc.BranchName != "" {
		parts = append(parts, decodeName(nc.BranchName))
	}
	if len(parts) == 0 && nc.FolderPath != "" {
		parts = append(parts, shortName(decodeName(nc.FolderPath)))
	}
	return strings.Join(parts, "/")
}
