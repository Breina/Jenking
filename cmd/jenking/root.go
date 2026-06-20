package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Breina/Jenking/internal/app"
	"github.com/Breina/Jenking/internal/app/view"
	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/config"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/jenkins"
	"github.com/Breina/Jenking/internal/logging"
	"github.com/Breina/Jenking/internal/navmsg"
	"github.com/Breina/Jenking/internal/tui/command"
	"github.com/Breina/Jenking/internal/tui/component"
	"github.com/Breina/Jenking/internal/tui/theme"
	"github.com/Breina/Jenking/internal/tui/widget"
	"github.com/Breina/Jenking/internal/version"
)

// cmdState holds wired-up dependencies shared by all subcommands.
type cmdState struct {
	cfg    *config.Manager
	client jmodel.JenkinsClient
	store  *cache.Store
}

var (
	cs          cmdState
	outputFlag  string
	timeoutFlag time.Duration
	contextFlag string
)

func execute() error {
	return rootCmd.Execute()
}

var rootCmd = &cobra.Command{
	Use:   "jenking",
	Short: "Jenkins TUI and CLI",
	Long: `jenking manages Jenkins jobs, builds, and pipelines from the terminal.

Run without arguments to open the interactive TUI. Use subcommands for
scriptable, non-interactive access to Jenkins data.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUIAt("", nil)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&outputFlag, "output", "o", "text", "Output format: text, json, yaml")
	rootCmd.PersistentFlags().DurationVar(&timeoutFlag, "timeout", 60*time.Second, "Timeout for Jenkins API calls")
	rootCmd.PersistentFlags().StringVarP(&contextFlag, "context", "c", "", "Jenkins context to use (overrides current_context)")
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Completion and version don't talk to Jenkins.
		if cmd.Name() == "__complete" || cmd.Name() == "completion" || cmd.Name() == "version" {
			return nil
		}
		return setupCmdState()
	}

	rootCmd.AddCommand(
		newJobsCmd(),
		newBuildsCmd(),
		newStagesCmd(),
		newRunningCmd(),
		newQueueCmd(),
		newWhoAmICmd(),
		newParamsCmd(),
		newMetadataCmd(),
		newArtifactsCmd(),
		newArtifactCmd(),
		newLogsCmd(),
		newDescribeCmd(),
		newTestsCmd(),
		newTriggerCmd(),
		newCancelCmd(),
		newDequeueCmd(),
		newUICmd(),
		newVersionCmd(),
	)

}

func setupCmdState() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if _, err := logging.Setup(logging.ParseLevel(cfg.Preferences.LogLevel)); err != nil {
		return fmt.Errorf("setting up logging: %w", err)
	}
	if contextFlag != "" {
		cfg.CurrentContext = contextFlag // in-memory override; not persisted
	}
	active := cfg.ActiveContext()
	client := jenkins.NewClient(active.URL, active.Username, active.Token, active.Insecure)
	disk := newDiskStore(active.URL)
	store := cache.NewStore(disk)
	cs = cmdState{cfg: cfg, client: client, store: store}
	return nil
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print jenking version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.App)
		},
	}
}

// runTUIAt launches the TUI. If verb is non-empty, the TUI opens at the
// deep-linked view; otherwise it opens on the dashboard.
func runTUIAt(verb string, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	user, err := cs.client.WhoAmI(ctx)
	if err != nil {
		active := cs.cfg.ActiveContext()
		return fmt.Errorf("connecting to Jenkins at %s (user: %s): %w", active.URL, active.Username, err)
	}

	themeID := theme.ThemeID(cs.cfg.Preferences.Theme)
	if themeID == "" {
		themeID = theme.ThemeDefault
	}
	sponsorKey := cs.cfg.Preferences.SponsorKey
	baseTheme := theme.ByID(themeID)
	if themeID == theme.ThemeRoyal && !theme.IsSponsor(user.ID, sponsorKey) {
		baseTheme.Peasant = true
	}
	cbType := theme.ColorblindnessType(cs.cfg.Preferences.ColorblindnessType)
	if cbType == "" {
		cbType = theme.ColorblindnessNone
	}
	activeTheme := theme.ApplyColorblindFilter(baseTheme, cbType)

	widget.SetVimPolicy(widget.VimPolicy{
		Enabled:         cs.cfg.Preferences.VimIntegration.Enabled,
		PrefetchSymbols: cs.cfg.Preferences.VimIntegration.PrefetchSymbols,
		ValidateOnSave:  cs.cfg.Preferences.VimIntegration.ValidateOnSave,
	})
	// Seed the config file with default artifact extensions when the key is
	// absent, so it's discoverable and editable in config.yaml.
	if len(cs.cfg.Preferences.TextArtifactExtensions) == 0 {
		cs.cfg.Preferences.TextArtifactExtensions = view.DefaultTextArtifactExtensions()
		if err := cs.cfg.SetTextArtifactExtensions(cs.cfg.Preferences.TextArtifactExtensions); err != nil {
			slog.Warn("could not persist default text_artifact_extensions", "err", err)
		}
	}
	view.SetTextArtifactExtensions(cs.cfg.Preferences.TextArtifactExtensions)

	keys := app.DefaultKeyMap()
	debug := logging.ParseLevel(cs.cfg.Preferences.LogLevel) == logging.LevelDebug
	active := cs.cfg.ActiveContext()
	header := component.NewHeader(activeTheme, active.URL, user.FullName, user.JenkinsVersion, version.App, debug)
	breadcrumb := component.NewBreadcrumb(activeTheme)
	statusBar := component.NewStatusBar(activeTheme)

	var initialView view.View = view.NewJobList(activeTheme, cs.client, cs.store, "", "Dashboard", false, user.ID, cs.cfg.Preferences.GitUsernames)
	if verb != "" {
		initialView, err = buildDeepLinkView(verb, args, deepLinkArgs{
			theme:        activeTheme,
			client:       cs.client,
			store:        cs.store,
			username:     user.ID,
			friendlyName: user.FullName,
			gitUsernames: cs.cfg.Preferences.GitUsernames,
			slowInterval: cs.cfg.Preferences.SlowRefreshInterval,
		})
		if err != nil {
			return fmt.Errorf("jenking ui: %w", err)
		}
	}

	model := app.NewApp(buildAppConfig(cs.cfg, active.Name, activeTheme, baseTheme, themeID, cbType,
		keys, cs.client, cs.store, user, debug, sponsorKey, header, breadcrumb, statusBar, initialView))

	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}
	if a, ok := finalModel.(app.App); ok && a.UpdatedTo != "" {
		fmt.Printf("Jenking updated to %s — please restart to use the new version.\n", a.UpdatedTo)
	}
	return nil
}

// ctxWithTimeout returns a context with the configured CLI timeout.
func ctxWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeoutFlag)
}

// withProjectBuild resolves a project+build from CLI args and calls fn with
// the resulting context, job path, and build number.
func withProjectBuild(args []string, fn func(ctx context.Context, jobPath string, buildNum int) error) error {
	target, err := command.ParseTarget(args)
	if err != nil {
		return err
	}
	nc, err := navmsg.ResolveTarget(target, cs.store, navmsg.NavigationContext{})
	if err != nil {
		return writeError(err)
	}
	if nc.ProjectName == "" {
		return fmt.Errorf("project required")
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	jobPath := nc.JobPath()
	buildNum, err := resolveBuildNum(ctx, cs.client, jobPath, nc.Build)
	if err != nil {
		return writeError(enrichBranchNotFound(ctx, nc, err))
	}
	return fn(ctx, jobPath, buildNum)
}

// enrichBranchNotFound turns a 404 from a branch-level call into a message that
// names the missing branch and lists the project's available branches. Any
// non-404 error (or a failure to look up branches) is returned unchanged.
func enrichBranchNotFound(ctx context.Context, nc navmsg.NavigationContext, err error) error {
	var he *jenkins.HTTPError
	if err == nil || nc.BranchName == "" || !errors.As(err, &he) || he.StatusCode != http.StatusNotFound {
		return err
	}

	projectPath := nc.FolderPath
	if nc.ProjectName != "" {
		if projectPath != "" {
			projectPath += "/"
		}
		projectPath += nc.ProjectName
	}
	pbuilds, perr := cs.client.ListProjectBuilds(ctx, projectPath)
	if perr != nil {
		return err
	}

	seen := map[string]bool{}
	var branches []string
	for _, b := range pbuilds {
		name := navmsg.DecodeName(b.BranchName)
		if name != "" && !seen[name] {
			seen[name] = true
			branches = append(branches, name)
		}
	}
	sort.Strings(branches)

	wanted := navmsg.DecodeName(nc.BranchName)
	project := navmsg.DecodePath(projectPath)
	if len(branches) == 0 {
		return fmt.Errorf("branch %q not found in %s", wanted, project)
	}
	return fmt.Errorf("branch %q not found in %s; available branches: %s", wanted, project, strings.Join(branches, ", "))
}

// resolveBuildNum resolves a NavBuildRef to a concrete build number.
// Calls ListBuilds to find the latest if ref carries no explicit number.
func resolveBuildNum(ctx context.Context, client jmodel.JenkinsClient, jobPath string, ref navmsg.NavBuildRef) (int, error) {
	if ref.Number > 0 {
		return ref.Number, nil
	}
	builds, err := client.ListBuilds(ctx, jobPath)
	if err != nil {
		return 0, fmt.Errorf("listing builds for %s: %w", jobPath, err)
	}
	if len(builds) == 0 {
		return 0, fmt.Errorf("no builds found for %s (multibranch projects require a branch)", jobPath)
	}
	return builds[0].Number, nil
}

// writeError writes {"error":"..."} to os.Stdout when JSON/YAML output is
// requested, so agents always have a parseable response.
func writeError(err error) error {
	switch outputFlag {
	case "json":
		_ = printJSON(os.Stdout, map[string]string{"error": err.Error()})
	case "yaml":
		_ = printYAML(os.Stdout, map[string]string{"error": err.Error()})
	}
	return err
}
