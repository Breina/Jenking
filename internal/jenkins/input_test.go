package jenkins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

func loadBuildDetailFixture(t *testing.T, name string) jmodel.BuildDetail {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "input", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var raw jsonBuildDetail
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", name, err)
	}
	return raw.toDomain()
}

func TestBuildDetailParsesSimpleInput(t *testing.T) {
	bd := loadBuildDetailFixture(t, "simple.json")
	if len(bd.PendingInputs) != 1 {
		t.Fatalf("PendingInputs len = %d, want 1", len(bd.PendingInputs))
	}
	pi := bd.PendingInputs[0]
	if pi.ID != "B4d5ddd8efc5dfa00224256770965bf1" {
		t.Errorf("ID = %q", pi.ID)
	}
	if pi.Message != "Proceed to publish?" {
		t.Errorf("Message = %q", pi.Message)
	}
	if pi.OkLabel != "Proceed" || pi.AbortLabel != "Abort" {
		t.Errorf("labels = %q/%q", pi.OkLabel, pi.AbortLabel)
	}
	if len(pi.Parameters) != 0 {
		t.Errorf("Parameters len = %d, want 0", len(pi.Parameters))
	}
	if pi.Submitter != "" {
		t.Errorf("Submitter = %q, want empty", pi.Submitter)
	}
}

func TestBuildDetailParsesParameterizedInput(t *testing.T) {
	bd := loadBuildDetailFixture(t, "parameterized.json")
	if len(bd.PendingInputs) != 1 {
		t.Fatalf("PendingInputs len = %d, want 1", len(bd.PendingInputs))
	}
	pi := bd.PendingInputs[0]
	if pi.OkLabel != "Deploy" {
		t.Errorf("OkLabel = %q, want Deploy", pi.OkLabel)
	}
	if pi.SubmitterParameter != "APPROVER" {
		t.Errorf("SubmitterParameter = %q", pi.SubmitterParameter)
	}
	if len(pi.Parameters) != 4 {
		t.Fatalf("Parameters len = %d, want 4", len(pi.Parameters))
	}
	want := []struct {
		name      string
		paramType jmodel.ParamType
		def       string
	}{
		{"TARGET", jmodel.ParamTypeString, "staging"},
		{"DRY_RUN", jmodel.ParamTypeBool, "true"},
		{"TIER", jmodel.ParamTypeChoice, "low"},
		{"TOKEN", jmodel.ParamTypePassword, ""},
	}
	for i, w := range want {
		got := pi.Parameters[i]
		if got.Name != w.name || got.Type != w.paramType || got.Default != w.def {
			t.Errorf("param[%d] = {%q,%q,%q}, want {%q,%q,%q}",
				i, got.Name, got.Type, got.Default, w.name, w.paramType, w.def)
		}
	}
	if got := pi.Parameters[2].Choices; len(got) != 3 || got[1] != "medium" {
		t.Errorf("TIER choices = %v", got)
	}
}

func TestBuildDetailParsesSubmitterInput(t *testing.T) {
	bd := loadBuildDetailFixture(t, "submitter.json")
	if len(bd.PendingInputs) != 1 {
		t.Fatalf("PendingInputs len = %d", len(bd.PendingInputs))
	}
	if got := bd.PendingInputs[0].Submitter; got != "admin,release-managers" {
		t.Errorf("Submitter = %q", got)
	}
}

func TestBuildDetailSkipsSettledInputs(t *testing.T) {
	bd := loadBuildDetailFixture(t, "settled.json")
	if len(bd.PendingInputs) != 0 {
		t.Errorf("PendingInputs = %v, want empty (input was settled)", bd.PendingInputs)
	}
}

func TestApplyPendingInputs(t *testing.T) {
	stages := []jmodel.Stage{
		{Name: "Build", Status: jmodel.BuildStatusSuccess},
		{Name: "Approve", Status: jmodel.BuildStatusRunning},
		{Name: "Deploy", Status: jmodel.BuildStatusUnknown},
	}
	inputs := []jmodel.PendingInput{{ID: "x", Message: "Approve?"}}
	got := jmodel.ApplyPendingInputs(stages, inputs)
	if got[0].Status != jmodel.BuildStatusSuccess {
		t.Errorf("[0] status mutated: %q", got[0].Status)
	}
	if got[1].Status != jmodel.BuildStatusPausedInput {
		t.Errorf("[1] status = %q, want paused_input", got[1].Status)
	}
	if got[2].Status != jmodel.BuildStatusUnknown {
		t.Errorf("[2] status mutated: %q", got[2].Status)
	}
}

func TestApplyPendingInputsNoInputs(t *testing.T) {
	stages := []jmodel.Stage{{Status: jmodel.BuildStatusRunning}}
	got := jmodel.ApplyPendingInputs(stages, nil)
	if got[0].Status != jmodel.BuildStatusRunning {
		t.Errorf("status mutated without inputs: %q", got[0].Status)
	}
}
