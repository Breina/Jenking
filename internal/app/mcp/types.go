package mcp

import "github.com/Breina/Jenking/internal/app/dto"

// ---- Tool inputs (jsonschema descriptions come from the `jsonschema` tag) ----

type jobRefIn struct {
	JobPath string `json:"job_path" jsonschema:"Full slash-separated job path, e.g. TeamA/service/main"`
}

type buildRefIn struct {
	JobPath     string `json:"job_path" jsonschema:"Full slash-separated job path, e.g. TeamA/service/main"`
	BuildNumber int    `json:"build_number,omitempty" jsonschema:"Build number; omit or 0 for the latest build"`
}

type listJobsIn struct {
	Folder string `json:"folder,omitempty" jsonschema:"Folder path to list under; empty for the root"`
	View   string `json:"view,omitempty" jsonschema:"Name of a Jenkins view to list instead; its jobs may live anywhere in the folder tree"`
}

type listViewsIn struct {
	Folder string `json:"folder,omitempty" jsonschema:"Folder whose views to list; empty for the root (personal views included)"`
}

type listBuildsIn struct {
	JobPath string `json:"job_path" jsonschema:"Full slash-separated job path"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum builds to return, newest first (default 15)"`
	Mine    bool   `json:"mine,omitempty" jsonschema:"Only builds attributed to the authenticated user: triggered by them or carrying one of their git usernames in the trigger cause"`
}

type listRunningIn struct {
	Mine bool `json:"mine,omitempty" jsonschema:"Only builds attributed to the authenticated user: triggered by them or carrying one of their git usernames in the trigger cause"`
}

type waitForBuildIn struct {
	JobPath        string `json:"job_path" jsonschema:"Full slash-separated job path, e.g. TeamA/service/main"`
	BuildNumber    int    `json:"build_number,omitempty" jsonschema:"Build number; omit or 0 for the latest build"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Max seconds this call blocks, honored as-is; omit or 0 to size it from the build's estimatedDuration"`
}

type waitForBuildOut struct {
	JobPath            string          `json:"job_path"`
	BuildNumber        int             `json:"build_number"`
	Status             string          `json:"status"`
	Done               bool            `json:"done" jsonschema:"true when the build reached a terminal result or paused for input; false when the call timed out still running"`
	TimedOut           bool            `json:"timed_out,omitempty" jsonschema:"true when the wait bound elapsed before the build settled"`
	ElapsedSeconds     int             `json:"elapsed_seconds"`
	CheckBackInSeconds int             `json:"check_back_in_seconds,omitempty" jsonschema:"When timed_out, the suggested delay before calling wait_for_build again"`
	Build              dto.BuildDetail `json:"build"`
}

type waitForNewBuildIn struct {
	JobPath        string `json:"job_path" jsonschema:"Full slash-separated job path, e.g. TeamA/service/main"`
	SinceBuild     int    `json:"since_build,omitempty" jsonschema:"Baseline build number; wait for a build newer than this. Omit or 0 to baseline on the job's current latest build"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Max seconds this call blocks, honored as-is; omit or 0 for a 15-minute default"`
}

type waitForNewBuildOut struct {
	JobPath            string         `json:"job_path"`
	State              string         `json:"state" jsonschema:"started (a new build number appeared), queued (only in the queue so far), or timed_out"`
	Done               bool           `json:"done" jsonschema:"true when a new build or queue item appeared; false when the call timed out first"`
	TimedOut           bool           `json:"timed_out,omitempty"`
	BuildNumber        int            `json:"build_number,omitempty" jsonschema:"Set when state is started"`
	Status             string         `json:"status,omitempty" jsonschema:"Build status when state is started"`
	ElapsedSeconds     int            `json:"elapsed_seconds"`
	CheckBackInSeconds int            `json:"check_back_in_seconds,omitempty" jsonschema:"When timed_out, the suggested delay before calling again"`
	Build              *dto.Build     `json:"build,omitempty" jsonschema:"The new build, when state is started"`
	Queue              *dto.QueueItem `json:"queue,omitempty" jsonschema:"The queue item, when state is queued"`
}

type resolveJobIn struct {
	ScmURL string `json:"scm_url" jsonschema:"A git remote or SCM URL (e.g. git@github.com:org/repo.git or https://github.com/org/repo) to resolve to Jenkins job path(s)"`
}

type testReportIn struct {
	JobPath     string `json:"job_path" jsonschema:"Full slash-separated job path"`
	BuildNumber int    `json:"build_number,omitempty" jsonschema:"Build number; omit or 0 for the latest build"`
	FailedOnly  *bool  `json:"failed_only,omitempty" jsonschema:"Return only failing cases (default true)"`
	MaxCases    int    `json:"max_cases,omitempty" jsonschema:"Maximum test cases to return (default 50)"`
}

type metadataIn struct {
	JobPath string `json:"job_path" jsonschema:"Full slash-separated job path"`
	Depth   int    `json:"depth,omitempty" jsonschema:"How deep to walk the Jenkins object graph (default 3)"`
}

type changesIn struct {
	JobPath      string `json:"job_path" jsonschema:"Full slash-separated job path"`
	BuildNumber  int    `json:"build_number,omitempty" jsonschema:"Build number; omit or 0 for the latest build"`
	IncludePaths bool   `json:"include_paths,omitempty" jsonschema:"Include each commit's affected file paths (default false)"`
}

type findCommitIn struct {
	JobPath   string `json:"job_path" jsonschema:"Full slash-separated job path"`
	Commit    string `json:"commit" jsonschema:"Commit SHA or unique prefix to search for"`
	MaxBuilds int    `json:"max_builds,omitempty" jsonschema:"How many recent builds to scan (default 25, max 50)"`
}

type getLogsIn struct {
	JobPath     string `json:"job_path" jsonschema:"Full slash-separated job path"`
	BuildNumber int    `json:"build_number,omitempty" jsonschema:"Build number; omit or 0 for the latest build"`
	Stage       string `json:"stage,omitempty" jsonschema:"Restrict the log to this pipeline stage (case-insensitive) instead of the whole build"`
	MaxBytes    int    `json:"max_bytes,omitempty" jsonschema:"If set (max 16384), also return this many bytes of the log inline from offset_bytes; otherwise only the file path is returned"`
	OffsetBytes int    `json:"offset_bytes,omitempty" jsonschema:"Byte offset for the inline window (used with max_bytes)"`
}

type symbolsIn struct {
	JobPath     string `json:"job_path" jsonschema:"Full slash-separated job path"`
	BuildNumber int    `json:"build_number,omitempty" jsonschema:"Build number whose resolved shared-library symbols to use; omit or 0 for the latest build"`
	Query       string `json:"query,omitempty" jsonschema:"Case-insensitive substring to filter symbol names by"`
	Kind        string `json:"kind,omitempty" jsonschema:"Restrict to one symbol kind: step, global, or keyword"`
	Name        string `json:"name,omitempty" jsonschema:"Exact symbol name to return full detail for (signature, params, doc, members)"`
}

type lintIn struct {
	Script string `json:"script" jsonschema:"The Jenkinsfile (declarative or scripted Groovy) to validate"`
}

type getArtifactIn struct {
	JobPath     string `json:"job_path" jsonschema:"Full slash-separated job path"`
	BuildNumber int    `json:"build_number,omitempty" jsonschema:"Build number; omit or 0 for the latest build"`
	Name        string `json:"name" jsonschema:"Artifact to download, as listed by list_artifacts (display path or bare file name)"`
	MaxBytes    int    `json:"max_bytes,omitempty" jsonschema:"If set (max 16384), also return this many bytes inline from offset_bytes; otherwise only the file path is returned"`
	OffsetBytes int    `json:"offset_bytes,omitempty" jsonschema:"Byte offset for the inline window (used with max_bytes)"`
}

type getArtifactOut struct {
	BuildNumber int    `json:"build_number"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	SizeBytes   int64  `json:"size_bytes"`
	ContentType string `json:"content_type,omitempty"`
	Window      string `json:"window,omitempty"`
}

type getScanLogIn struct {
	JobPath     string `json:"job_path" jsonschema:"Full path of the multibranch project or folder to read the scan log of"`
	MaxBytes    int    `json:"max_bytes,omitempty" jsonschema:"Inline window size in bytes (max 16384); omit to get only the file path"`
	OffsetBytes int    `json:"offset_bytes,omitempty" jsonschema:"Byte offset the inline window starts at"`
}

type queueIn struct {
	Kind string `json:"kind,omitempty" jsonschema:"Which queue items to return: build (default), scan for branch-indexing tasks, or all"`
}

type queueHistoryIn struct {
	WindowMinutes int `json:"window_minutes,omitempty" jsonschema:"How far back to summarize, in minutes (default 120)"`
}

// ---- Mutating tool inputs ----

type triggerIn struct {
	JobPath         string            `json:"job_path" jsonschema:"Full slash-separated job path"`
	Params          map[string]string `json:"params,omitempty" jsonschema:"Build parameters as name=value pairs"`
	Wait            bool              `json:"wait,omitempty" jsonschema:"Block until the build leaves the queue and finishes (default false)"`
	WaitTimeoutSecs int               `json:"wait_timeout_seconds,omitempty" jsonschema:"Max seconds to wait when wait is set (default 300, max 600)"`
}

type replayIn struct {
	JobPath     string `json:"job_path" jsonschema:"Full slash-separated job path"`
	BuildNumber int    `json:"build_number,omitempty" jsonschema:"Build number whose pipeline to replay; omit or 0 for the latest build"`
	Script      string `json:"script" jsonschema:"The replacement Jenkinsfile (Groovy) to run"`
}

type cancelIn struct {
	JobPath     string `json:"job_path" jsonschema:"Full slash-separated job path"`
	BuildNumber int    `json:"build_number" jsonschema:"Build number to cancel (required; there is no latest default on this destructive action)"`
}

type dequeueIn struct {
	QueueID int64 `json:"queue_id" jsonschema:"Queue id of the waiting build (from list_queue)"`
}

type approveInputIn struct {
	JobPath     string            `json:"job_path" jsonschema:"Full slash-separated job path"`
	BuildNumber int               `json:"build_number,omitempty" jsonschema:"Build number; omit or 0 for the latest build"`
	InputID     string            `json:"input_id,omitempty" jsonschema:"Input step id; omit when exactly one input is pending"`
	Params      map[string]string `json:"params,omitempty" jsonschema:"Input parameters as name=value pairs"`
}

type rejectInputIn struct {
	JobPath     string `json:"job_path" jsonschema:"Full slash-separated job path"`
	BuildNumber int    `json:"build_number,omitempty" jsonschema:"Build number; omit or 0 for the latest build"`
	InputID     string `json:"input_id,omitempty" jsonschema:"Input step id; omit when exactly one input is pending"`
}

type nodeOfflineIn struct {
	Name   string `json:"name" jsonschema:"Node name; the controller is \"(built-in)\""`
	Reason string `json:"reason,omitempty" jsonschema:"Offline reason shown in Jenkins"`
}

type nodeOnlineIn struct {
	Name string `json:"name" jsonschema:"Node name; the controller is \"(built-in)\""`
}

// ---- Tool outputs (wrapped so each has an object schema) ----

type jobsOut struct {
	Jobs []dto.Job `json:"jobs"`
}

type viewsOut struct {
	Views []dto.View `json:"views"`
}

type buildsOut struct {
	Builds []dto.Build `json:"builds"`
}

type stagesOut struct {
	BuildNumber int         `json:"build_number"`
	Stages      []dto.Stage `json:"stages"`
}

type testReportOut struct {
	BuildNumber int            `json:"build_number"`
	Report      dto.TestReport `json:"report"`
}

type artifactsOut struct {
	BuildNumber int            `json:"build_number"`
	Artifacts   []dto.Artifact `json:"artifacts"`
}

type paramsOut struct {
	Params []dto.Param `json:"params"`
}

type metadataOut struct {
	Entries []dto.Meta `json:"entries"`
}

type queueOut struct {
	Items []dto.QueueItem `json:"items"`
}

type nodesOut struct {
	Nodes []dto.Node `json:"nodes"`
}

type runningOut struct {
	Builds []dto.UserBuild `json:"builds"`
	Count  int             `json:"count"`
}

type resolveJobOut struct {
	Matches []dto.JobMatch `json:"matches"`
	Count   int            `json:"count"`
}

type inputsOut struct {
	BuildNumber int         `json:"build_number"`
	Inputs      []dto.Input `json:"inputs"`
}

type changesOut struct {
	BuildNumber int          `json:"build_number"`
	Changes     []dto.Change `json:"changes"`
}

type findCommitOut struct {
	Hits           []dto.CommitHit `json:"hits"`
	SearchedBuilds int             `json:"searched_builds"`
}

type getLogsOut struct {
	BuildNumber int    `json:"build_number"`
	Path        string `json:"path"`
	SizeBytes   int64  `json:"size_bytes"`
	Complete    bool   `json:"complete"`
	Window      string `json:"window,omitempty"`
}

type symbolsOut struct {
	BuildNumber int                  `json:"build_number"`
	Steps       []dto.PipelineStep   `json:"steps,omitempty"`
	Globals     []dto.PipelineGlobal `json:"globals,omitempty"`
	Keywords    []string             `json:"keywords,omitempty"`
}

type lintOut struct {
	OK     bool            `json:"ok"`
	Issues []dto.LintIssue `json:"issues,omitempty"`
}

type scriptOut struct {
	BuildNumber int    `json:"build_number"`
	Script      string `json:"script"`
}

// ---- Mutating tool outputs ----

type triggerOut struct {
	JobPath     string `json:"job_path"`
	QueueID     int64  `json:"queue_id"`
	BuildNumber int    `json:"build_number,omitempty"`
	Status      string `json:"status,omitempty"`
}

type inputResultOut struct {
	BuildNumber int    `json:"build_number"`
	InputID     string `json:"input_id"`
	Action      string `json:"action"`
}

// actionOut is the generic acknowledgement for a fire-and-forget mutation.
type actionOut struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type waitForLogMatchIn struct {
	JobPath        string `json:"job_path" jsonschema:"Full slash-separated job path, e.g. TeamA/service/main"`
	Pattern        string `json:"pattern" jsonschema:"RE2 regular expression to wait for; prefix (?i) for a case-insensitive match"`
	BuildNumber    int    `json:"build_number,omitempty" jsonschema:"Build number; omit or 0 for the latest build. Ignored when source is scan"`
	Stage          string `json:"stage,omitempty" jsonschema:"Follow only this pipeline stage (case-insensitive) instead of the whole build log"`
	Source         string `json:"source,omitempty" jsonschema:"Which log to follow: build (default) for a build's console, or scan for a multibranch/folder repository scan log"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Max seconds this call blocks, honored as-is; omit or 0 for a 10-minute default"`
}

type waitForLogMatchOut struct {
	JobPath            string `json:"job_path"`
	BuildNumber        int    `json:"build_number,omitempty" jsonschema:"The build followed; absent when following a scan log"`
	Matched            bool   `json:"matched" jsonschema:"true when the pattern appeared in the log"`
	Match              string `json:"match,omitempty" jsonschema:"The matched text"`
	Line               string `json:"line,omitempty" jsonschema:"The whole log line the match occurred on"`
	LineNumber         int    `json:"line_number,omitempty" jsonschema:"1-based line number of the match"`
	OffsetBytes        int    `json:"offset_bytes,omitempty" jsonschema:"Byte offset of the match in the log file"`
	Complete           bool   `json:"complete" jsonschema:"true when the log has finished being written; with matched=false it means the pattern will never appear"`
	TimedOut           bool   `json:"timed_out,omitempty" jsonschema:"true when the wait bound elapsed with the log still live and no match"`
	ElapsedSeconds     int    `json:"elapsed_seconds"`
	CheckBackInSeconds int    `json:"check_back_in_seconds,omitempty" jsonschema:"When timed_out, the suggested delay before calling again"`
	Path               string `json:"path" jsonschema:"File the followed log was written to; grep it with your own shell"`
	SizeBytes          int64  `json:"size_bytes"`
}
