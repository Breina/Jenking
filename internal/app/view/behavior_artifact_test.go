package view

import (
	"testing"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/tui/theme"
)

func makeArtifactBehavior(store *cache.Store, nc NavigationContext, build jmodel.Build) *artifactBehavior {
	return newArtifactBehavior(theme.Default(), nil, func() *cache.Store { return store },
		fixedBuildAccessor(&nc, &build), swapTo)
}

// TestArtifactBehavior_UsesDeliveredList is the regression for artifacts not
// appearing at the end of a build. fetchArtifacts only writes the store cache
// once the registry agrees the build is terminal, which lags the view's own
// observation of the finish; the behavior read that cache and nothing else,
// so the freshly fetched list was dropped on the floor and "A" stayed hidden
// until the build was reopened. The delivered message is now the primary
// source.
func TestArtifactBehavior_UsesDeliveredList(t *testing.T) {
	nc := NavigationContext{Level: CtxBranch, ProjectName: "job", BranchName: "main",
		Build: NavBuildRef{Number: 7}}
	b := makeArtifactBehavior(nil, nc, jmodel.Build{Number: 7}) // no cache at all

	if _, ok := b.Shortcut(); ok {
		t.Fatal("shortcut offered before any artifacts were delivered")
	}
	handled, _ := b.HandleMsg(ArtifactsMsg{JobPath: nc.JobPath(), BuildNum: 7,
		Artifacts: []jmodel.Artifact{{DisplayPath: "report.html", URL: "http://x/report.html"}}})
	if handled {
		t.Error("ArtifactsMsg was consumed; list views tracking the same broadcast would starve")
	}
	if _, ok := b.Shortcut(); !ok {
		t.Fatal("no artifact shortcut after ArtifactsMsg delivered a non-empty list")
	}
}

// TestArtifactBehavior_IgnoresOtherBuilds verifies the delivered list is
// keyed to its build, so a message for a neighbouring build cannot light up
// the shortcut for the one on screen.
func TestArtifactBehavior_IgnoresOtherBuilds(t *testing.T) {
	nc := NavigationContext{Level: CtxBranch, ProjectName: "job", BranchName: "main",
		Build: NavBuildRef{Number: 7}}
	b := makeArtifactBehavior(nil, nc, jmodel.Build{Number: 7})

	b.HandleMsg(ArtifactsMsg{JobPath: nc.JobPath(), BuildNum: 6,
		Artifacts: []jmodel.Artifact{{DisplayPath: "other.txt"}}})
	if _, ok := b.Shortcut(); ok {
		t.Error("shortcut offered from another build's artifact list")
	}
}

// TestArtifactBehavior_IgnoresFailedFetch verifies an errored fetch does not
// overwrite a good list with an empty one.
func TestArtifactBehavior_IgnoresFailedFetch(t *testing.T) {
	nc := NavigationContext{Level: CtxBranch, ProjectName: "job", BranchName: "main",
		Build: NavBuildRef{Number: 7}}
	b := makeArtifactBehavior(nil, nc, jmodel.Build{Number: 7})

	b.HandleMsg(ArtifactsMsg{JobPath: nc.JobPath(), BuildNum: 7,
		Artifacts: []jmodel.Artifact{{DisplayPath: "report.html"}}})
	b.HandleMsg(ArtifactsMsg{JobPath: nc.JobPath(), BuildNum: 7, Err: errFetch})
	if _, ok := b.Shortcut(); !ok {
		t.Error("a failed fetch dropped the artifact list that had already arrived")
	}
}

var errFetch = errTest("fetch failed")

type errTest string

func (e errTest) Error() string { return string(e) }
