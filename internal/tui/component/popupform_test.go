package component

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/tui/theme"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "ctrl+t":
		return tea.KeyMsg{Type: tea.KeyCtrlT}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestWrapText(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		{"empty", "", 10, []string{""}},
		{"single short", "hello", 10, []string{"hello"}},
		{"wraps on space", "hello world goodbye", 11, []string{"hello world", "goodbye"}},
		{"hard breaks long word", "supercalifragilistic", 5, []string{"super", "calif", "ragil", "istic"}},
		{"preserves newlines", "line one\nline two", 20, []string{"line one", "line two"}},
		{"width zero returns input", "anything", 0, []string{"anything"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := wrapText(c.in, c.width)
			if len(got) != len(c.want) {
				t.Fatalf("len mismatch: got %d %q, want %d %q", len(got), got, len(c.want), c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("line %d: got %q, want %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestRequiredFieldBlocksSubmit(t *testing.T) {
	pf := NewPopupForm(theme.Default(), "Test", []Field{
		{Key: "name", Label: "Name", Kind: FieldText, Required: true},
		{Key: "url", Label: "URL", Kind: FieldText, Required: true},
	})
	r := pf.Update(keyMsg("enter"))
	if r.Status != PopupActive {
		t.Fatalf("expected PopupActive when required field empty, got %v", r.Status)
	}
	if pf.fieldErrors[0] == "" || pf.fieldErrors[1] == "" {
		t.Fatalf("expected per-field errors on both invalid fields, got %v", pf.fieldErrors)
	}
	if pf.cursor != 0 {
		t.Fatalf("expected cursor to focus first invalid field, got %d", pf.cursor)
	}
}

func TestAllValidationErrorsShownAtOnce(t *testing.T) {
	pf := NewPopupForm(theme.Default(), "Test", []Field{
		{Key: "a", Label: "A", Kind: FieldText, Required: true},
		{Key: "b", Label: "B", Kind: FieldText, Default: "x",
			Validator: func(s string) error { return errors.New("bad-b") }},
		{Key: "c", Label: "C", Kind: FieldText, Required: true},
	})
	pf.Update(keyMsg("enter"))
	if pf.fieldErrors[0] == "" {
		t.Errorf("expected error on field 0")
	}
	if !strings.Contains(pf.fieldErrors[1], "bad-b") {
		t.Errorf("expected validator error on field 1, got %q", pf.fieldErrors[1])
	}
	if pf.fieldErrors[2] == "" {
		t.Errorf("expected error on field 2")
	}
}

func TestValidatorBlocksSubmit(t *testing.T) {
	pf := NewPopupForm(theme.Default(), "Test", []Field{
		{Key: "n", Label: "N", Kind: FieldText, Default: "abc",
			Validator: func(s string) error { return errors.New("bad") }},
	})
	r := pf.Update(keyMsg("enter"))
	if r.Status != PopupActive {
		t.Fatalf("expected PopupActive, got %v", r.Status)
	}
	if !strings.Contains(pf.fieldErrors[0], "bad") {
		t.Fatalf("expected validator error on field 0, got %q", pf.fieldErrors[0])
	}
}

func TestSubmitClean(t *testing.T) {
	pf := NewPopupForm(theme.Default(), "Test", []Field{
		{Key: "n", Label: "N", Kind: FieldText, Default: "ok", Required: true},
	})
	r := pf.Update(keyMsg("enter"))
	if r.Status != PopupSubmitted {
		t.Fatalf("expected PopupSubmitted, got %v", r.Status)
	}
	if got := pf.Values()["n"]; got != "ok" {
		t.Fatalf("expected value 'ok', got %q", got)
	}
}

func TestCustomKeyDispatch(t *testing.T) {
	pf := NewPopupForm(theme.Default(), "Test", []Field{
		{Key: "n", Label: "N", Kind: FieldText},
	})
	pf.RegisterCustomKey("ctrl+t", "test", "test connection")
	r := pf.Update(keyMsg("ctrl+t"))
	if r.Status != PopupCustom || r.Custom != "test" {
		t.Fatalf("expected PopupCustom/test, got %v/%q", r.Status, r.Custom)
	}
}

func TestChoiceCycling(t *testing.T) {
	pf := NewPopupForm(theme.Default(), "Test", []Field{
		{Key: "c", Label: "C", Kind: FieldChoice, Choices: []string{"a", "b", "c"}, Default: "a"},
	})
	pf.Update(keyMsg("right"))
	if got := pf.Values()["c"]; got != "b" {
		t.Fatalf("right should cycle to b, got %q", got)
	}
	pf.Update(keyMsg("right"))
	pf.Update(keyMsg("right")) // capped at last
	if got := pf.Values()["c"]; got != "c" {
		t.Fatalf("right cycling capped should be c, got %q", got)
	}
	pf.Update(keyMsg("left"))
	if got := pf.Values()["c"]; got != "b" {
		t.Fatalf("left should cycle back to b, got %q", got)
	}
}

func TestBoolToggle(t *testing.T) {
	pf := NewPopupForm(theme.Default(), "Test", []Field{
		{Key: "b", Label: "B", Kind: FieldBool, Default: "false"},
	})
	pf.Update(keyMsg(" "))
	if got := pf.Values()["b"]; got != "true" {
		t.Fatalf("space should toggle to true, got %q", got)
	}
	pf.Update(keyMsg(" "))
	if got := pf.Values()["b"]; got != "false" {
		t.Fatalf("space should toggle back to false, got %q", got)
	}
}

func TestCancel(t *testing.T) {
	pf := NewPopupForm(theme.Default(), "Test", []Field{{Key: "n", Label: "N", Kind: FieldText}})
	r := pf.Update(keyMsg("esc"))
	if r.Status != PopupCancelled {
		t.Fatalf("expected PopupCancelled, got %v", r.Status)
	}
}

func TestNavigation(t *testing.T) {
	pf := NewPopupForm(theme.Default(), "Test", []Field{
		{Key: "a", Label: "A", Kind: FieldText},
		{Key: "b", Label: "B", Kind: FieldText},
		{Key: "c", Label: "C", Kind: FieldText},
	})
	pf.Update(keyMsg("down"))
	if pf.Cursor() != 1 {
		t.Fatalf("down: expected cursor 1, got %d", pf.Cursor())
	}
	pf.Update(keyMsg("tab"))
	if pf.Cursor() != 2 {
		t.Fatalf("tab: expected cursor 2, got %d", pf.Cursor())
	}
	pf.Update(keyMsg("down")) // should clamp at last
	if pf.Cursor() != 2 {
		t.Fatalf("down at end: expected cursor 2, got %d", pf.Cursor())
	}
	pf.Update(keyMsg("up"))
	if pf.Cursor() != 1 {
		t.Fatalf("up: expected cursor 1, got %d", pf.Cursor())
	}
}

func TestFocusKey(t *testing.T) {
	pf := NewPopupForm(theme.Default(), "Test", []Field{
		{Key: "a", Label: "A", Kind: FieldText},
		{Key: "b", Label: "B", Kind: FieldText},
	})
	pf.FocusKey("b")
	if pf.Cursor() != 1 {
		t.Fatalf("FocusKey: expected cursor 1, got %d", pf.Cursor())
	}
}

func TestViewRendersTitleFieldsAndFooter(t *testing.T) {
	pf := NewPopupForm(theme.Default(), "Add Context", []Field{
		{Key: "name", Label: "Name", Kind: FieldText, Required: true,
			Description: "A short identifier for this Jenkins instance."},
		{Key: "url", Label: "URL", Kind: FieldText, Required: true,
			Description: "Full Jenkins base URL including scheme."},
		{Key: "insecure", Label: "skip TLS verify", Kind: FieldBool},
	})
	pf.RegisterCustomKey("ctrl+t", "test", "test connection")
	pf.SetSize(100, 30)
	out := pf.View()

	for _, want := range []string{
		"Add Context",
		"Name *",
		"URL *",
		"skip TLS verify",
		"<↑↓>",
		"navigate",
		"<enter>",
		"accept",
		"<esc>",
		"cancel",
		"<ctrl+t>",
		"test connection",
	} {
		if !strings.Contains(stripANSI(out), want) {
			t.Errorf("View() missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestViewWrapsLongDescription(t *testing.T) {
	long := "This is a very long description that needs to be wrapped across multiple lines so the popup box doesn't break when the field has lots of helpful text."
	pf := NewPopupForm(theme.Default(), "T", []Field{
		{Key: "n", Label: "N", Kind: FieldText, Description: long},
	})
	pf.SetSize(60, 30) // contentW = 52
	out := pf.View()
	for _, line := range strings.Split(out, "\n") {
		stripped := stripANSI(line)
		runeLen := len([]rune(stripped))
		// content 52 + padding 6 + border 2 = 60. Allow a couple cols slack
		// for any trailing space lipgloss adds.
		if runeLen > 62 {
			t.Errorf("rendered line too long (%d runes): %q", runeLen, stripped)
		}
	}
	// Also make sure the description actually wrapped (multiple lines containing words).
	if !strings.Contains(out, "wrapped") || !strings.Contains(out, "multiple") {
		t.Errorf("expected description text in output:\n%s", out)
	}
}

func stripANSI(s string) string {
	var out strings.Builder
	inEsc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inEsc {
			if c == 'm' {
				inEsc = false
			}
			continue
		}
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			inEsc = true
			i++
			continue
		}
		out.WriteByte(c)
	}
	return out.String()
}

func TestSetSizeClamp(t *testing.T) {
	pf := NewPopupForm(theme.Default(), "T", []Field{{Key: "a", Label: "A", Kind: FieldText}})
	pf.SetSize(40, 30)
	if pf.ContentWidth() != 50 {
		t.Errorf("tiny terminal: expected min 50, got %d", pf.ContentWidth())
	}
	pf.SetSize(200, 30)
	if pf.ContentWidth() != 80 {
		t.Errorf("huge terminal: expected max 80, got %d", pf.ContentWidth())
	}
	pf.SetSize(70, 30)
	if pf.ContentWidth() != 62 {
		t.Errorf("70-col terminal: expected 62, got %d", pf.ContentWidth())
	}
}
