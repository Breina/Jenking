package app

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Breina/Jenking/internal/app/view"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/config"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/theme"
)

// buildCommandRegistry constructs the slash-command registry used by App.
// All commands emit a tea.Msg that App.Update consumes; no command mutates
// App state directly. The two parameters carry the per-Jenkins-context data
// that the suggestion closures need (project list + named contexts).
//
// Suggestion closures capture `store` and `contexts` at registration time.
// After a :context switch the App's store reference is reassigned but the
// captured one is not — suggestions stay tied to the original Jenkins
// instance until the process restarts. This is the same caveat the inline
// version had before extraction.
func buildCommandRegistry(store *cache.Store, contexts []config.ContextConfig) *command.Registry {
	r := command.NewRegistry()
	projectSuggest := func(prefix string) []string {
		return view.TargetArgSuggest(store, prefix)
	}

	r.Register(command.Command{
		Name: "quit", Aliases: []string{"q"}, Help: "Exit application",
		Execute: func(args []string) tea.Cmd { return tea.Quit },
	})
	r.Register(command.Command{
		Name: "colorblind", Aliases: []string{"cb"},
		Help:       "Select colorblindness compensation type",
		Execute:    executeColorblind,
		ArgSuggest: prefixMatchSuggest(colorblindnessTypeNames()),
	})
	r.Register(command.Command{
		Name: "builds", Aliases: []string{"b", "build"},
		Help:       "Show builds [<project> [<branch>]]",
		Execute:    openTargetCmd(kindBuilds),
		ArgSuggest: projectSuggest,
	})
	r.Register(command.Command{
		Name: "stages", Aliases: []string{"s", "stage"},
		Help:       "Show stages [<project> [<branch>] [#<n>|#last]]",
		Execute:    openTargetCmd(kindStages),
		ArgSuggest: projectSuggest,
	})
	r.Register(command.Command{
		Name: "jobs", Aliases: []string{"j", "job"},
		Help:       "Navigate to job list [<project>]",
		Execute:    openTargetCmd(kindJobs),
		ArgSuggest: projectSuggest,
	})
	r.Register(command.Command{
		Name: "log", Aliases: []string{"l", "logs"},
		Help:       "Show console log [<project> [<branch>] [#<n>|#last]]",
		Execute:    openTargetCmd(kindLogs),
		ArgSuggest: projectSuggest,
	})
	r.Register(command.Command{
		Name: "running", Aliases: []string{"r"}, Help: "Show running builds",
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return openRunningBuildsMsg{} }
		},
	})
	r.Register(command.Command{
		Name: "matrix", Help: "The Matrix has you...", Hidden: true,
		Execute: openTargetCmd(kindMatrix),
	})
	r.Register(command.Command{
		Name: "theme", Aliases: []string{"th"}, Help: "Select colour theme",
		Execute:    executeTheme,
		ArgSuggest: prefixMatchSuggest(themeIDNames()),
	})
	r.Register(command.Command{
		Name: "help", Help: "Show available commands",
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return openHelpMsg{} }
		},
	})
	r.Register(command.Command{
		Name: "config", Aliases: []string{"preferences", "prefs"},
		Help: "Edit preferences (notifications, git usernames, refresh interval)",
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return openPrefsMsg{} }
		},
	})
	r.Register(command.Command{
		Name: "context", Aliases: []string{"ctx"},
		Help:       "Manage Jenkins contexts (switch, add, delete)",
		Execute:    executeContext,
		ArgSuggest: prefixMatchSuggest(contextNames(contexts)),
	})
	r.Register(command.Command{
		Name: "update", Aliases: []string{"upgrade"},
		Help: "Update Jenking to the latest release",
		Execute: func(args []string) tea.Cmd {
			return func() tea.Msg { return startUpdateMsg{} }
		},
	})
	return r
}

func executeColorblind(args []string) tea.Cmd {
	if len(args) == 0 {
		return func() tea.Msg { return openColorblindMenuMsg{} }
	}
	cbType := theme.ColorblindnessType(args[0])
	for _, t := range theme.AllColorblindnessTypes {
		if t == cbType {
			return func() tea.Msg { return view.ColorblindConfirmMsg{Type: cbType} }
		}
	}
	return func() tea.Msg {
		return view.ErrorMsg{Err: fmt.Errorf("unknown colorblindness type: %s", args[0])}
	}
}

func executeTheme(args []string) tea.Cmd {
	if len(args) == 0 {
		return func() tea.Msg { return openThemeMenuMsg{} }
	}
	id := theme.ThemeID(args[0])
	for _, t := range theme.AllThemes {
		if t.ID == id {
			return func() tea.Msg { return view.ThemeConfirmMsg{ID: id} }
		}
	}
	return func() tea.Msg { return view.ErrorMsg{Err: fmt.Errorf("unknown theme: %s", args[0])} }
}

func executeContext(args []string) tea.Cmd {
	if len(args) == 0 {
		return func() tea.Msg { return openContextMenuMsg{} }
	}
	return func() tea.Msg { return switchContextMsg{name: args[0]} }
}

func colorblindnessTypeNames() []string {
	out := make([]string, 0, len(theme.AllColorblindnessTypes))
	for _, t := range theme.AllColorblindnessTypes {
		out = append(out, string(t))
	}
	return out
}

func themeIDNames() []string {
	out := make([]string, 0, len(theme.AllThemes))
	for _, t := range theme.AllThemes {
		out = append(out, string(t.ID))
	}
	return out
}

func contextNames(contexts []config.ContextConfig) []string {
	out := make([]string, 0, len(contexts))
	for _, c := range contexts {
		out = append(out, c.Name)
	}
	return out
}

// prefixMatchSuggest returns the standard ArgSuggest closure: case-sensitive
// prefix match against a fixed name list, excluding exact matches.
func prefixMatchSuggest(names []string) func(prefix string) []string {
	return func(prefix string) []string {
		var result []string
		for _, n := range names {
			if strings.HasPrefix(n, prefix) && n != prefix {
				result = append(result, n)
			}
		}
		sort.Strings(result)
		return result
	}
}
