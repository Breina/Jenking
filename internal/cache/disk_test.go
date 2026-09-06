package cache

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/domain/pipelinesyntax"
)

func newTestDiskStore(t *testing.T) *DiskStore {
	t.Helper()
	dir := t.TempDir()
	d, err := NewDiskStore(dir)
	if err != nil {
		t.Fatalf("NewDiskStore: %v", err)
	}
	return d
}

func TestDiskStore_Registry_RoundTrip(t *testing.T) {
	d := newTestDiskStore(t)

	_, err := d.LoadRegistry()
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected ErrNotExist for missing file, got %v", err)
	}

	records := []buildregistry.Record{
		{
			JobPath:  "folder/job",
			Terminal: true,
			Build: jmodel.Build{
				Number:    1,
				Status:    jmodel.BuildStatusSuccess,
				Timestamp: time.Unix(1700000000, 0),
				Duration:  5 * time.Second,
			},
		},
		{
			JobPath:  "folder/job",
			Terminal: true,
			Build: jmodel.Build{
				Number:    2,
				Status:    jmodel.BuildStatusFailed,
				Timestamp: time.Unix(1700001000, 0),
				Duration:  3 * time.Second,
			},
		},
	}

	if err := d.SaveRegistry(records); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	got, err := d.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if len(got) != len(records) {
		t.Fatalf("expected %d records, got %d", len(records), len(got))
	}
	for i, want := range records {
		if got[i].JobPath != want.JobPath || got[i].Build.Number != want.Build.Number || got[i].Build.Status != want.Build.Status {
			t.Errorf("record[%d] mismatch: got %+v, want %+v", i, got[i], want)
		}
	}
}

func TestDiskStore_Stages_RoundTrip(t *testing.T) {
	d := newTestDiskStore(t)

	_, err := d.LoadStages("missing:1")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected ErrNotExist for missing key, got %v", err)
	}

	stages := []jmodel.Stage{
		{Name: "Build", Status: jmodel.BuildStatusSuccess, Duration: 10 * time.Second},
		{Name: "Test", Status: jmodel.BuildStatusFailed, Duration: 20 * time.Second},
	}

	if err := d.SaveStages("folder/job:42", stages); err != nil {
		t.Fatalf("SaveStages: %v", err)
	}

	got, err := d.LoadStages("folder/job:42")
	if err != nil {
		t.Fatalf("LoadStages: %v", err)
	}
	if len(got) != len(stages) {
		t.Fatalf("expected %d stages, got %d", len(stages), len(got))
	}
	for i, s := range stages {
		if got[i].Name != s.Name || got[i].Status != s.Status {
			t.Errorf("stage[%d] mismatch: got %+v, want %+v", i, got[i], s)
		}
	}
}

func TestDiskStore_Stages_MultipleKeys(t *testing.T) {
	d := newTestDiskStore(t)

	s1 := []jmodel.Stage{{Name: "A", Status: jmodel.BuildStatusSuccess}}
	s2 := []jmodel.Stage{{Name: "B", Status: jmodel.BuildStatusFailed}}

	if err := d.SaveStages("job:1", s1); err != nil {
		t.Fatalf("SaveStages job:1: %v", err)
	}
	if err := d.SaveStages("job:2", s2); err != nil {
		t.Fatalf("SaveStages job:2: %v", err)
	}

	got1, err := d.LoadStages("job:1")
	if err != nil || got1[0].Name != "A" {
		t.Errorf("job:1: got %v, err %v", got1, err)
	}
	got2, err := d.LoadStages("job:2")
	if err != nil || got2[0].Name != "B" {
		t.Errorf("job:2: got %v, err %v", got2, err)
	}
}

func TestDiskStore_TestReport_RoundTrip(t *testing.T) {
	d := newTestDiskStore(t)

	_, err := d.LoadTestReport("missing:1")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected ErrNotExist for missing key, got %v", err)
	}

	report := &jmodel.TestReport{
		Failed:  2,
		Passed:  10,
		Skipped: 1,
		Suites: []jmodel.TestSuite{
			{Name: "SuiteA", Cases: []jmodel.TestCase{
				{Name: "test1", Status: jmodel.TestStatusPassed},
				{Name: "test2", Status: jmodel.TestStatusFailed, ErrorDetails: "assertion failed"},
			}},
		},
	}

	if err := d.SaveTestReport("folder/job:7", report); err != nil {
		t.Fatalf("SaveTestReport: %v", err)
	}

	got, err := d.LoadTestReport("folder/job:7")
	if err != nil {
		t.Fatalf("LoadTestReport: %v", err)
	}
	if got.Failed != report.Failed || got.Passed != report.Passed {
		t.Errorf("report counts mismatch: got %+v", got)
	}
	if len(got.Suites) != 1 || len(got.Suites[0].Cases) != 2 {
		t.Errorf("suites/cases mismatch: got %+v", got.Suites)
	}
	if got.Suites[0].Cases[1].ErrorDetails != "assertion failed" {
		t.Errorf("ErrorDetails mismatch: got %q", got.Suites[0].Cases[1].ErrorDetails)
	}
}

func TestDiskStore_Jobs_RoundTrip(t *testing.T) {
	d := newTestDiskStore(t)

	_, err := d.LoadJobs("missing/folder")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected ErrNotExist for missing key, got %v", err)
	}

	jobs := []jmodel.Job{
		{Name: "pipeline-a", FullPath: "Code/pipeline-a", Type: jmodel.JobTypePipeline},
		{Name: "pipeline-b", FullPath: "Code/pipeline-b", Type: jmodel.JobTypePipeline},
	}

	if err := d.SaveJobs("Code", jobs); err != nil {
		t.Fatalf("SaveJobs: %v", err)
	}

	got, err := d.LoadJobs("Code")
	if err != nil {
		t.Fatalf("LoadJobs: %v", err)
	}
	if len(got) != len(jobs) {
		t.Fatalf("expected %d jobs, got %d", len(jobs), len(got))
	}
	for i, j := range jobs {
		if got[i].Name != j.Name || got[i].FullPath != j.FullPath {
			t.Errorf("job[%d] mismatch: got %+v, want %+v", i, got[i], j)
		}
	}
}

func TestDiskStore_Populate(t *testing.T) {
	d := newTestDiskStore(t)

	folderJobs := []jmodel.Job{{Name: "pipeline-a", FullPath: "Code/pipeline-a", Type: jmodel.JobTypePipeline}}
	stages := []jmodel.Stage{{Name: "Deploy", Status: jmodel.BuildStatusSuccess}}
	report := &jmodel.TestReport{Passed: 5}

	if err := d.SaveJobs("Code", folderJobs); err != nil {
		t.Fatalf("SaveJobs: %v", err)
	}
	if err := d.SaveStages("job:1", stages); err != nil {
		t.Fatalf("SaveStages: %v", err)
	}
	if err := d.SaveTestReport("job:1", report); err != nil {
		t.Fatalf("SaveTestReport: %v", err)
	}

	jobsCache := New[string, []jmodel.Job](0)
	stagesCache := New[string, []jmodel.Stage](0)
	reportsCache := New[string, *jmodel.TestReport](100)
	artifactsCache := New[string, []jmodel.Artifact](100)
	symbolsCache := New[string, *pipelinesyntax.Symbols](100)
	repoURLsCache := New[string, string](0)
	if err := d.SaveRepoURL("Code/git/omv/main", "https://github.com/org/omv/tree/main"); err != nil {
		t.Fatalf("SaveRepoURL: %v", err)
	}
	d.populate(jobsCache, stagesCache, reportsCache, artifactsCache, symbolsCache, repoURLsCache)

	if e := jobsCache.Get("Code"); e == nil || len(e.Value) != 1 {
		t.Errorf("Jobs not populated: %v", e)
	}
	if e := repoURLsCache.Get("Code/git/omv/main"); e == nil || e.Value == "" {
		t.Errorf("RepoURLs not populated: %v", e)
	}
	if e := stagesCache.Get("job:1"); e == nil || len(e.Value) != 1 {
		t.Errorf("Stages not populated: %v", e)
	}
	if e := reportsCache.Get("job:1"); e == nil || e.Value.Passed != 5 {
		t.Errorf("TestReports not populated: %v", e)
	}
}
