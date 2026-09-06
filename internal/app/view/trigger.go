package view

import (
	"context"

	"github.com/Breina/Jenking/internal/domain/jmodel"
	tea "github.com/charmbracelet/bubbletea"
)

// fetchJobParams returns a tea.Cmd that fetches parameter definitions for a job.
func fetchJobParams(client jmodel.JenkinsClient, nc NavigationContext) tea.Cmd {
	return func() tea.Msg {
		params, err := client.GetJobParameters(context.Background(), nc.JobPath())
		return JobParamsMsg{
			NC:     nc,
			Params: params,
			Err:    err,
		}
	}
}

// triggerBuild returns a tea.Cmd that triggers a build with the given parameters.
func triggerBuild(client jmodel.JenkinsClient, nc NavigationContext, params map[string]string) tea.Cmd {
	return func() tea.Msg {
		_, err := client.TriggerBuild(context.Background(), nc.JobPath(), params)
		return TriggerBuildResultMsg{NC: nc, Err: err}
	}
}

// triggerScan returns a tea.Cmd that starts a container's repository scan. It
// posts to the same /build endpoint a job does — for a multibranch project or
// folder, that endpoint means "scan now" — but reports its own result type, so
// the caller does not wait for a build number that will never exist.
func triggerScan(client jmodel.JenkinsClient, nc NavigationContext, jobPath string) tea.Cmd {
	return func() tea.Msg {
		_, err := client.TriggerBuild(context.Background(), jobPath, nil)
		return TriggerScanResultMsg{NC: nc, JobPath: jobPath, Err: err}
	}
}

// replayBuild returns a tea.Cmd that replays a build using the given Groovy script.
func replayBuild(client jmodel.JenkinsClient, nc NavigationContext, sourceBuildNum int, script string) tea.Cmd {
	return func() tea.Msg {
		err := client.ReplayBuild(context.Background(), nc.JobPath(), sourceBuildNum, script)
		return TriggerBuildResultMsg{NC: nc, Err: err}
	}
}
