package cache

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/gofrs/flock"

	"github.com/Breina/Jenking/internal/domain/buildregistry"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/domain/pipelinesyntax"
)

// DiskStore persists immutable build data to disk using gob encoding.
// Data is stored under dir, one file per domain.
//
// Concurrency: a process-local mutex guards all read-modify-write operations so
// multiple goroutines (completion cascade cmds) can call Save* concurrently. A
// cross-process advisory file lock (gofrs/flock on <dir>/.lock) serializes RMW
// cycles between separate Jenking processes sharing one cache dir (e.g. the TUI
// and the MCP server), so keyed-map writes don't lose each other's entries.
type DiskStore struct {
	dir  string
	mu   sync.Mutex
	lock *flock.Flock

	// owner identifies this process in the shared live-poll file (see
	// ClaimPoll). Always the pid in production; overridden in tests to simulate
	// two processes sharing one cache dir.
	owner int
}

// NewDiskStore creates a DiskStore rooted at dir, creating the directory if needed.
func NewDiskStore(dir string) (*DiskStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &DiskStore{
		dir:   dir,
		lock:  flock.New(filepath.Join(dir, ".lock")),
		owner: os.Getpid(),
	}, nil
}

// withLock runs fn while holding both the process-local mutex and the
// cross-process advisory lock, so a read-modify-write cycle is atomic against
// other goroutines and other Jenking processes. The file lock is best-effort:
// if it can't be acquired (e.g. a filesystem without flock support) fn still
// runs under the local mutex.
func (d *DiskStore) withLock(fn func() error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lock != nil {
		if err := d.lock.Lock(); err == nil {
			defer func() { _ = d.lock.Unlock() }()
		}
	}
	return fn()
}

func (d *DiskStore) stagesPath() string      { return filepath.Join(d.dir, "stages.gob") }
func (d *DiskStore) testReportsPath() string { return filepath.Join(d.dir, "testreports.gob") }
func (d *DiskStore) artifactsPath() string   { return filepath.Join(d.dir, "artifacts.gob") }
func (d *DiskStore) jobsPath() string        { return filepath.Join(d.dir, "jobs.gob") }
func (d *DiskStore) registryPath() string    { return filepath.Join(d.dir, "registry.gob") }
func (d *DiskStore) symbolsPath() string     { return filepath.Join(d.dir, "symbols.gob") }
func (d *DiskStore) repoURLsPath() string    { return filepath.Join(d.dir, "repourls.gob") }

// LoadRegistry returns the persisted registry records, or os.ErrNotExist if absent.
func (d *DiskStore) LoadRegistry() ([]buildregistry.Record, error) {
	return readGob[[]buildregistry.Record](d.registryPath())
}

// SaveRegistry atomically writes registry records to disk, replacing whatever
// is there. Prefer SaveRegistryMerged when another process may share the dir.
func (d *DiskStore) SaveRegistry(records []buildregistry.Record) error {
	return d.withLock(func() error {
		return writeGob(d.registryPath(), records)
	})
}

// SaveRegistryMerged unions records with whatever is already on disk before
// writing, so a concurrent Jenking process's records aren't lost. The merge is
// terminal-is-sticky (see buildregistry.MergeRecords). The whole cycle runs
// under the cross-process lock.
func (d *DiskStore) SaveRegistryMerged(records []buildregistry.Record) error {
	return d.withLock(func() error {
		existing, err := readGob[[]buildregistry.Record](d.registryPath())
		if err != nil {
			existing = nil // absent or unreadable: treat as empty
		}
		return writeGob(d.registryPath(), buildregistry.MergeRecords(existing, records))
	})
}

// RemoveLegacyFiles deletes on-disk files left over from pre-registry versions.
// Called once at startup; errors are returned but typically ignored by the caller.
func (d *DiskStore) RemoveLegacyFiles() error {
	return d.withLock(func() error {
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
	})
}

// SaveRepoURL persists one job's SCM URL (jobPath -> url; "" means "no SCM URL")
// via a read-modify-write on the shared map file, so concurrent Jenking
// processes and the metadata view don't clobber each other's entries.
func (d *DiskStore) SaveRepoURL(jobPath, url string) error {
	return d.withLock(func() error {
		m, _ := readGob[map[string]string](d.repoURLsPath())
		if m == nil {
			m = make(map[string]string)
		}
		m[jobPath] = url
		return writeGob(d.repoURLsPath(), m)
	})
}

// LoadRepoURLs returns all persisted job SCM URLs, or os.ErrNotExist if absent.
func (d *DiskStore) LoadRepoURLs() (map[string]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return readGob[map[string]string](d.repoURLsPath())
}

// LoadStages returns the cached stages for the given key ("jobPath:buildNum").
func (d *DiskStore) LoadStages(key string) ([]jmodel.Stage, error) {
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
func (d *DiskStore) SaveStages(key string, stages []jmodel.Stage) error {
	return d.withLock(func() error {
		m, _ := d.loadAllStagesLocked()
		if m == nil {
			m = make(map[string][]jmodel.Stage)
		}
		m[key] = stages
		return writeGob(d.stagesPath(), m)
	})
}

// loadAllStages returns all persisted stages.
func (d *DiskStore) loadAllStages() (map[string][]jmodel.Stage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loadAllStagesLocked()
}

func (d *DiskStore) loadAllStagesLocked() (map[string][]jmodel.Stage, error) {
	return readGob[map[string][]jmodel.Stage](d.stagesPath())
}

// LoadTestReport returns the cached test report for the given key ("jobPath:buildNum").
func (d *DiskStore) LoadTestReport(key string) (*jmodel.TestReport, error) {
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
func (d *DiskStore) SaveTestReport(key string, report *jmodel.TestReport) error {
	return d.withLock(func() error {
		m, _ := d.loadAllTestReportsLocked()
		if m == nil {
			m = make(map[string]*jmodel.TestReport)
		}
		m[key] = report
		return writeGob(d.testReportsPath(), m)
	})
}

// loadAllTestReports returns all persisted test reports.
func (d *DiskStore) loadAllTestReports() (map[string]*jmodel.TestReport, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loadAllTestReportsLocked()
}

func (d *DiskStore) loadAllTestReportsLocked() (map[string]*jmodel.TestReport, error) {
	return readGob[map[string]*jmodel.TestReport](d.testReportsPath())
}

// LoadArtifacts returns the cached artifact list for the given key ("jobPath:buildNum").
func (d *DiskStore) LoadArtifacts(key string) ([]jmodel.Artifact, error) {
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
func (d *DiskStore) SaveArtifacts(key string, artifacts []jmodel.Artifact) error {
	return d.withLock(func() error {
		m, _ := d.loadAllArtifactsLocked()
		if m == nil {
			m = make(map[string][]jmodel.Artifact)
		}
		m[key] = artifacts
		return writeGob(d.artifactsPath(), m)
	})
}

func (d *DiskStore) loadAllArtifacts() (map[string][]jmodel.Artifact, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loadAllArtifactsLocked()
}

func (d *DiskStore) loadAllArtifactsLocked() (map[string][]jmodel.Artifact, error) {
	return readGob[map[string][]jmodel.Artifact](d.artifactsPath())
}

// LoadJobs returns the cached job listing for the given folder path.
func (d *DiskStore) LoadJobs(folderPath string) ([]jmodel.Job, error) {
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
func (d *DiskStore) SaveJobs(folderPath string, jobs []jmodel.Job) error {
	return d.withLock(func() error {
		m, _ := d.loadAllJobsLocked()
		if m == nil {
			m = make(map[string][]jmodel.Job)
		}
		m[folderPath] = jobs
		return writeGob(d.jobsPath(), m)
	})
}

// loadAllJobs returns all persisted folder job listings.
func (d *DiskStore) loadAllJobs() (map[string][]jmodel.Job, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loadAllJobsLocked()
}

func (d *DiskStore) loadAllJobsLocked() (map[string][]jmodel.Job, error) {
	return readGob[map[string][]jmodel.Job](d.jobsPath())
}

// LoadSymbols returns the cached Symbols for the given key ("jobPath#buildNum").
func (d *DiskStore) LoadSymbols(key string) (*pipelinesyntax.Symbols, error) {
	m, err := d.loadAllSymbols()
	if err != nil {
		return nil, err
	}
	v, ok := m[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return v, nil
}

// SaveSymbols persists Symbols for a single build (read-modify-write on the
// shared map file). Builds are immutable, so entries effectively never expire.
func (d *DiskStore) SaveSymbols(key string, sym *pipelinesyntax.Symbols) error {
	return d.withLock(func() error {
		m, _ := d.loadAllSymbolsLocked()
		if m == nil {
			m = make(map[string]*pipelinesyntax.Symbols)
		}
		m[key] = sym
		return writeGob(d.symbolsPath(), m)
	})
}

func (d *DiskStore) loadAllSymbols() (map[string]*pipelinesyntax.Symbols, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loadAllSymbolsLocked()
}

func (d *DiskStore) loadAllSymbolsLocked() (map[string]*pipelinesyntax.Symbols, error) {
	return readGob[map[string]*pipelinesyntax.Symbols](d.symbolsPath())
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

// writeGob atomically encodes v to path via a temp file + rename. The temp
// name carries the pid so two Jenking processes sharing a cache dir never write
// the same temp file (the rename onto path is atomic either way).
func writeGob(path string, v any) error {
	tmp := path + "." + strconv.Itoa(os.Getpid()) + ".tmp"
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
	jobs *Cache[string, []jmodel.Job],
	stages *Cache[string, []jmodel.Stage],
	testReports *Cache[string, *jmodel.TestReport],
	artifacts *Cache[string, []jmodel.Artifact],
	symbols *Cache[string, *pipelinesyntax.Symbols],
	repoURLs *Cache[string, string],
) {
	if jm, err := d.loadAllJobs(); err == nil {
		for fp, j := range jm {
			jobs.Put(fp, j)
		}
	}

	if rm, err := d.LoadRepoURLs(); err == nil {
		for jp, u := range rm {
			repoURLs.Put(jp, u)
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

	if sm, err := d.loadAllSymbols(); err == nil {
		for key, s := range sm {
			symbols.Put(key, s)
		}
	}
}
