package view

import "testing"

func TestReproMyViewsURL(t *testing.T) {
	const base = "https://jenkins.cumuli.be"
	for _, raw := range []string{
		"https://jenkins.cumuli.be/job/Code/job/git%252Frie-iepr%252Fmjv/job/main/multi-pipeline-graph/",
		"https://jenkins.cumuli.be/user/ef503c71fd2d226f145a2892394e97d586dfc60b18be9383f46f4cbbfe064a99/my-views/view/Our%20projects/job/Code/job/git%252Frie-iepr%252Fmjv/job/main/209/stages/?selected-node=31",
	} {
		dl, err := ParseJenkinsURL(base, raw, nil)
		if err != nil {
			t.Logf("URL %s\n  -> ERROR: %v", raw, err)
			continue
		}
		t.Logf("URL %s\n  -> kind=%d label=%q folder=%q project=%q branch=%q build=%v",
			raw, dl.Kind, dl.Label, dl.NC.FolderPath, dl.NC.ProjectName, dl.NC.BranchName, dl.NC.Build)
	}
}
