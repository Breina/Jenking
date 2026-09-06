package buildregistry

import "sort"

// MergeRecords unions two persisted record sets, resolving key collisions so the
// result never regresses a build's state. It is pure (no clock, no lock) so it
// can run inside a cross-process file lock during a read-modify-write cycle when
// several Jenking processes share one cache directory.
//
// Collision policy, honoring the registry's terminal-is-sticky invariant:
//   - If exactly one side is Terminal, the terminal record wins — a completed
//     build must never be reverted to Running by another process's stale view.
//   - Otherwise (both terminal, or both non-terminal) the record with the newer
//     UpdatedAt wins; ties keep the first argument.
//
// The winner additionally adopts the later LastSeenRunning of the two, so a
// live-running confirmation from either process is preserved.
func MergeRecords(a, b []Record) []Record {
	merged := make(map[Key]Record, len(a)+len(b))
	for _, rec := range a {
		merged[keyOfRecord(rec)] = rec
	}
	for _, rec := range b {
		k := keyOfRecord(rec)
		cur, exists := merged[k]
		if !exists {
			merged[k] = rec
			continue
		}
		merged[k] = pickWinner(cur, rec)
	}

	out := make([]Record, 0, len(merged))
	for _, rec := range merged {
		out = append(out, rec)
	}
	return boundPerJob(out)
}

// boundPerJob enforces the same retainPerJob cap the in-memory registry applies,
// so the persisted set does not accumulate unbounded across save/merge cycles
// (the merge would otherwise keep every historical record forever). For each job
// it keeps the retainPerJob highest build numbers; any non-terminal (e.g. still
// running) record beyond the cap is also kept so a live build is never dropped.
func boundPerJob(recs []Record) []Record {
	byJob := make(map[string][]Record)
	for _, r := range recs {
		byJob[r.JobPath] = append(byJob[r.JobPath], r)
	}
	out := make([]Record, 0, len(recs))
	for _, group := range byJob {
		if len(group) <= retainPerJob {
			out = append(out, group...)
			continue
		}
		sort.Slice(group, func(i, j int) bool {
			return group[i].Build.Number > group[j].Build.Number
		})
		for i, r := range group {
			if i < retainPerJob || !r.Terminal {
				out = append(out, r)
			}
		}
	}
	return out
}

func keyOfRecord(r Record) Key {
	return Key{JobPath: r.JobPath, Number: r.Build.Number}
}

// pickWinner resolves a collision between two records for the same key.
func pickWinner(a, b Record) Record {
	var win Record
	switch {
	case a.Terminal && !b.Terminal:
		win = a
	case b.Terminal && !a.Terminal:
		win = b
	case b.UpdatedAt.After(a.UpdatedAt):
		win = b
	default:
		win = a
	}
	// Preserve the most recent running confirmation regardless of which side won.
	if a.LastSeenRunning.After(win.LastSeenRunning) {
		win.LastSeenRunning = a.LastSeenRunning
	}
	if b.LastSeenRunning.After(win.LastSeenRunning) {
		win.LastSeenRunning = b.LastSeenRunning
	}
	return win
}
