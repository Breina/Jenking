package cache

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"sync"

	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/jenkins"
)

// DiskStore persists immutable build data to disk using gob encoding.
// Data is stored under dir, one file per domain.
//
// Concurrency: a mutex guards all read-modify-write operations so multiple
// goroutines (completion cascade cmds) can call Save* concurrently.
type DiskStore struct {
	dir string
	mu  sync.Mutex
}

// NewDiskStore creates a DiskStore rooted at dir, creating the directory if needed.
func NewDiskStore(dir string) (*DiskStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &DiskStore{dir: dir}, nil
}

func (d *DiskStore) stagesPath() string      { return filepath.Join(d.dir, "stages.gob") }
func (d *DiskStore) testReportsPath() string { return filepath.Join(d.dir, "testreports.gob") }
func (d *DiskStore) artifactsPath() string   { return filepath.Join(d.dir, "artifacts.gob") }
func (d *DiskStore) jobsPath() string        { return filepath.Join(d.dir, "jobs.gob") }
func (d *DiskStore) registryPath() string    { return filepath.Join(d.dir, "registry.gob") }

// LoadRegistry returns the persisted registry records, or os.ErrNotExist if absent.
func (d *DiskStore) LoadRegistry() ([]buildregistry.Record, error) {
	return readGob[[]buildregistry.Record](d.registryPath())
}

// SaveRegistry atomically writes registry records to disk.
func (d *DiskStore) SaveRegistry(records []buildregistry.Record) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return writeGob(d.registryPath(), records)
}

// RemoveLegacyFiles deletes on-disk files left over from pre-registry versions.
// Called once at startup; errors are returned but typically ignored by the caller.
func (d *DiskStore) RemoveLegacyFiles() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	legacy := []string{
		filepath.Join(d.dir, "allbuilds.gob"),
		filepath.Join(d.dir, "allbuilds.gob.tmp"),
	}
	for _, p := range legacy {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// LoadStages returns the cached stages for the given key ("jobPath:buildNum").
func (d *DiskStore) LoadStages(key string) ([]jenkins.Stage, error) {
	m, err := d.loadAllStages()
	if err != nil {
		return nil, err
	}
	v, ok := m[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return v, nil
}

// SaveStages persists stages for a single build (read-modify-write on the shared map file).
func (d *DiskStore) SaveStages(key string, stages []jenkins.Stage) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	m, _ := d.loadAllStagesLocked()
	if m == nil {
		m = make(map[string][]jenkins.Stage)
	}
	m[key] = stages
	return writeGob(d.stagesPath(), m)
}

// loadAllStages returns all persisted stages.
func (d *DiskStore) loadAllStages() (map[string][]jenkins.Stage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loadAllStagesLocked()
}

func (d *DiskStore) loadAllStagesLocked() (map[string][]jenkins.Stage, error) {
	return readGob[map[string][]jenkins.Stage](d.stagesPath())
}

// LoadTestReport returns the cached test report for the given key ("jobPath:buildNum").
func (d *DiskStore) LoadTestReport(key string) (*jenkins.TestReport, error) {
	m, err := d.loadAllTestReports()
	if err != nil {
		return nil, err
	}
	v, ok := m[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return v, nil
}

// SaveTestReport persists a test report for a single build (read-modify-write).
func (d *DiskStore) SaveTestReport(key string, report *jenkins.TestReport) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	m, _ := d.loadAllTestReportsLocked()
	if m == nil {
		m = make(map[string]*jenkins.TestReport)
	}
	m[key] = report
	return writeGob(d.testReportsPath(), m)
}

// loadAllTestReports returns all persisted test reports.
func (d *DiskStore) loadAllTestReports() (map[string]*jenkins.TestReport, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loadAllTestReportsLocked()
}

func (d *DiskStore) loadAllTestReportsLocked() (map[string]*jenkins.TestReport, error) {
	return readGob[map[string]*jenkins.TestReport](d.testReportsPath())
}

// LoadArtifacts returns the cached artifact list for the given key ("jobPath:buildNum").
func (d *DiskStore) LoadArtifacts(key string) ([]jenkins.Artifact, error) {
	m, err := d.loadAllArtifacts()
	if err != nil {
		return nil, err
	}
	v, ok := m[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return v, nil
}

// SaveArtifacts persists the artifact list for a single build (read-modify-write).
func (d *DiskStore) SaveArtifacts(key string, artifacts []jenkins.Artifact) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	m, _ := d.loadAllArtifactsLocked()
	if m == nil {
		m = make(map[string][]jenkins.Artifact)
	}
	m[key] = artifacts
	return writeGob(d.artifactsPath(), m)
}

func (d *DiskStore) loadAllArtifacts() (map[string][]jenkins.Artifact, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loadAllArtifactsLocked()
}

func (d *DiskStore) loadAllArtifactsLocked() (map[string][]jenkins.Artifact, error) {
	return readGob[map[string][]jenkins.Artifact](d.artifactsPath())
}

// LoadJobs returns the cached job listing for the given folder path.
func (d *DiskStore) LoadJobs(folderPath string) ([]jenkins.Job, error) {
	m, err := d.loadAllJobs()
	if err != nil {
		return nil, err
	}
	v, ok := m[folderPath]
	if !ok {
		return nil, os.ErrNotExist
	}
	return v, nil
}

// SaveJobs persists the job listing for a single folder (read-modify-write).
func (d *DiskStore) SaveJobs(folderPath string, jobs []jenkins.Job) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	m, _ := d.loadAllJobsLocked()
	if m == nil {
		m = make(map[string][]jenkins.Job)
	}
	m[folderPath] = jobs
	return writeGob(d.jobsPath(), m)
}

// loadAllJobs returns all persisted folder job listings.
func (d *DiskStore) loadAllJobs() (map[string][]jenkins.Job, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loadAllJobsLocked()
}

func (d *DiskStore) loadAllJobsLocked() (map[string][]jenkins.Job, error) {
	return readGob[map[string][]jenkins.Job](d.jobsPath())
}

// readGob reads and decodes a gob file into T.
func readGob[T any](path string) (T, error) {
	var v T
	f, err := os.Open(path)
	if err != nil {
		return v, err
	}
	defer f.Close()
	return v, gob.NewDecoder(f).Decode(&v)
}

// writeGob atomically encodes v to path via a temp file + rename.
func writeGob(path string, v any) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(v); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// populate loads all persisted data into the provided caches.
// Called once from NewStore at startup; errors are silently ignored (cache is regenerable).
// Build status lives in the registry, loaded separately via LoadRegistry.
func (d *DiskStore) populate(
	jobs *Cache[string, []jenkins.Job],
	stages *Cache[string, []jenkins.Stage],
	testReports *Cache[string, *jenkins.TestReport],
	artifacts *Cache[string, []jenkins.Artifact],
) {
	if jm, err := d.loadAllJobs(); err == nil {
		for fp, j := range jm {
			jobs.Put(fp, j)
		}
	}

	if sm, err := d.loadAllStages(); err == nil {
		for key, st := range sm {
			stages.Put(key, st)
		}
	}

	if rm, err := d.loadAllTestReports(); err == nil {
		for key, r := range rm {
			testReports.Put(key, r)
		}
	}

	if am, err := d.loadAllArtifacts(); err == nil {
		for key, a := range am {
			artifacts.Put(key, a)
		}
	}
}
