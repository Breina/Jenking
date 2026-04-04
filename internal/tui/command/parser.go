package command

import "strings"

// Parsed represents a parsed command input.
type Parsed struct {
	Name string
	Args []string
}

// Parse splits a command string like "build param1=val1" into name and args.
func Parse(input string) Parsed {
	input = strings.TrimSpace(input)
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return Parsed{}
	}
	return Parsed{
		Name: parts[0],
		Args: parts[1:],
	}
}
