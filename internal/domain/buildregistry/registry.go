// Package buildregistry is the single source of truth for build state across
// the TUI. Every poller (running-set monitor, all-builds scan, branch and
// project lists) feeds Records in through ingress methods that enforce
// invariants; views read through Query.
//
// Invariants:
//  1. Terminal-is-sticky: once a record's Status is a terminal value
//     (Success/Failed/Aborted/Unstable/NotBuilt), it cannot transition back to
//     Running. ApplyCompletion sets Terminal=true.
//  2. Running requires live confirmation: Query downgrades Status=Running to
//     Unknown unless the key is in the most-recent running-set snapshot OR the
//     record was confirmed running within RunTTL. This closes the bug where a
//     scan reported building=true after the executor had already released.
//  3. All mutation goes through Ingest*/Apply*; callers cannot write the
//     records map directly.
package buildregistry

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// retainPerJob bounds how many records the registry keeps per job path, both in
// memory and on disk (see boundPerJob). It matches the depth of a branch build
// list fetch (ListBuilds requests {0,25}) so the cached set holds everything a
// branch view can display — a fetch never returns more, so a higher cap would
// only retain stale history. The all-builds scan returns maxBuildsPerJob (10),
// well under this. Currently-running builds are always retained regardless.
const retainPerJob = 25

// Source identifies which ingress wrote a record.
type Source int

const (
	SourceUnknown Source = iota
	SourceRunningPoll
	SourceScan
	SourceBranchList
	SourceProjectList
	SourceCompletion
	SourceDiskLoad
)

// Key identifies a build globally.
type Key struct {
	JobPath string
	Number  int
}

// String returns "jobPath#number", matching jmodel.BuildKey.
func (k Key) String() string { return jmodel.BuildKey(k.JobPath, k.Number) }

// KeyOf returns the Key for a UserBuild.
func KeyOf(b jmodel.UserBuild) Key { return Key{JobPath: b.JobPath, Number: b.Number} }

// Record is the canonical per-build state stored in the registry.
type Record struct {
	Build           jmodel.Build
	JobPath         string
	BranchName      string // for multibranch projects
	DisplayName     string
	Node            string
	LastSeenRunning time.Time // last running-set confirmation
	LastWriter      Source
	Terminal        bool // true once a terminal Status has been confirmed
	UpdatedAt       time.Time
}

// UserBuild materializes a Record as a UserBuild with the *display* status
// (after applying invariant 2).
func (r Record) UserBuild(displayStatus jmodel.BuildStatus) jmodel.UserBuild {
	b := r.Build
	b.Status = displayStatus
	return jmodel.UserBuild{
		JobPath:     r.JobPath,
		Node:        r.Node,
		DisplayName: r.DisplayName,
		Build:       b,
	}
}

// Filter selects records for Query.
type Filter struct {
	FolderPrefix string // matches when JobPath has this prefix + "/"
	JobPath      string // exact match
	ProjectPath  string // matches JobPath prefix (for multibranch project view)
	OnlyRunning  bool
}

func (f Filter) matches(r Record) bool {
	if f.JobPath != "" && r.JobPath != f.JobPath {
		return false
	}
	if f.FolderPrefix != "" && !strings.HasPrefix(r.JobPath, f.FolderPrefix+"/") {
		return false
	}
	if f.ProjectPath != "" && !strings.HasPrefix(r.JobPath, f.ProjectPath+"/") {
		return false
	}
	return true
}

// ReconcileFn is invoked when the registry needs an authoritative status fetch
// for a build (e.g. a key just departed the running set, or a scan reported
// Running but the key is not in the live set). The implementation should
// asynchronously call GetBuild and feed the result back via ApplyCompletion.
type ReconcileFn func(Key)

// ChangeFn is invoked (synchronously, under the registry lock) after every
// mutation. Use it to wake subscribers / schedule re-renders. Keep it cheap.
type ChangeFn func()

// PersistFn is invoked after every mutation, with the full record set. The
// implementation may debounce; the registry passes a snapshot copy so the
// callback can write to disk without holding the lock.
type PersistFn func([]Record)

// Registry is the single source of truth for build state.
type Registry struct {
	mu          sync.RWMutex
	records     map[Key]Record
	liveRunning map[Key]struct{}
	liveRunAt   time.Time

	runTTL    time.Duration
	now       func() time.Time
	reconcile ReconcileFn
	onChange  ChangeFn
	persist   PersistFn
}

// Config configures a Registry.
type Config struct {
	// RunTTL is how long after the last running-set confirmation Query keeps
	// returning Running for a record before downgrading to Unknown. Default 5s.
	RunTTL time.Duration
	// Now overrides the clock (for tests). Default time.Now.
	Now func() time.Time
	// Reconcile is called when a key needs an authoritative status fetch.
	Reconcile ReconcileFn
	// OnChange is called after every mutation.
	OnChange ChangeFn
	// Persist is called after every mutation, with a snapshot of records.
	Persist PersistFn
}

// New creates a Registry from cfg.
func New(cfg Config) *Registry {
	if cfg.RunTTL <= 0 {
		cfg.RunTTL = 5 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Registry{
		records:     make(map[Key]Record),
		liveRunning: make(map[Key]struct{}),
		runTTL:      cfg.RunTTL,
		now:         cfg.Now,
		reconcile:   cfg.Reconcile,
		onChange:    cfg.OnChange,
		persist:     cfg.Persist,
	}
}

// isTerminal reports whether s is a terminal (non-running, non-unknown) status.
func isTerminal(s jmodel.BuildStatus) bool {
	switch s {
	case jmodel.BuildStatusSuccess, jmodel.BuildStatusFailed,
		jmodel.BuildStatusAborted, jmodel.BuildStatusUnstable,
		jmodel.BuildStatusNotBuilt:
		return true
	}
	return false
}

// upsertLocked merges b into the existing record (or creates one). Terminal
// records are never overwritten by a non-terminal status (invariant 1).
// Returns true if any field changed.
func (r *Registry) upsertLocked(k Key, b jmodel.Build, jobPath, branchName, displayName, node string, src Source, runningSeenAt time.Time) bool {
	now := r.now()
	cur, exists := r.records[k]
	if exists && cur.Terminal && !isTerminal(b.Status) {
		// Invariant 1: refuse to undo terminal status.
		if !runningSeenAt.IsZero() && runningSeenAt.After(cur.LastSeenRunning) {
			cur.LastSeenRunning = runningSeenAt
			r.records[k] = cur
			return true
		}
		return false
	}

	next := cur
	next.JobPath = jobPath
	if branchName != "" {
		next.BranchName = branchName
	}
	if displayName != "" {
		next.DisplayName = displayName
	}
	if node != "" {
		next.Node = node
	}
	// Preserve duration/timestamp from a more authoritative writer if the new
	// payload is empty.
	merged := b
	if !exists {
		next.Build = merged
	} else {
		if merged.Timestamp.IsZero() {
			merged.Timestamp = cur.Build.Timestamp
		}
		if merged.Duration == 0 {
			merged.Duration = cur.Build.Duration
		}
		if merged.EstimatedDuration == 0 {
			merged.EstimatedDuration = cur.Build.EstimatedDuration
		}
		if merged.TriggeredBy == "" {
			merged.TriggeredBy = cur.Build.TriggeredBy
		}
		if merged.TriggeredByName == "" {
			merged.TriggeredByName = cur.Build.TriggeredByName
		}
		if merged.Cause == "" {
			merged.Cause = cur.Build.Cause
		}
		if merged.Params == nil {
			merged.Params = cur.Build.Params
		}
		// A lightweight running-poll payload carries no name/description; keep the
		// values a fuller fetch already recorded instead of blanking them.
		if merged.Name == "" {
			merged.Name = cur.Build.Name
		}
		if merged.Description == "" {
			merged.Description = cur.Build.Description
		}
		next.Build = merged
	}
	next.LastWriter = src
	next.UpdatedAt = now
	if !runningSeenAt.IsZero() {
		next.LastSeenRunning = runningSeenAt
	}
	if isTerminal(merged.Status) {
		next.Terminal = true
	}
	r.records[k] = next
	return true
}

// pruneLocked evicts stale completed records so the registry stays bounded.
// For each job path it keeps the retainPerJob highest build numbers; older
// records are dropped, except any build that is currently live (running), which
// is never evicted. Must be called with r.mu held for writing.
func (r *Registry) pruneLocked() {
	byJob := make(map[string][]int)
	for k := range r.records {
		byJob[k.JobPath] = append(byJob[k.JobPath], k.Number)
	}
	for jobPath, nums := range byJob {
		if len(nums) <= retainPerJob {
			continue
		}
		// Highest build numbers first; evict everything past the retention cap.
		sort.Sort(sort.Reverse(sort.IntSlice(nums)))
		for _, n := range nums[retainPerJob:] {
			k := Key{JobPath: jobPath, Number: n}
			if _, live := r.liveRunning[k]; live {
				continue // never evict a running build
			}
			delete(r.records, k)
		}
	}
}

// IngestRunningSnapshot replaces liveRunning with builds and upserts each.
// For every key that was in the previous liveRunning set and is now absent,
// schedules a reconciliation fetch.
func (r *Registry) IngestRunningSnapshot(builds []jmodel.UserBuild, polledAt time.Time) {
	r.mu.Lock()
	if polledAt.IsZero() {
		polledAt = r.now()
	}
	newLive := make(map[Key]struct{}, len(builds))
	for _, b := range builds {
		k := KeyOf(b)
		newLive[k] = struct{}{}
		// Force Status=Running from a live executor poll. Terminal records are
		// shielded by upsertLocked.
		bb := b.Build
		bb.Status = jmodel.BuildStatusRunning
		r.upsertLocked(k, bb, b.JobPath, "", b.DisplayName, b.Node, SourceRunningPoll, polledAt)
	}
	departed := make([]Key, 0)
	for k := range r.liveRunning {
		if _, still := newLive[k]; !still {
			departed = append(departed, k)
		}
	}
	r.liveRunning = newLive
	r.liveRunAt = polledAt
	reconcile := r.reconcile
	onChange := r.onChange
	snapshot := r.snapshotLocked()
	persist := r.persist
	r.mu.Unlock()

	if reconcile != nil {
		for _, k := range departed {
			reconcile(k)
		}
	}
	if onChange != nil {
		onChange()
	}
	if persist != nil {
		persist(snapshot)
	}
}

// IngestScan upserts builds from a wide scan (ScanAllBuilds). A "Running"
// status from a scan is stored as-is — Query will gate its visibility via
// invariant 2 and schedule a reconciliation if needed.
func (r *Registry) IngestScan(builds []jmodel.UserBuild) {
	r.ingestList(builds, SourceScan)
}

func (r *Registry) ingestList(builds []jmodel.UserBuild, src Source) {
	r.mu.Lock()
	unconfirmed := make([]Key, 0)
	for _, b := range builds {
		k := KeyOf(b)
		r.upsertLocked(k, b.Build, b.JobPath, "", b.DisplayName, b.Node, src, time.Time{})
		if b.Status == jmodel.BuildStatusRunning {
			if _, live := r.liveRunning[k]; !live {
				unconfirmed = append(unconfirmed, k)
			}
		}
	}
	r.pruneLocked()
	snapshot := r.snapshotLocked()
	reconcile := r.reconcile
	onChange := r.onChange
	persist := r.persist
	r.mu.Unlock()

	if reconcile != nil {
		for _, k := range unconfirmed {
			reconcile(k)
		}
	}
	if onChange != nil {
		onChange()
	}
	if persist != nil {
		persist(snapshot)
	}
}

// IngestBranchList upserts builds for a single branch/job (BranchBuildsProvider).
func (r *Registry) IngestBranchList(jobPath string, builds []jmodel.Build) {
	ub := make([]jmodel.UserBuild, len(builds))
	for i, b := range builds {
		ub[i] = jmodel.UserBuild{JobPath: jobPath, Build: b}
	}
	r.ingestList(ub, SourceBranchList)
}

// IngestProjectList upserts builds for all branches of a multibranch project.
func (r *Registry) IngestProjectList(projectPath string, builds []jmodel.ProjectBuild) {
	r.mu.Lock()
	unconfirmed := make([]Key, 0)
	for _, b := range builds {
		k := Key{JobPath: b.BranchPath, Number: b.Number}
		r.upsertLocked(k, b.Build, b.BranchPath, b.BranchName, "", "", SourceProjectList, time.Time{})
		if b.Status == jmodel.BuildStatusRunning {
			if _, live := r.liveRunning[k]; !live {
				unconfirmed = append(unconfirmed, k)
			}
		}
	}
	r.pruneLocked()
	snapshot := r.snapshotLocked()
	reconcile := r.reconcile
	onChange := r.onChange
	persist := r.persist
	r.mu.Unlock()

	if reconcile != nil {
		for _, k := range unconfirmed {
			reconcile(k)
		}
	}
	if onChange != nil {
		onChange()
	}
	if persist != nil {
		persist(snapshot)
	}
}

// ApplyCompletion installs the terminal status of a build. This is unconditional
// (it can finalize a record the registry didn't yet know about) and locks the
// record (Terminal=true) so subsequent scans cannot revert it to Running.
func (r *Registry) ApplyCompletion(k Key, b jmodel.Build) {
	r.mu.Lock()
	// Force status to whatever the completion fetch returned; the upsert will
	// still gate by isTerminal — but if b.Status is somehow still Running, we
	// trust the completion fetch and store as-is (the executor said it left).
	// In that case Query's invariant 2 will downgrade it anyway.
	prev, existed := r.records[k]
	r.upsertLocked(k, b, k.JobPath, "", "", "", SourceCompletion, time.Time{})
	if existed && prev.BranchName != "" {
		rec := r.records[k]
		rec.BranchName = prev.BranchName
		r.records[k] = rec
	}
	// Once a completion fetch has come back, the key is definitively NOT
	// running, regardless of what Status came back.
	delete(r.liveRunning, k)
	if isTerminal(b.Status) {
		rec := r.records[k]
		rec.Terminal = true
		r.records[k] = rec
	}
	snapshot := r.snapshotLocked()
	onChange := r.onChange
	persist := r.persist
	r.mu.Unlock()

	if onChange != nil {
		onChange()
	}
	if persist != nil {
		persist(snapshot)
	}
}

// LoadFromDisk seeds the registry from persisted records. Any non-terminal
// Running entries will be invisible to Query until a live IngestRunningSnapshot
// confirms them (their LastSeenRunning is from a previous run and far in the
// past, so invariant 2 downgrades to Unknown and a reconcile is scheduled).
func (r *Registry) LoadFromDisk(records []Record) {
	r.mu.Lock()
	reconcileKeys := make([]Key, 0)
	for _, rec := range records {
		k := Key{JobPath: rec.JobPath, Number: rec.Build.Number}
		// Don't trust LastSeenRunning across restarts.
		rec.LastSeenRunning = time.Time{}
		rec.LastWriter = SourceDiskLoad
		r.records[k] = rec
		if !rec.Terminal && rec.Build.Status == jmodel.BuildStatusRunning {
			reconcileKeys = append(reconcileKeys, k)
		}
	}
	// Bound the loaded set the same way ingests do, so a disk file that still
	// holds pre-cap history doesn't briefly render more builds than a fetch will
	// return (which then "trims" once the first live list comes back).
	r.pruneLocked()
	reconcile := r.reconcile
	onChange := r.onChange
	r.mu.Unlock()

	if reconcile != nil {
		for _, k := range reconcileKeys {
			reconcile(k)
		}
	}
	if onChange != nil {
		onChange()
	}
}

// displayStatus applies invariant 2: a record's stored Status=Running is
// only shown as Running if the key is in the latest running snapshot OR was
// confirmed there within RunTTL. Otherwise we return Unknown (and the caller
// schedules a reconcile via needsReconcile).
func (r *Registry) displayStatusLocked(k Key, rec Record) (jmodel.BuildStatus, bool) {
	if rec.Build.Status != jmodel.BuildStatusRunning || rec.Terminal {
		return rec.Build.Status, false
	}
	if _, live := r.liveRunning[k]; live {
		return jmodel.BuildStatusRunning, false
	}
	if !rec.LastSeenRunning.IsZero() && r.now().Sub(rec.LastSeenRunning) <= r.runTTL {
		return jmodel.BuildStatusRunning, false
	}
	return jmodel.BuildStatusUnknown, true
}

// Query returns all UserBuilds matching filter, with statuses gated by
// invariant 2. Sorted newest-timestamp-first.
func (r *Registry) Query(filter Filter) []jmodel.UserBuild {
	r.mu.RLock()
	out := make([]jmodel.UserBuild, 0, len(r.records))
	var needRecon []Key
	for k, rec := range r.records {
		if !filter.matches(rec) {
			continue
		}
		status, recon := r.displayStatusLocked(k, rec)
		if recon {
			needRecon = append(needRecon, k)
		}
		if filter.OnlyRunning && status != jmodel.BuildStatusRunning {
			continue
		}
		out = append(out, rec.UserBuild(status))
	}
	reconcile := r.reconcile
	r.mu.RUnlock()

	// Sort newest first.
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.After(out[j].Timestamp)
	})

	if reconcile != nil {
		for _, k := range needRecon {
			reconcile(k)
		}
	}
	return out
}

// QueryProject returns ProjectBuilds for the given multibranch project, with
// invariant 2 applied.
func (r *Registry) QueryProject(projectPath string) []jmodel.ProjectBuild {
	r.mu.RLock()
	out := make([]jmodel.ProjectBuild, 0)
	var needRecon []Key
	for k, rec := range r.records {
		if projectPath == "" || !strings.HasPrefix(rec.JobPath, projectPath+"/") {
			continue
		}
		status, recon := r.displayStatusLocked(k, rec)
		if recon {
			needRecon = append(needRecon, k)
		}
		b := rec.Build
		b.Status = status
		out = append(out, jmodel.ProjectBuild{
			Build:      b,
			BranchName: rec.BranchName,
			BranchPath: rec.JobPath,
		})
	}
	reconcile := r.reconcile
	r.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	if reconcile != nil {
		for _, k := range needRecon {
			reconcile(k)
		}
	}
	return out
}

// HasRunning reports whether any record matching filter is currently visible as Running.
func (r *Registry) HasRunning(filter Filter) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for k, rec := range r.records {
		if !filter.matches(rec) {
			continue
		}
		status, _ := r.displayStatusLocked(k, rec)
		if status == jmodel.BuildStatusRunning {
			return true
		}
	}
	return false
}

// IsTerminal reports whether the build is confirmed complete (a terminal
// status has been observed). Returns false when the build is running, status
// is unconfirmed, or the build is unknown to the registry. Callers use this to
// avoid persisting data (e.g. artifact lists) that is only immutable after the
// build completes.
func (r *Registry) IsTerminal(jobPath string, buildNum int) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.records[Key{JobPath: jobPath, Number: buildNum}]
	return ok && rec.Terminal
}

// RunningCount returns the size of the latest running-set snapshot.
// This is the authoritative live count.
func (r *Registry) RunningCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.liveRunning)
}

// RunningBuilds returns the latest running-set snapshot as UserBuilds. Use this
// where callers previously read store.RunningBuilds.
func (r *Registry) RunningBuilds() []jmodel.UserBuild {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]jmodel.UserBuild, 0, len(r.liveRunning))
	for k := range r.liveRunning {
		if rec, ok := r.records[k]; ok {
			out = append(out, rec.UserBuild(jmodel.BuildStatusRunning))
		}
	}
	return out
}

// SetReconcile installs the reconcile callback (wired post-construction by the
// app once it has a JenkinsClient). Safe to call concurrently with Query/Ingest.
func (r *Registry) SetReconcile(fn ReconcileFn) {
	r.mu.Lock()
	r.reconcile = fn
	r.mu.Unlock()
}

// SetOnChange installs the change-notify callback.
func (r *Registry) SetOnChange(fn ChangeFn) {
	r.mu.Lock()
	r.onChange = fn
	r.mu.Unlock()
}

// Snapshot returns a copy of all records for persistence/inspection.
func (r *Registry) Snapshot() []Record {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshotLocked()
}

func (r *Registry) snapshotLocked() []Record {
	out := make([]Record, 0, len(r.records))
	for _, rec := range r.records {
		out = append(out, rec)
	}
	return out
}
