package view

import "testing"

func TestClassifyGutter(t *testing.T) {
	// gw=4 corresponds to a 3-digit line count (e.g. up to 999) plus one space.
	const gw = 4
	cases := []struct {
		name string
		in   string
		want gutterKind
	}{
		{"numbered row", "  5 def foo()", gutterNumbered},
		{"max-width number", "123 pipeline {", gutterNumbered},
		{"indented numbered row", " 12     return x", gutterNumbered},
		{"blank gutter", "    more", gutterContinuation},
		{"partial mid-line selection", "pipeline {", gutterNone},
		{"digit-then-space is not a gutter", "1 2 x", gutterNone},
		{"shorter than gutter", "ab", gutterNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyGutter(c.in, gw); got != c.want {
				t.Errorf("classifyGutter(%q, %d) = %v, want %v", c.in, gw, got, c.want)
			}
		})
	}
}

func TestStripSelectionGutter_NoWrap(t *testing.T) {
	dv := &DescribeView{}
	dv.lineCount = 280 // 3 digits -> gw=4
	in := " 24             name: 'INPUT_VARIANT',\n 25         choices: ['none'],\n 27         )\n 28     }"
	// Line numbers gone, source indentation kept, one line per source line.
	want := "            name: 'INPUT_VARIANT',\n        choices: ['none'],\n        )\n    }"
	if got := dv.stripSelectionGutter(in); got != want {
		t.Errorf("stripSelectionGutter =\n%q\nwant\n%q", got, want)
	}
}

func TestStripSelectionGutter_WrapRejoinsChunks(t *testing.T) {
	dv := &DescribeView{}
	dv.lineCount = 280 // gw=4
	dv.scriptLV.SetSize(40, 20)
	dv.scriptLV.ToggleWrap() // wrap on
	// A partial first line, then a numbered row followed by two blank-gutter
	// continuation rows that must rejoin onto it.
	in := "choice(\n" +
		" 26             description: 'Which interac\n" +
		"    tive input step variant to run during\n" +
		"     the Approve stage'"
	want := "choice(\n" +
		"            description: 'Which interactive input step variant to run during the Approve stage'"
	if got := dv.stripSelectionGutter(in); got != want {
		t.Errorf("stripSelectionGutter =\n%q\nwant\n%q", got, want)
	}
}

func TestStripSelectionGutter_NoLines(t *testing.T) {
	dv := &DescribeView{}
	dv.lineCount = 0
	in := " 24 x"
	if got := dv.stripSelectionGutter(in); got != in {
		t.Errorf("with no lines, got %q, want unchanged", got)
	}
}
