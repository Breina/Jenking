package view

import "time"

// timescale is the shared, zoomable window that drives every time-based pane.
type timescale struct {
	levels []time.Duration
	idx    int
}

func newTimescale() timescale {
	levels := []time.Duration{
		15 * time.Minute,
		30 * time.Minute,
		time.Hour,
		2 * time.Hour,
	}
	return timescale{levels: levels, idx: len(levels) - 1} // default 2h
}

func (ts timescale) window() time.Duration { return ts.levels[ts.idx] }

func (ts *timescale) zoomIn() {
	if ts.idx > 0 {
		ts.idx--
	}
}

func (ts *timescale) zoomOut() {
	if ts.idx < len(ts.levels)-1 {
		ts.idx++
	}
}

func (ts timescale) label() string {
	w := ts.window()
	if w >= time.Hour {
		return trimDur(w.Truncate(time.Minute).String())
	}
	return trimDur(w.String())
}

func trimDur(s string) string {
	for _, suf := range []string{"0m0s", "0s"} {
		if len(s) > len(suf) && s[len(s)-len(suf):] == suf {
			s = s[:len(s)-len(suf)]
		}
	}
	return s
}
