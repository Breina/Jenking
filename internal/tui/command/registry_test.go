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
