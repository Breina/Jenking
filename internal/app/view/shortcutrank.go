package view

// Canonical intra-group ranks for the header shortcut columns. Centralised here
// so every view (and every behavior) produces shortcuts in the same order
// without depending on Add()/append() registration sequence.
//
// Lower renders first within its Group. Optional shortcuts (T tests, A
// artifacts, C copy) get high ranks so they always trail the always-present
// navigation/action keys.
const (
	// View group (Group=GroupView): drill-in nav, build-detail tabs, then
	// optional tests/artifacts at the end.
	rankViewEnter     = 0
	rankViewFullLog   = 10  // "l"
	rankViewStages    = 20  // "s"
	rankViewDescribe  = 30  // "d"
	rankViewAllBuilds = 40  // "b"
	rankViewTests     = 90  // "T" — optional, conditional on test report present
	rankViewArtifacts = 100 // "A" — optional, conditional on artifacts present

	// Action group (Group=GroupAction): always-present actions first, then
	// optional/contextual.
	rankActionTrigger = 10 // "t"
	rankActionCancel  = 20 // "x"
	rankActionCopy    = 90 // "C" — optional, only when a selection exists
)
