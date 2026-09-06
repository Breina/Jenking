//go:build integration

package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/Breina/Jenking/test/e2e/harness"
)

// TestLiveFix_StageViewBarTransitionsAfterBuildCompletes is a one-off live
// regression for the "stuck running bar" fix. It navigates to the
// `Omgeving` folder's `migratie/jenkins-e2e` fixture (the live setup at
// jenkins-on.cumuli.be), triggers a build, and asserts the synthetic
// Pipeline row in the StageView transitions away from "Running" within the
// build's lifetime + refresh-chain margin. Skips if that exact path isn't
// present.
func TestLiveFix_StageViewBarTransitionsAfterBuildCompletes(t *testing.T) {
	h := harness.New(t, harness.Options{BinaryPath: binaryPath, Context: "ontwikkel"})
	openAllJobs(t, h)

	if !h.Contains("Omgeving") {
		t.Skip("expected `Omgeving` folder in the job list; live setup differs")
	}

	// Open Omgeving folder.
	h.SendKeys("/")
	h.MustWaitFor(t, func(g string) bool {
		return strings.Contains(strings.ToLower(g), "search") || strings.Contains(g, ">")
	}, harness.RenderTimeout)
	h.SendKeys("Omgeving<cr>")
	h.MustWaitFor(t, func(g string) bool { return strings.Contains(g, "Omgeving") }, harness.NetworkTimeout)
	h.SendKeys("<enter>")
	h.MustWaitFor(t, func(g string) bool { return strings.Contains(g, "jobs(") }, harness.NetworkTimeout)

	// Find the jenkins-e2e fixture in this folder.
	if !h.Contains("jenkins-e2e") {
		h.SendKeys("/")
		h.MustWaitFor(t, func(g string) bool {
			return strings.Contains(strings.ToLower(g), "search") || strings.Contains(g, ">")
		}, harness.RenderTimeout)
		h.SendKeys("jenkins-e2e<cr>")
	}
	if !poll(h, func(g string) bool { return strings.Contains(g, "jenkins-e2e") }, harness.NetworkTimeout) {
		t.Skip("jenkins-e2e fixture not found in Omgeving folder")
	}

	// Open the multibranch fixture → branches list.
	h.SendKeys("<enter>")
	h.MustWaitFor(t, func(g string) bool { return strings.Contains(g, "jobs(") }, harness.NetworkTimeout)

	// Open first branch → builds view.
	h.SendKeys("<enter>")
	h.MustWaitFor(t, func(g string) bool { return strings.Contains(g, "builds(") }, harness.NetworkTimeout)

	// Trigger via 't'; submit param dialog with defaults via ctrl+s.
	h.SendKeys("t")
	h.MustWaitFor(t, func(g string) bool {
		g = strings.ToLower(g)
		return strings.Contains(g, "fail") || strings.Contains(g, "speed") || strings.Contains(g, "parameter")
	}, harness.RenderTimeout)
	h.SendKeys("<c-s>")

	// Pending → live stages view.
	h.MustWaitFor(t, func(g string) bool {
		return strings.Contains(g, "stages(") || strings.Contains(g, "Pending")
	}, harness.NetworkTimeout)

	// Wait for the synthetic Pipeline row to enter Running.
	if err := h.WaitFor(func(g string) bool {
		return strings.Contains(g, "Pipeline") && strings.Contains(g, "Running")
	}, harness.NetworkTimeout); err != nil {
		t.Fatalf("Pipeline row never reached Running state:\n%s", h.Grid())
	}
	h.MustSnapshot(t, "live-bar-during-build")

	// THE assertion: bar must transition away from Running before the test
	// times out. With the fix, this transitions within ~2s of build completion;
	// the build itself is ~10s.
	err := h.WaitFor(func(g string) bool {
		return strings.Contains(g, "Pipeline") && !pipelineRowSaysRunning(g)
	}, 120*time.Second)
	if err != nil {
		h.MustSnapshot(t, "live-stuck-running-FAILED")
		t.Fatalf("Pipeline row remained Running past the build's lifetime — bug not fixed.\nGrid:\n%s", h.Grid())
	}
	h.MustSnapshot(t, "live-bar-after-completion")
}
