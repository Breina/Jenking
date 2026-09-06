package jmodel

import "time"

// Change is a single SCM commit associated with a build. It is plugin-agnostic:
// the same shape covers freestyle (AbstractBuild.changeSet) and pipeline
// (WorkflowRun.changeSets) sources. AffectedPaths may be empty when the SCM or
// the tree query omits it.
type Change struct {
	CommitID      string
	Author        string
	AuthorEmail   string
	Message       string
	Timestamp     time.Time
	AffectedPaths []string
}

// BuildCommitHit records that a build's change set contains a commit matching a
// search prefix. Returned by JenkinsClient.FindCommit to answer "which build
// contains commit X".
type BuildCommitHit struct {
	BuildNumber int
	CommitID    string
}
