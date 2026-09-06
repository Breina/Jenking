package view

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/brecht/jenkins-tui/internal/jenkins"
)

// fetchFailedStage returns a tea.Cmd that fetches stages for a build and
// finds the first failed one. The result is sent as a FailedStageMsg.
func fetchFailedStage(client jenkins.JenkinsClient, jobPath, jobName, branchName string, build jenkins.Build) tea.Cmd {
	return func() tea.Msg {
		stages, err := client.ListStages(context.Background(), jobPath, build.Number)
		if err != nil {
			return FailedStageMsg{JobPath: jobPath, JobName: jobName, BranchName: branchName, Build: build, Err: err}
		}
		msg := FailedStageMsg{JobPath: jobPath, JobName: jobName, BranchName: branchName, Build: build, Stages: stages, FailedIdx: -1}
		for i := range stages {
			if stages[i].Status == jenkins.BuildStatusFailed {
				msg.FailedStage = &stages[i]
				msg.FailedIdx = i
				break
			}
		}
		return msg
	}
}
