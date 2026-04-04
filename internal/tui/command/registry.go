package command

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// Command represents a registered command.
type Command struct {
	Name    string
	Aliases []string
	Help    string
	Hidden  bool // true = omitted from :help (easter egg commands)
	Execute func(args []string) tea.Cmd
}

// Registry holds all registered commands.
type Registry struct {
	commands map[string]*Command
}

// NewRegistry creates an empty command registry.
func NewRegistry() *Registry {
	return &Registry{commands: make(map[string]*Command)}
}

// Register adds a command to the registry.
func (r *Registry) Register(cmd Command) {
	r.commands[cmd.Name] = &cmd
	for _, alias := range cmd.Aliases {
		r.commands[alias] = &cmd
	}
}

// Execute looks up and runs a command by name.
func (r *Registry) Execute(input string) (tea.Cmd, error) {
	parsed := Parse(input)
	if parsed.Name == "" {
		return nil, nil
	}
	cmd, ok := r.commands[parsed.Name]
	if !ok {
		return nil, fmt.Errorf("unknown command: %s", parsed.Name)
	}
	return cmd.Execute(parsed.Args), nil
}

// List returns all unique commands (no alias duplicates).
func (r *Registry) List() []Command {
	seen := make(map[string]bool)
	var result []Command
	for _, cmd := range r.commands {
		if !seen[cmd.Name] {
			seen[cmd.Name] = true
			result = append(result, *cmd)
		}
	}
	return result
}

// ListVisible returns all unique non-hidden commands.
func (r *Registry) ListVisible() []Command {
	seen := make(map[string]bool)
	var result []Command
	for _, cmd := range r.commands {
		if !seen[cmd.Name] && !cmd.Hidden {
			seen[cmd.Name] = true
			result = append(result, *cmd)
		}
	}
	return result
}
