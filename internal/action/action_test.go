package action

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/tui/command"
)

// fakeClient implements the apiClient subset for tests.
type fakeClient struct {
	builds      []jenkins.Build
	console     string
	script      string
	testReport  *jenkins.TestReport
	listErr     error
	consoleErr  error
	scriptErr   error
	reportErr   error
	gotJobPath  string
	gotBuildNum int
}

func (f *fakeClient) ListBuilds(_ context.Context, jobPath string) ([]jenkins.Build, error) {
	f.gotJobPath = jobPath
	return f.builds, f.listErr
}

func (f *fakeClient) GetFullConsoleText(_ context.Context, jobPath string, n int) (string, error) {
	f.gotJobPath = jobPath
	f.gotBuildNum = n
	return f.console, f.consoleErr
}

func (f *fakeClient) GetBuildScript(_ context.Context, jobPath string, n int) (string, error) {
	f.gotJobPath = jobPath
	f.gotBuildNum = n
	return f.script, f.scriptErr
}

func (f *fakeClient) GetTestReport(_ context.Context, jobPath string, n int) (*jenkins.TestReport, error) {
	f.gotJobPath = jobPath
	f.gotBuildNum = n
	return f.testReport, f.reportErr
}

func newStoreWithProject(_ *testing.T) *cache.Store {
	s := cache.NewStore(nil)
	s.Jobs.Put("", []jenkins.Job{
		{Name: "webidm", FullPath: "webidm", Type: jenkins.JobTypeMultiBranch},
	})
	return s
}

func TestRun_Logs(t *testing.T) {
	c := &fakeClient{console: "build output\n"}
	var buf bytes.Buffer
	err := runWith(context.Background(), c, newStoreWithProject(t), Request{
		Kind:   KindLogs,
		Target: command.Target{ProjectSuffix: "webidm", Branch: "main", Build: command.BuildRef{Number: 42, Set: true}},
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "build output\n" {
		t.Errorf("got %q", buf.String())
	}
	if c.gotJobPath != "webidm/main" || c.gotBuildNum != 42 {
		t.Errorf("api called with jobPath=%q buildNum=%d", c.gotJobPath, c.gotBuildNum)
	}
}

func TestRun_LogsResolvesLast(t *testing.T) {
	c := &fakeClient{
		builds:  []jenkins.Build{{Number: 99}, {Number: 98}},
		console: "latest output",
	}
	var buf bytes.Buffer
	err := runWith(context.Background(), c, newStoreWithProject(t), Request{
		Kind:   KindLogs,
		Target: command.Target{ProjectSuffix: "webidm", Branch: "main", Build: command.BuildRef{IsLast: true, Set: true}},
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if c.gotBuildNum != 99 {
		t.Errorf("expected last build 99, got %d", c.gotBuildNum)
	}
	if buf.String() != "latest output" {
		t.Errorf("got %q", buf.String())
	}
}

func TestRun_LogsNoBuilds(t *testing.T) {
	c := &fakeClient{builds: nil}
	var buf bytes.Buffer
	err := runWith(context.Background(), c, newStoreWithProject(t), Request{
		Kind:   KindLogs,
		Target: command.Target{ProjectSuffix: "webidm", Branch: "main", Build: command.BuildRef{IsLast: true, Set: true}},
	}, &buf)
	if err == nil || !strings.Contains(err.Error(), "no builds found") {
		t.Errorf("expected 'no builds found' error, got %v", err)
	}
}

func TestRun_Describe(t *testing.T) {
	c := &fakeClient{script: "pipeline { stages {} }"}
	var buf bytes.Buffer
	err := runWith(context.Background(), c, newStoreWithProject(t), Request{
		Kind:   KindDescribe,
		Target: command.Target{ProjectSuffix: "webidm", Branch: "main", Build: command.BuildRef{Number: 7, Set: true}},
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "pipeline { stages {} }" {
		t.Errorf("got %q", buf.String())
	}
}

func TestRun_Tests(t *testing.T) {
	c := &fakeClient{testReport: &jenkins.TestReport{Passed: 10, Failed: 2, Skipped: 1}}
	var buf bytes.Buffer
	err := runWith(context.Background(), c, newStoreWithProject(t), Request{
		Kind:   KindTests,
		Target: command.Target{ProjectSuffix: "webidm", Branch: "main", Build: command.BuildRef{Number: 7, Set: true}},
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	var got jenkins.TestReport
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if got.Passed != 10 || got.Failed != 2 || got.Skipped != 1 {
		t.Errorf("decoded report mismatch: %+v", got)
	}
}

func TestRun_TestsNoReport(t *testing.T) {
	c := &fakeClient{testReport: nil}
	var buf bytes.Buffer
	err := runWith(context.Background(), c, newStoreWithProject(t), Request{
		Kind:   KindTests,
		Target: command.Target{ProjectSuffix: "webidm", Branch: "main", Build: command.BuildRef{Number: 7, Set: true}},
	}, &buf)
	if err == nil || !strings.Contains(err.Error(), "no test report") {
		t.Errorf("expected 'no test report' error, got %v", err)
	}
}

func TestRun_NoProject(t *testing.T) {
	c := &fakeClient{}
	var buf bytes.Buffer
	err := runWith(context.Background(), c, cache.NewStore(nil), Request{
		Kind:   KindLogs,
		Target: command.Target{Build: command.BuildRef{Number: 1, Set: true}},
	}, &buf)
	if err == nil || !strings.Contains(err.Error(), "project required") {
		t.Errorf("expected 'project required' error, got %v", err)
	}
}

func TestRun_UnknownProject(t *testing.T) {
	c := &fakeClient{}
	var buf bytes.Buffer
	err := runWith(context.Background(), c, newStoreWithProject(t), Request{
		Kind:   KindLogs,
		Target: command.Target{ProjectSuffix: "nope", Branch: "main", Build: command.BuildRef{Number: 1, Set: true}},
	}, &buf)
	if err == nil || !strings.Contains(err.Error(), "unknown project") {
		t.Errorf("expected 'unknown project' error, got %v", err)
	}
}

func TestRun_APIError(t *testing.T) {
	want := errors.New("network down")
	c := &fakeClient{consoleErr: want}
	var buf bytes.Buffer
	err := runWith(context.Background(), c, newStoreWithProject(t), Request{
		Kind:   KindLogs,
		Target: command.Target{ProjectSuffix: "webidm", Branch: "main", Build: command.BuildRef{Number: 1, Set: true}},
	}, &buf)
	if !errors.Is(err, want) {
		t.Errorf("expected wrapped network error, got %v", err)
	}
}

func TestParseKind(t *testing.T) {
	cases := map[string]Kind{
		"logs":     KindLogs,
		"log":      KindLogs,
		"l":        KindLogs,
		"describe": KindDescribe,
		"desc":     KindDescribe,
		"tests":    KindTests,
		"test":     KindTests,
	}
	for verb, want := range cases {
		got, ok := ParseKind(verb)
		if !ok || got != want {
			t.Errorf("ParseKind(%q) = (%d, %v), want (%d, true)", verb, got, ok, want)
		}
	}
	if _, ok := ParseKind("nope"); ok {
		t.Error("ParseKind(nope) should fail")
	}
}
