package command

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Command represents a registered command.
type Command struct {
	Name       string
	Aliases    []string
	Help       string
	Hidden     bool // true = omitted from :help (easter egg commands)
	Execute    func(args []string) tea.Cmd
	ArgSuggest func(prefix string) []string // optional: returns arg completions for given prefix
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

// Suggest returns completions for the given input.
// If the input contains a space, it completes arguments for the named command.
// Otherwise it completes command names and aliases, sorted alphabetically.
func (r *Registry) Suggest(prefix string) []string {
	if prefix == "" {
		return nil
	}
	if spaceIdx := strings.Index(prefix, " "); spaceIdx != -1 {
		cmdName := prefix[:spaceIdx]
		argPrefix := prefix[spaceIdx+1:]
		cmd, ok := r.commands[cmdName]
		if !ok || cmd.ArgSuggest == nil {
			return nil
		}
		args := cmd.ArgSuggest(argPrefix)
		// Preserve the ArgSuggest function's ordering (e.g. #last first,
		// then build numbers descending). ArgSuggest implementations are
		// responsible for sorting their own results if alphabetical order
		// is desired.
		result := make([]string, 0, len(args))
		for _, a := range args {
			result = append(result, cmdName+" "+a)
		}
		return result
	}
	var result []string
	for key, cmd := range r.commands {
		if cmd.Hidden {
			continue
		}
		if strings.HasPrefix(key, prefix) && key != prefix {
			result = append(result, key)
		}
	}
	sort.Strings(result)
	return result
}
