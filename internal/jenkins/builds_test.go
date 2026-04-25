package jenkins

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"
)

func TestJsonBuildToDomain_TriggeredBy(t *testing.T) {
	t.Run("extracts userId from first cause", func(t *testing.T) {
		raw := `{
			"number": 42,
			"result": "SUCCESS",
			"building": false,
			"duration": 5000,
			"estimatedDuration": 6000,
			"timestamp": 0,
			"_class": "org.jenkinsci.plugins.workflow.job.WorkflowRun",
			"actions": [
				{"_class": "hudson.model.CauseAction", "causes": [{"userId": "brecht", "userName": "Brecht", "shortDescription": "Started by user Brecht"}]}
			]
		}`
		var b jsonBuild
		if err := json.Unmarshal([]byte(raw), &b); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		domain := b.toDomain()
		if domain.TriggeredBy != "brecht" {
			t.Errorf("TriggeredBy = %q, want %q", domain.TriggeredBy, "brecht")
		}
	})

	t.Run("empty when no causes", func(t *testing.T) {
		raw := `{
			"number": 1,
			"result": "SUCCESS",
			"building": false,
			"duration": 1000,
			"estimatedDuration": 1000,
			"timestamp": 0,
			"_class": "org.jenkinsci.plugins.workflow.job.WorkflowRun",
			"actions": []
		}`
		var b jsonBuild
		if err := json.Unmarshal([]byte(raw), &b); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		domain := b.toDomain()
		if domain.TriggeredBy != "" {
			t.Errorf("TriggeredBy = %q, want empty", domain.TriggeredBy)
		}
	})

	t.Run("skips causes with empty userId", func(t *testing.T) {
		raw := `{
			"number": 5,
			"result": "SUCCESS",
			"building": false,
			"duration": 1000,
			"estimatedDuration": 1000,
			"timestamp": 0,
			"_class": "org.jenkinsci.plugins.workflow.job.WorkflowRun",
			"actions": [
				{"_class": "hudson.model.CauseAction", "causes": [{"userId": "", "shortDescription": "Started by timer"}]}
			]
		}`
		var b jsonBuild
		if err := json.Unmarshal([]byte(raw), &b); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		domain := b.toDomain()
		if domain.TriggeredBy != "" {
			t.Errorf("TriggeredBy = %q, want empty for timer cause", domain.TriggeredBy)
		}
	})
}

// TestJsonBuildDetailToDomain_NonStringParameters is the regression test for
// the GetBuild-silently-failing bug. Jenkins serialises parameter values
// using the parameter's native JSON type (BooleanParameterValue → bool,
// StringParameterValue → string, etc.). Decoding the entire build response
// failed when ANY parameter was non-string, leaving every downstream view
// without a build status. Fixed by typing jsonParameter.Value as
// json.RawMessage and converting per-value in stringValue().
func TestJsonBuildDetailToDomain_NonStringParameters(t *testing.T) {
	raw := `{
		"number": 9,
		"result": "SUCCESS",
		"building": false,
		"duration": 12000,
		"estimatedDuration": 15000,
		"timestamp": 0,
		"_class": "org.jenkinsci.plugins.workflow.job.WorkflowRun",
		"actions": [
			{"_class": "hudson.model.ParametersAction", "parameters": [
				{"_class": "hudson.model.BooleanParameterValue", "name": "FAIL", "value": false},
				{"_class": "hudson.model.StringParameterValue", "name": "MESSAGE", "value": "hello from jenking-e2e"},
				{"_class": "hudson.model.StringParameterValue", "name": "SPEED", "value": "fast"}
			]}
		]
	}`
	var bd jsonBuildDetail
	if err := json.Unmarshal([]byte(raw), &bd); err != nil {
		t.Fatalf("decode failed (the bug): %v", err)
	}
	d := bd.toDomain()
	if d.Status != BuildStatusSuccess {
		t.Errorf("Status = %v, want Success — parameter parsing must not block status decode", d.Status)
	}
	if d.Params["FAIL"] != "false" {
		t.Errorf("Params[FAIL] = %q, want %q (bool→string conversion)", d.Params["FAIL"], "false")
	}
	if d.Params["MESSAGE"] != "hello from jenking-e2e" {
		t.Errorf("Params[MESSAGE] = %q, want plain string (no surrounding quotes)", d.Params["MESSAGE"])
	}
	if d.Params["SPEED"] != "fast" {
		t.Errorf("Params[SPEED] = %q, want %q", d.Params["SPEED"], "fast")
	}
}

// TestJsonBuildDetailToDomain_ParameterTypes covers the full matrix of
// JSON value shapes Jenkins emits for ParametersAction.
func TestJsonBuildDetailToDomain_ParameterTypes(t *testing.T) {
	raw := `{
		"number": 1, "result": "SUCCESS", "building": false,
		"duration": 0, "estimatedDuration": 0, "timestamp": 0,
		"actions": [{"_class": "hudson.model.ParametersAction", "parameters": [
			{"name": "S", "value": "text"},
			{"name": "B_TRUE",  "value": true},
			{"name": "B_FALSE", "value": false},
			{"name": "N_INT",   "value": 42},
			{"name": "N_FLOAT", "value": 3.14},
			{"name": "NIL",     "value": null},
			{"name": "EMPTY",   "value": ""}
		]}]
	}`
	var bd jsonBuildDetail
	if err := json.Unmarshal([]byte(raw), &bd); err != nil {
		t.Fatalf("decode: %v", err)
	}
	d := bd.toDomain()
	want := map[string]string{
		"S": "text", "B_TRUE": "true", "B_FALSE": "false",
		"N_INT": "42", "N_FLOAT": "3.14",
		// JSON null unmarshals into a Go string as the empty string —
		// fine for our display use case (no separate "null" sentinel).
		"NIL": "", "EMPTY": "",
	}
	for k, v := range want {
		if got := d.Params[k]; got != v {
			t.Errorf("Params[%q] = %q, want %q", k, got, v)
		}
	}
}

func TestParseFlowGraphTable_FailurePropagation(t *testing.T) {
	// Stage "Build Maven" has a child "sh" that failed.
	// All stages at same depth (padding * 9).
	html := `<table>
<tr><td style="padding-left: calc(var(--p) * 9)"><a tooltip="ID: 10">stage - (5.1 sec in block)</a></td><td>Checkout SCM</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 10)"><a tooltip="ID: 11">stage block (Checkout SCM) - (5 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 11)"><a tooltip="ID: 12">checkout - (5 sec in self)</a></td><td></td><td><a href="/node/12/log/">log</a></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 9)"><a tooltip="ID: 25">stage - (20 sec in block)</a></td><td>Build Maven</td><td><a href="/node/25/log/">log</a></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 10)"><a tooltip="ID: 26">stage block (Build Maven) - (19 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 11)"><a tooltip="ID: 32">echo - (42 ms in self)</a></td><td>hello</td><td><a href="/node/32/log/">log</a></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 11)"><a tooltip="ID: 33">sh - (18 sec in self)</a></td><td>mvn package</td><td><a href="/node/33/log/">log</a></td><td>Failed</td></tr>
<tr><td style="padding-left: calc(var(--p) * 9)"><a tooltip="ID: 41">stage - (1.2 sec in block)</a></td><td>Quality</td><td><a href="/node/41/log/">log</a></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 10)"><a tooltip="ID: 42">stage block (Quality) - (1 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
</table>`

	stages, err := parseFlowGraphTable(html)
	if err != nil {
		t.Fatalf("parseFlowGraphTable() error: %v", err)
	}
	if len(stages) != 3 {
		t.Fatalf("expected 3 stages, got %d", len(stages))
	}

	if stages[0].Name != "Checkout SCM" {
		t.Errorf("stage[0].Name = %q, want Checkout SCM", stages[0].Name)
	}
	if stages[0].Status != BuildStatusSuccess {
		t.Errorf("stage[0].Status = %q, want success", stages[0].Status)
	}
	if stages[0].Depth != 0 {
		t.Errorf("stage[0].Depth = %d, want 0", stages[0].Depth)
	}

	if stages[1].Name != "Build Maven" {
		t.Errorf("stage[1].Name = %q, want Build Maven", stages[1].Name)
	}
	if stages[1].Status != BuildStatusFailed {
		t.Errorf("stage[1].Status = %q, want failed (child sh failed)", stages[1].Status)
	}
	if len(stages[1].NodeIDs) != 3 {
		t.Errorf("stage[1].NodeIDs = %v, want 3 entries", stages[1].NodeIDs)
	}

	// Quality remains SUCCESS from the AJAX table — skip detection is now
	// log-based (earlier-failure skips are parsed from console output).
	if stages[2].Status != BuildStatusSuccess {
		t.Errorf("stage[2].Status = %q, want success (skip detection is log-based)", stages[2].Status)
	}
}

func TestParseFlowGraphTable_NestingAndParallel(t *testing.T) {
	// Simulates: Quality & Safety > parallel > Docker > Build Docker, Trivy scan
	html := `<table>
<tr><td style="padding-left: calc(var(--p) * 9)"><a tooltip="ID: 41">stage - (1.2 sec in block)</a></td><td>Quality &amp; Safety</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 10)"><a tooltip="ID: 42">stage block (Quality &amp; Safety) - (1 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 11)"><a tooltip="ID: 44">parallel - (0.95 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 12)"><a tooltip="ID: 46">parallel block (Branch: Docker) - (9 ms in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 13)"><a tooltip="ID: 48">stage - (0.74 sec in block)</a></td><td>Docker</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 14)"><a tooltip="ID: 49">stage block (Docker) - (0.67 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 15)"><a tooltip="ID: 57">stage - (0.23 sec in block)</a></td><td>Build Docker</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 16)"><a tooltip="ID: 58">stage block (Build Docker) - (77 ms in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 15)"><a tooltip="ID: 62">stage - (0.15 sec in block)</a></td><td>Trivy scan</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 16)"><a tooltip="ID: 63">stage block (Trivy scan) - (80 ms in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 12)"><a tooltip="ID: 47">parallel block (Branch: Sonar scan) - (0.27 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 13)"><a tooltip="ID: 50">stage - (0.17 sec in block)</a></td><td>Sonar scan</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 14)"><a tooltip="ID: 51">stage block (Sonar scan) - (0.11 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 9)"><a tooltip="ID: 73">stage - (0.19 sec in block)</a></td><td>Push Docker image</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 10)"><a tooltip="ID: 74">stage block (Push Docker image) - (73 ms in block)</a></td><td></td><td></td><td>Success</td></tr>
</table>`

	stages, err := parseFlowGraphTable(html)
	if err != nil {
		t.Fatalf("parseFlowGraphTable() error: %v", err)
	}

	// Expected stages: Quality & Safety, Docker, Build Docker, Trivy scan, Sonar scan, Push Docker image
	if len(stages) != 6 {
		for i, s := range stages {
			t.Logf("  stage[%d]: %q depth=%d parallel=%v", i, s.Name, s.Depth, s.Parallel)
		}
		t.Fatalf("expected 6 stages, got %d", len(stages))
	}

	tests := []struct {
		name     string
		depth    int
		parallel bool
	}{
		{"Quality & Safety", 0, true},
		{"Docker", 1, false},
		{"Build Docker", 2, false},
		{"Trivy scan", 2, false},
		{"Sonar scan", 1, false},
		{"Push Docker image", 0, false},
	}
	for i, tt := range tests {
		if stages[i].Name != tt.name {
			t.Errorf("stage[%d].Name = %q, want %q", i, stages[i].Name, tt.name)
		}
		if stages[i].Depth != tt.depth {
			t.Errorf("stage[%d].Depth = %d, want %d (name=%q)", i, stages[i].Depth, tt.depth, tt.name)
		}
		if stages[i].Parallel != tt.parallel {
			t.Errorf("stage[%d].Parallel = %v, want %v (name=%q)", i, stages[i].Parallel, tt.parallel, tt.name)
		}
	}
}

func TestParseFlowGraphTable_Duration(t *testing.T) {
	html := `<table>
<tr><td style="padding-left: calc(var(--p) * 9)"><a tooltip="ID: 10">stage - (4 min 13 sec in block)</a></td><td>Build</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 10)"><a tooltip="ID: 11">stage block (Build) - (4 min 12 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
</table>`

	stages, err := parseFlowGraphTable(html)
	if err != nil {
		t.Fatalf("parseFlowGraphTable() error: %v", err)
	}
	if len(stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(stages))
	}
	want := 4*time.Minute + 13*time.Second
	if stages[0].Duration != want {
		t.Errorf("stage.Duration = %v, want %v", stages[0].Duration, want)
	}
}

func TestParseFlowGraphTable_Matrix(t *testing.T) {
	// Matrix pipeline: Build environments > parallel > Matrix branches > Render manifest
	html := `<table>
<tr><td style="padding-left: calc(var(--p) * 9)"><a tooltip="ID: 10">stage - (1.8 sec in block)</a></td><td>Main</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 10)"><a tooltip="ID: 11">stage block (Main) - (1.7 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 11)"><a tooltip="ID: 20">stage - (1.3 sec in block)</a></td><td>Build environments</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 12)"><a tooltip="ID: 21">stage block (Build environments) - (1.2 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 13)"><a tooltip="ID: 30">parallel - (1.1 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 14)"><a tooltip="ID: 31">parallel block (Branch: Matrix - ENVIRONMENT_DIR = 'on') - (12 ms in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 15)"><a tooltip="ID: 32">stage - (0.81 sec in block)</a></td><td>Matrix - ENVIRONMENT_DIR = 'on'</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 16)"><a tooltip="ID: 33">stage block (Matrix - ENVIRONMENT_DIR = 'on') - (0.72 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 17)"><a tooltip="ID: 34">stage - (0.33 sec in block)</a></td><td>Render manifest</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 18)"><a tooltip="ID: 35">stage block (Render manifest) - (0.16 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 14)"><a tooltip="ID: 36">parallel block (Branch: Matrix - ENVIRONMENT_DIR = 'oe') - (10 ms in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 15)"><a tooltip="ID: 37">stage - (0.81 sec in block)</a></td><td>Matrix - ENVIRONMENT_DIR = 'oe'</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 16)"><a tooltip="ID: 38">stage block (Matrix - ENVIRONMENT_DIR = 'oe') - (0.69 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 17)"><a tooltip="ID: 39">stage - (0.33 sec in block)</a></td><td>Render manifest</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 18)"><a tooltip="ID: 40">stage block (Render manifest) - (0.17 sec in block)</a></td><td></td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 9)"><a tooltip="ID: 50">stage - (0.13 sec in block)</a></td><td>Publish changes</td><td></td><td>Success</td></tr>
<tr><td style="padding-left: calc(var(--p) * 10)"><a tooltip="ID: 51">stage block (Publish changes) - (68 ms in block)</a></td><td></td><td></td><td>Success</td></tr>
</table>`

	stages, err := parseFlowGraphTable(html)
	if err != nil {
		t.Fatalf("parseFlowGraphTable() error: %v", err)
	}

	// Expected: Main, Build environments (parallel), Matrix on, Render manifest,
	//           Matrix oe, Render manifest, Publish changes
	tests := []struct {
		name     string
		depth    int
		parallel bool
	}{
		{"Main", 0, false},
		{"Build environments", 1, true},
		{"Matrix - ENVIRONMENT_DIR = 'on'", 2, false},
		{"Render manifest", 3, false},
		{"Matrix - ENVIRONMENT_DIR = 'oe'", 2, false},
		{"Render manifest", 3, false},
		{"Publish changes", 0, false},
	}

	if len(stages) != len(tests) {
		for i, s := range stages {
			t.Logf("  stage[%d]: %q depth=%d parallel=%v", i, s.Name, s.Depth, s.Parallel)
		}
		t.Fatalf("expected %d stages, got %d", len(tests), len(stages))
	}

	for i, tt := range tests {
		if stages[i].Name != tt.name {
			t.Errorf("stage[%d].Name = %q, want %q", i, stages[i].Name, tt.name)
		}
		if stages[i].Depth != tt.depth {
			t.Errorf("stage[%d].Depth = %d, want %d (name=%q)", i, stages[i].Depth, tt.depth, tt.name)
		}
		if stages[i].Parallel != tt.parallel {
			t.Errorf("stage[%d].Parallel = %v, want %v (name=%q)", i, stages[i].Parallel, tt.parallel, tt.name)
		}
	}
}

func TestParseDurationText(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"4 min 13 sec", 4*time.Minute + 13*time.Second},
		{"6.4 sec", 6400 * time.Millisecond},
		{"39 ms", 39 * time.Millisecond},
		{"1 hr 2 min 3 sec", time.Hour + 2*time.Minute + 3*time.Second},
		{"0.14 sec", 140 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseDurationText(tt.input)
			if got != tt.want {
				t.Errorf("parseDurationText(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseSkippedStages_LeafSkip(t *testing.T) {
	log := `[Pipeline] { (Maven deploy)
[Pipeline] stage
Stage "Maven deploy" skipped due to when conditional`

	result := ParseSkippedStages(log)
	occs := result["Maven deploy"]
	if len(occs) != 1 || !occs[0] {
		t.Errorf("expected Maven deploy occurrence 0 = true, got %v", occs)
	}
}

func TestParseSkippedStages_ParentSkipWithChildren(t *testing.T) {
	// When parent is skipped, children show the parent's name in the skip message.
	// The current stage (from { (Name) lines) is what matters.
	log := `[Pipeline] { (Primary branch)
Stage "Primary branch" skipped due to when conditional
[Pipeline] { (Validate tag)
Stage "Primary branch" skipped due to when conditional
[Pipeline] { (Sonar scan)
Stage "Primary branch" skipped due to when conditional
[Pipeline] { (Maven release)
Stage "Primary branch" skipped due to when conditional`

	result := ParseSkippedStages(log)
	for _, name := range []string{"Primary branch", "Validate tag", "Sonar scan", "Maven release"} {
		occs := result[name]
		if len(occs) != 1 || !occs[0] {
			t.Errorf("expected %q occurrence 0 = true, got %v", name, occs)
		}
	}
}

// TestParseSkippedStages_OccurrenceDifferentiation verifies that when the same
// stage name appears in two parallel branches (one skipped, one not), only the
// correct occurrence is marked.
func TestParseSkippedStages_OccurrenceDifferentiation(t *testing.T) {
	// Primary branch runs; Non-primary branch is skipped.
	// "Maven deploy" appears under both — first occurrence runs, second is skipped.
	log := `[Pipeline] { (Primary branch)
[Pipeline] { (Maven deploy)
[Pipeline] { (Non-primary branch)
Stage "Non-primary branch" skipped due to when conditional
[Pipeline] { (Maven deploy)
Stage "Non-primary branch" skipped due to when conditional`

	result := ParseSkippedStages(log)
	occs := result["Maven deploy"]
	if len(occs) != 2 {
		t.Fatalf("expected 2 Maven deploy occurrences, got %d", len(occs))
	}
	if occs[0] {
		t.Error("first Maven deploy (under Primary branch) should NOT be skipped")
	}
	if !occs[1] {
		t.Error("second Maven deploy (under Non-primary branch) should be skipped")
	}
}

func TestMarkSkipped_OnlyDowngradesSuccess(t *testing.T) {
	stages := []Stage{
		{Name: "Build", Status: BuildStatusSuccess},
		{Name: "Test", Status: BuildStatusFailed},
		{Name: "Deploy", Status: BuildStatusSuccess, NodeIDs: []int{42}},
	}
	skippedOccs := map[string][]bool{"Build": {true}, "Test": {true}, "Deploy": {true}}
	MarkSkipped(stages, skippedOccs)

	if stages[0].Status != BuildStatusSkipped {
		t.Errorf("Build: expected SKIPPED, got %v", stages[0].Status)
	}
	if stages[1].Status != BuildStatusFailed {
		t.Errorf("Test: expected FAILED (unchanged), got %v", stages[1].Status)
	}
	if stages[2].Status != BuildStatusSkipped {
		t.Errorf("Deploy: expected SKIPPED, got %v", stages[2].Status)
	}
	if len(stages[2].NodeIDs) != 0 {
		t.Errorf("Deploy: expected NodeIDs cleared, got %v", stages[2].NodeIDs)
	}
}

// TestMarkSkipped_OccurrenceAware verifies that duplicate stage names in
// parallel branches are matched by occurrence order, not just name.
func TestMarkSkipped_OccurrenceAware(t *testing.T) {
	stages := []Stage{
		{Name: "Maven deploy", Status: BuildStatusSuccess, NodeIDs: []int{1}},
		{Name: "Maven deploy", Status: BuildStatusSuccess, NodeIDs: []int{2}},
	}
	skippedOccs := map[string][]bool{"Maven deploy": {false, true}}
	MarkSkipped(stages, skippedOccs)

	if stages[0].Status != BuildStatusSuccess {
		t.Errorf("first Maven deploy should remain SUCCESS, got %v", stages[0].Status)
	}
	if stages[1].Status != BuildStatusSkipped {
		t.Errorf("second Maven deploy should be SKIPPED, got %v", stages[1].Status)
	}
	if len(stages[1].NodeIDs) != 0 {
		t.Errorf("second Maven deploy NodeIDs should be cleared, got %v", stages[1].NodeIDs)
	}
}

// TestParseSkippedStages_ProgressiveLogFormat tests that parsing handles
// Jenkins' ANSI hidden text annotations embedded in progressive log output.
func TestParseSkippedStages_ProgressiveLogFormat(t *testing.T) {
	// Jenkins embeds base64 metadata inside ANSI hidden blocks: \x1b[8m...data...\x1b[0m
	log := "\x1b[8mha:////base64data=\x1b[0m[Pipeline] { (Non-primary branch)\n" +
		"Stage \"Non-primary branch\" skipped due to when conditional\n" +
		"\x1b[8mha:////morebase64=\x1b[0m[Pipeline] { (Maven deploy)\n" +
		"Stage \"Non-primary branch\" skipped due to when conditional\n"

	result := ParseSkippedStages(log)
	occs := result["Non-primary branch"]
	if len(occs) != 1 || !occs[0] {
		t.Errorf("expected Non-primary branch skipped, got %v", occs)
	}
	occs = result["Maven deploy"]
	if len(occs) != 1 || !occs[0] {
		t.Errorf("expected Maven deploy skipped, got %v", occs)
	}
}

func TestParseSkippedStages_EarlierFailure(t *testing.T) {
	log := `[Pipeline] { (Build Maven)
[Pipeline] sh
[Pipeline] { (Sonar scan)
Stage "Sonar scan" skipped due to earlier failure(s)
[Pipeline] { (Maven deploy)
Stage "Maven deploy" skipped due to earlier failure(s)`

	result := ParseSkippedStages(log)
	occs := result["Build Maven"]
	if len(occs) != 1 || occs[0] {
		t.Errorf("Build Maven should NOT be skipped, got %v", occs)
	}
	occs = result["Sonar scan"]
	if len(occs) != 1 || !occs[0] {
		t.Errorf("Sonar scan should be skipped, got %v", occs)
	}
	occs = result["Maven deploy"]
	if len(occs) != 1 || !occs[0] {
		t.Errorf("Maven deploy should be skipped, got %v", occs)
	}
}

// TestParseSkippedStages_ParallelChildrenBatchedSkips verifies that when
// parallel children's [Pipeline] { entries all appear before their skip lines,
// each child is correctly attributed (not just the last-entered one).
func TestParseSkippedStages_ParallelChildrenBatchedSkips(t *testing.T) {
	log := `[Pipeline] stage
[Pipeline] { (Non-primary branch)
Stage "Non-primary branch" skipped due to when conditional
[Pipeline] getContext
[Pipeline] parallel
[Pipeline] { (Branch: Trivy scan)
[Pipeline] { (Branch: Maven verify)
[Pipeline] { (Branch: Maven deploy)
[Pipeline] stage
[Pipeline] { (Trivy scan)
[Pipeline] stage
[Pipeline] { (Maven verify)
[Pipeline] stage
[Pipeline] { (Maven deploy)
Stage "Trivy scan" skipped due to when conditional
[Pipeline] getContext
[Pipeline] }
Stage "Maven verify" skipped due to when conditional
[Pipeline] getContext
[Pipeline] }
Stage "Maven deploy" skipped due to when conditional
[Pipeline] getContext
[Pipeline] }
[Pipeline] // stage
[Pipeline] // stage
[Pipeline] // stage
[Pipeline] }
[Pipeline] }
[Pipeline] }
[Pipeline] // parallel
[Pipeline] }
[Pipeline] // stage
[Pipeline] stage
[Pipeline] { (Primary branch)
[Pipeline] stage
[Pipeline] { (Validate tag)
[Pipeline] script
[Pipeline] {
[Pipeline] sh
+ git tag -l 0.1.99
[Pipeline] }
[Pipeline] // script
[Pipeline] }
[Pipeline] // stage
[Pipeline] stage
[Pipeline] { (Maven deploy)
[Pipeline] script`

	result := ParseSkippedStages(log)

	// All children under Non-primary branch should be skipped.
	for _, name := range []string{"Non-primary branch", "Trivy scan", "Maven verify"} {
		occs := result[name]
		if len(occs) != 1 || !occs[0] {
			t.Errorf("%q should be skipped, got %v", name, occs)
		}
	}

	// Maven deploy appears twice: first (under Non-primary) skipped, second (under Primary) not.
	occs := result["Maven deploy"]
	if len(occs) != 2 {
		t.Fatalf("expected 2 Maven deploy occurrences, got %d: %v", len(occs), occs)
	}
	if !occs[0] {
		t.Error("first Maven deploy (under Non-primary branch) should be skipped")
	}
	if occs[1] {
		t.Error("second Maven deploy (under Primary branch) should NOT be skipped")
	}

	// Stages under Primary branch should not be skipped.
	occs = result["Primary branch"]
	if len(occs) != 1 || occs[0] {
		t.Errorf("Primary branch should NOT be skipped, got %v", occs)
	}
	occs = result["Validate tag"]
	if len(occs) != 1 || occs[0] {
		t.Errorf("Validate tag should NOT be skipped, got %v", occs)
	}
}

func TestMarkSkippedEmptyMap(t *testing.T) {
	stages := []Stage{
		{Name: "Build", Status: BuildStatusSuccess},
	}
	MarkSkipped(stages, map[string][]bool{})
	if stages[0].Status != BuildStatusSuccess {
		t.Errorf("expected SUCCESS unchanged, got %v", stages[0].Status)
	}
}

func TestListProjectBuilds_ParseAndSort(t *testing.T) {
	// Fixture: two branch jobs, each with builds at different timestamps.
	// Builds should be returned sorted by Timestamp descending.
	olderTS := time.Now().Add(-2 * time.Hour).UnixMilli()
	newerTS := time.Now().Add(-1 * time.Hour).UnixMilli()
	latestTS := time.Now().UnixMilli()

	fixture := map[string]interface{}{
		"jobs": []interface{}{
			map[string]interface{}{
				"name":     "main",
				"fullName": "myproject/main",
				"builds": []interface{}{
					map[string]interface{}{
						"number":            1,
						"result":            "SUCCESS",
						"building":          false,
						"duration":          5000,
						"estimatedDuration": 6000,
						"timestamp":         olderTS,
						"actions":           []interface{}{},
					},
					map[string]interface{}{
						"number":            2,
						"result":            nil,
						"building":          true,
						"duration":          0,
						"estimatedDuration": 6000,
						"timestamp":         latestTS,
						"actions": []interface{}{
							map[string]interface{}{
								"_class": "hudson.model.CauseAction",
								"causes": []interface{}{map[string]interface{}{"userId": "alice"}},
							},
						},
					},
				},
			},
			map[string]interface{}{
				"name":     "feature-x",
				"fullName": "myproject/feature-x",
				"builds": []interface{}{
					map[string]interface{}{
						"number":            1,
						"result":            "FAILURE",
						"building":          false,
						"duration":          3000,
						"estimatedDuration": 4000,
						"timestamp":         newerTS,
						"actions":           []interface{}{},
					},
				},
			},
		},
	}

	fixtureJSON, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(fixtureJSON)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "", "", false)
	builds, err := client.ListProjectBuilds(t.Context(), "myproject")
	if err != nil {
		t.Fatalf("ListProjectBuilds() error: %v", err)
	}

	if len(builds) != 3 {
		t.Fatalf("expected 3 builds, got %d", len(builds))
	}

	// Verify sorted by Timestamp descending.
	if !sort.SliceIsSorted(builds, func(i, j int) bool {
		return builds[i].Timestamp.After(builds[j].Timestamp)
	}) {
		t.Error("builds are not sorted by Timestamp descending")
	}

	// First build should be the running one (latestTS, main branch #2).
	if builds[0].BranchName != "main" {
		t.Errorf("builds[0].BranchName = %q, want %q", builds[0].BranchName, "main")
	}
	if builds[0].Number != 2 {
		t.Errorf("builds[0].Number = %d, want 2", builds[0].Number)
	}
	if builds[0].Status != BuildStatusRunning {
		t.Errorf("builds[0].Status = %q, want running", builds[0].Status)
	}
	if builds[0].TriggeredBy != "alice" {
		t.Errorf("builds[0].TriggeredBy = %q, want alice", builds[0].TriggeredBy)
	}
	if builds[0].BranchPath != "myproject/main" {
		t.Errorf("builds[0].BranchPath = %q, want myproject/main", builds[0].BranchPath)
	}

	// Second should be feature-x #1 (newerTS).
	if builds[1].BranchName != "feature-x" {
		t.Errorf("builds[1].BranchName = %q, want feature-x", builds[1].BranchName)
	}
	if builds[1].Status != BuildStatusFailed {
		t.Errorf("builds[1].Status = %q, want failed", builds[1].Status)
	}

	// Third should be main #1 (olderTS).
	if builds[2].BranchName != "main" {
		t.Errorf("builds[2].BranchName = %q, want main", builds[2].BranchName)
	}
	if builds[2].Number != 1 {
		t.Errorf("builds[2].Number = %d, want 1", builds[2].Number)
	}
	if builds[2].Status != BuildStatusSuccess {
		t.Errorf("builds[2].Status = %q, want success", builds[2].Status)
	}
}
