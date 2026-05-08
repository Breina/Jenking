package command

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRegistryExecute(t *testing.T) {
	r := NewRegistry()
	called := false
	r.Register(Command{
		Name:    "test",
		Aliases: []string{"t"},
		Help:    "test command",
		Execute: func(args []string) tea.Cmd {
			called = true
			return nil
		},
	})

	// Execute by name
	_, err := r.Execute("test")
	if err != nil {
		t.Fatalf("Execute(test) error: %v", err)
	}
	if !called {
		t.Error("command was not called")
	}

	// Execute by alias
	called = false
	_, err = r.Execute("t")
	if err != nil {
		t.Fatalf("Execute(t) error: %v", err)
	}
	if !called {
		t.Error("command was not called via alias")
	}

	// Unknown command
	_, err = r.Execute("unknown")
	if err == nil {
		t.Error("Execute(unknown) expected error, got nil")
	}
}

func TestRegistrySuggestArgs(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{
		Name:    "theme",
		Execute: func(args []string) tea.Cmd { return nil },
		ArgSuggest: func(prefix string) []string {
			all := []string{"default", "dracula", "matrix"}
			var out []string
			for _, a := range all {
				if len(prefix) == 0 || (len(a) > len(prefix) && a[:len(prefix)] == prefix) {
					out = append(out, a)
				}
			}
			return out
		},
	})

	// Command-name completion still works
	got := r.Suggest("th")
	if len(got) != 1 || got[0] != "theme" {
		t.Errorf("Suggest(th) = %v, want [theme]", got)
	}

	// Arg completion: prefix "d" → default, dracula
	got = r.Suggest("theme d")
	if len(got) != 2 || got[0] != "theme default" || got[1] != "theme dracula" {
		t.Errorf("Suggest(theme d) = %v, want [theme default, theme dracula]", got)
	}

	// Arg completion: empty prefix → all
	got = r.Suggest("theme ")
	if len(got) != 3 {
		t.Errorf("Suggest(theme ) = %v, want 3 suggestions", got)
	}

	// No suggestions for unknown command
	got = r.Suggest("unknown ")
	if len(got) != 0 {
		t.Errorf("Suggest(unknown ) = %v, want []", got)
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{
		Name:    "test",
		Aliases: []string{"t"},
		Execute: func(args []string) tea.Cmd { return nil },
	})
	cmds := r.List()
	if len(cmds) != 1 {
		t.Errorf("List() returned %d commands, want 1", len(cmds))
	}
}
