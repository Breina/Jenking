package view

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/jenkins"
)

// fetchFailedStage returns a tea.Cmd that fetches stages for a build and
// finds the first failed one. The result is sent as a FailedStageMsg.
func fetchFailedStage(client jenkins.JenkinsClient, nc NavigationContext, build jenkins.Build) tea.Cmd {
	return func() tea.Msg {
		stages, err := client.ListStages(context.Background(), nc.JobPath(), build.Number)
		if err != nil {
			return FailedStageMsg{NC: nc, Build: build, Err: err}
		}
		msg := FailedStageMsg{NC: nc, Build: build, Stages: stages, FailedIdx: -1}
		for i := range stages {
			if stages[i].Status == jenkins.BuildStatusFailed {
				msg.FailedStage = &stages[i]
				msg.FailedIdx = i
				// No break — find the LAST failed stage
			}
		}
		return msg
	}
}
