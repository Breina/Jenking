package jenkins

import (
	"strings"
	"testing"
)

func TestParseValidateResponse_OK(t *testing.T) {
	got := parseValidateResponse("Jenkinsfile successfully validated.\n")
	if !got.OK {
		t.Errorf("expected OK=true, got %+v", got)
	}
	if len(got.Issues) != 0 {
		t.Errorf("expected no issues, got %+v", got.Issues)
	}
}

func TestParseValidateResponse_WithLineAndCol(t *testing.T) {
	resp := `Errors encountered validating Jenkinsfile:
WorkflowScript: 4: 12: Expected "}" but found "stage"
WorkflowScript: 7: Unknown step "shh"`
	got := parseValidateResponse(resp)
	if got.OK {
		t.Errorf("expected OK=false")
	}
	if len(got.Issues) != 2 {
		t.Fatalf("expected 2 issues, got %d: %+v", len(got.Issues), got.Issues)
	}
	if got.Issues[0].Line != 4 || got.Issues[0].Col != 12 {
		t.Errorf("first issue position wrong: %+v", got.Issues[0])
	}
	if !strings.Contains(got.Issues[0].Message, `Expected "}"`) {
		t.Errorf("first issue msg wrong: %q", got.Issues[0].Message)
	}
	if got.Issues[1].Line != 7 || got.Issues[1].Col != 0 {
		t.Errorf("second issue position wrong: %+v", got.Issues[1])
	}
}

func TestParseValidateResponse_SuffixLineColumn(t *testing.T) {
	resp := `Errors encountered validating Jenkinsfile:
Not a valid section definition: "sdfsdfsdfsdf". Some extra configuration is required. @ line 7, column 1.
Expected a stage @ line 12, column 5.`
	got := parseValidateResponse(resp)
	if got.OK {
		t.Errorf("expected OK=false")
	}
	if len(got.Issues) != 2 {
		t.Fatalf("expected 2 issues, got %d: %+v", len(got.Issues), got.Issues)
	}
	if got.Issues[0].Line != 7 || got.Issues[0].Col != 1 {
		t.Errorf("first suffix issue position wrong: %+v", got.Issues[0])
	}
	if !strings.Contains(got.Issues[0].Message, "Not a valid section definition") || strings.Contains(got.Issues[0].Message, "@ line") {
		t.Errorf("first suffix issue message not cleaned: %q", got.Issues[0].Message)
	}
	if got.Issues[1].Line != 12 || got.Issues[1].Col != 5 {
		t.Errorf("second suffix issue position wrong: %+v", got.Issues[1])
	}
}

func TestParseValidateResponse_DropsContinuationLines(t *testing.T) {
	// Real-world shape: each error followed by source echo + caret pointer.
	// Two declarative errors must surface as exactly two ValidationIssues.
	resp := `Errors encountered validating Jenkinsfile:
Not a valid section definition: "asdasdasd". Some extra configuration is required. @ line 7, column 1.
asdasdasd
^
Expected a build parameter definition @ line 20, column 5.
booleanParam(
^`
	got := parseValidateResponse(resp)
	if got.OK {
		t.Errorf("expected OK=false")
	}
	if len(got.Issues) != 2 {
		t.Fatalf("expected 2 issues (continuation lines must be dropped), got %d: %+v", len(got.Issues), got.Issues)
	}
	if got.Issues[0].Line != 7 || got.Issues[1].Line != 20 {
		t.Errorf("issue line numbers wrong: %+v", got.Issues)
	}
}

func TestParseValidateLine_SuffixStrippedFromMessage(t *testing.T) {
	iss := parseValidateLine(
		`Not a valid section definition: "x". Some extra configuration is required. @ line 7, column 1.`)
	if iss.Line != 7 || iss.Col != 1 {
		t.Fatalf("position wrong: %+v", iss)
	}
	if strings.Contains(iss.Message, "@ line") {
		t.Errorf("suffix not stripped from message: %q", iss.Message)
	}
}

func TestParseValidateResponse_FreeformError(t *testing.T) {
	got := parseValidateResponse("Jenkinsfile content '...' did not contain a Jenkinsfile")
	if got.OK || len(got.Issues) != 1 {
		t.Errorf("freeform error not surfaced: %+v", got)
	}
	if got.Issues[0].Line != 0 {
		t.Errorf("freeform error should have no line: %+v", got.Issues[0])
	}
}

func TestFormatErrorformat(t *testing.T) {
	r := ValidationResult{Issues: []ValidationIssue{
		{Line: 4, Col: 12, Message: `Expected "}"`},
		{Line: 7, Message: "Unknown step"},
		{Message: "whole-file error"},
	}}
	got := r.FormatErrorformat("/tmp/f.groovy")
	want := "/tmp/f.groovy:4:12: Expected \"}\"\n" +
		"/tmp/f.groovy:7: Unknown step\n" +
		"/tmp/f.groovy:1: whole-file error\n"
	if got != want {
		t.Errorf("errorformat output:\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}
