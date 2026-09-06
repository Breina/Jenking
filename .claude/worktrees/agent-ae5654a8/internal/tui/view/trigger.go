package view

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/brecht/jenkins-tui/internal/jenkins"
)

// fetchJobParams returns a tea.Cmd that fetches parameter definitions for a job.
func fetchJobParams(client jenkins.JenkinsClient, jobPath, jobName, branchName string) tea.Cmd {
	return func() tea.Msg {
		params, err := client.GetJobParameters(context.Background(), jobPath)
		return JobParamsMsg{
			JobPath:    jobPath,
			JobName:    jobName,
			BranchName: branchName,
			Params:     params,
			Err:        err,
		}
	}
}

// triggerBuild returns a tea.Cmd that triggers a build with the given parameters.
func triggerBuild(client jenkins.JenkinsClient, jobPath string, params map[string]string) tea.Cmd {
	return func() tea.Msg {
		err := client.TriggerBuild(context.Background(), jobPath, params)
		return TriggerBuildResultMsg{JobPath: jobPath, Err: err}
	}
}
