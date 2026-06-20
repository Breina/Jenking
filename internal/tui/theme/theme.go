package theme

import "github.com/charmbracelet/lipgloss"

// ThemeID identifies a named theme.
type ThemeID string

// Theme holds all styles used across the TUI.
type Theme struct {
	ID          ThemeID
	Name        string
	Icons       Icons
	Header      HeaderStyles
	Breadcrumb  BreadcrumbStyles
	Search      SearchStyles
	Table       TableStyles
	StatusBar   StatusBarStyles
	BuildStatus BuildStatusStyles
	ProgressBar ProgressBarStyles
	Log         LogStyles
	Stage       StageStyles
	Weather     WeatherStyles
	NavTag      NavTagStyles
	Popup       PopupStyles
	PanelBorder lipgloss.Style // foreground = main panel border colour
	Border      lipgloss.Border
	// Peasant is true when the Royal theme is active without a valid sponsor key.
	// The crown art and "Monarch" label are replaced with their degraded equivalents.
	Peasant bool
}

// Icons holds optional glyph overrides. Zero values use built-in defaults.
// The Matrix theme uses this to replace colored emojis with unicode glyphs.
type Icons struct {
	StatusRunning     string
	StatusSuccess     string
	StatusFailed      string
	StatusAborted     string
	StatusUnstable    string
	StatusSkipped     string
	StatusNotBuilt    string
	StatusPausedInput string
	StatusUnknown     string

	WeatherSun      string
	WeatherUnstable string
	WeatherStorm    string

	TypeFolder      string
	TypePipeline    string
	TypeMultiBranch string
	TypeFreeStyle   string

	Warning string // badge warning icon (default ⚠)
	Error   string // badge error icon (default ✕)
}

type HeaderStyles struct {
	Title        lipgloss.Style
	URL          lipgloss.Style
	Label        lipgloss.Style
	Value        lipgloss.Style
	Connected    lipgloss.Style
	Disconnected lipgloss.Style // muted red — shown when the remote server is unreachable
	RunningBadge lipgloss.Style // amber dot shown next to running build count
	Logo         lipgloss.Style
	Crown        lipgloss.Style // bright yellow crown on the app logo
}

type BreadcrumbStyles struct {
	Segment   lipgloss.Style // legacy — kept for fallback
	Separator lipgloss.Style // legacy — kept for fallback
	Badge     lipgloss.Style
	ViewType  lipgloss.Style // accent(75), bold
	Filter    lipgloss.Style // dim italic — "my", "running" prefix words
	Context   lipgloss.Style // bright(252)
	BuildNum  lipgloss.Style // green(71)
	Paren     lipgloss.Style // dim(245) — also used for / and :
	Count     lipgloss.Style // dim(245)
}

type SearchStyles struct {
	Match        lipgloss.Style
	CurrentMatch lipgloss.Style // the actively-selected match (n/N navigation)
}

type TableStyles struct {
	Header   lipgloss.Style
	Row      lipgloss.Style
	Selected lipgloss.Style
}

type StatusBarStyles struct {
	Bar        lipgloss.Style
	Key        lipgloss.Style
	Help       lipgloss.Style
	Input      lipgloss.Style
	Error      lipgloss.Style
	Command    lipgloss.Style
	Suggestion lipgloss.Style
}

type BuildStatusStyles struct {
	Running     lipgloss.Style
	Success     lipgloss.Style
	Failed      lipgloss.Style
	Aborted     lipgloss.Style
	Unstable    lipgloss.Style
	PausedInput lipgloss.Style // stage waiting for human input
}

type ProgressBarStyles struct {
	Filled      lipgloss.Style // filled portion (blue 74 fg, dark gray bg for fractional blocks)
	Empty       lipgloss.Style // empty portion (dim 240 fg, dark gray bg)
	Overrun     lipgloss.Style // overrun filled portion (amber 179 fg, dark gray bg)
	FilledText  lipgloss.Style // text over filled portion (dark fg, blue bg)
	EmptyText   lipgloss.Style // text over empty portion (bright fg, dark gray bg)
	OverrunText lipgloss.Style // text over overrun filled portion (dark fg, amber bg)
	// Selected variants — brighter colors on selected row background.
	SelFilled      lipgloss.Style
	SelEmpty       lipgloss.Style
	SelOverrun     lipgloss.Style
	SelFilledText  lipgloss.Style
	SelEmptyText   lipgloss.Style
	SelOverrunText lipgloss.Style
}

// LogStyles controls how console/stage log lines are coloured.
type LogStyles struct {
	Normal           lipgloss.Style // regular log line
	Dim              lipgloss.Style // internal/noise lines hidden by default (see InternalLineFn)
	Error            lipgloss.Style // lines matching error/fatal/exception keywords
	Warning          lipgloss.Style // lines matching warning/deprecated keywords
	Trunc            lipgloss.Style // truncation indicator (»)
	CurrentHighlight lipgloss.Style // currently navigated error/warning line (n/N)
}

// StageStyles controls styles specific to the stage list view.
type StageStyles struct {
	GhostDim lipgloss.Style // ghost (predicted-future) stage rows shown during a running build
}

// WeatherStyles controls the weather-icon colours shown in the MAIN column.
type WeatherStyles struct {
	Sun      lipgloss.Style // ☀  success
	Unstable lipgloss.Style // ⛅  unstable
	Storm    lipgloss.Style // ⛈  failed
	None     lipgloss.Style // —   not built / aborted / unknown
}

// NavTagStyles controls the k9s-style navigation tags shown at the bottom.
type NavTagStyles struct {
	Active   lipgloss.Style // current view tag: bright accent bg
	Ancestor lipgloss.Style // ancestor tag in the trail: muted bg
}

// PopupStyles controls colours for modal dialogs, menus, and forms.
type PopupStyles struct {
	Title  lipgloss.Style // bold title text (orange fg)
	Accent lipgloss.Style // selected button (orange bg, black fg)
	Hint   lipgloss.Style // dim hint text
	Label  lipgloss.Style // bright label text
	Desc   lipgloss.Style // dim italic description text
	Normal lipgloss.Style // normal text in popup
}

// Default returns the default color theme.
func Default() Theme {
	green := lipgloss.Color("71")
	dim := lipgloss.Color("245")
	bright := lipgloss.Color("252")
	accent := lipgloss.Color("75")

	return Theme{
		ID:   "default",
		Name: "Default",
		Header: HeaderStyles{
			Title:        lipgloss.NewStyle().Bold(true).Foreground(accent),
			URL:          lipgloss.NewStyle().Foreground(bright),
			Label:        lipgloss.NewStyle().Foreground(dim),
			Value:        lipgloss.NewStyle().Foreground(bright),
			Connected:    lipgloss.NewStyle().Foreground(green),
			Disconnected: lipgloss.NewStyle().Foreground(lipgloss.Color("167")), // muted red
			RunningBadge: lipgloss.NewStyle().Foreground(lipgloss.Color("214")), // amber
			Logo:         lipgloss.NewStyle().Foreground(accent).Bold(true),
			Crown:        lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Bold(true), // bright yellow
		},
		Breadcrumb: BreadcrumbStyles{
			Segment:   lipgloss.NewStyle().Bold(true).Foreground(bright),
			Separator: lipgloss.NewStyle().Foreground(dim),
			Badge:     lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("179")).Bold(true),
			ViewType:  lipgloss.NewStyle().Bold(true).Foreground(accent),
			Filter:    lipgloss.NewStyle().Foreground(dim).Italic(true),
			Context:   lipgloss.NewStyle().Foreground(bright),
			BuildNum:  lipgloss.NewStyle().Foreground(green),
			Paren:     lipgloss.NewStyle().Foreground(dim),
			Count:     lipgloss.NewStyle().Foreground(dim),
		},
		Search: SearchStyles{
			Match:        lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("179")).Bold(true),
			CurrentMatch: lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("220")).Bold(true),
		},
		Table: TableStyles{
			Header:   lipgloss.NewStyle().Bold(true).Foreground(accent).Padding(0, 1),
			Row:      lipgloss.NewStyle().Foreground(bright).Padding(0, 1),
			Selected: lipgloss.NewStyle().Background(lipgloss.Color("62")).Foreground(lipgloss.Color("230")).Bold(true).Padding(0, 1),
		},
		StatusBar: StatusBarStyles{
			Bar:        lipgloss.NewStyle().Foreground(bright),
			Key:        lipgloss.NewStyle().Bold(true).Foreground(accent),
			Help:       lipgloss.NewStyle().Foreground(dim),
			Input:      lipgloss.NewStyle().Foreground(lipgloss.Color("230")),
			Error:      lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true),
			Command:    lipgloss.NewStyle().Foreground(lipgloss.Color("230")),
			Suggestion: lipgloss.NewStyle().Foreground(dim),
		},
		BuildStatus: BuildStatusStyles{
			Running:     lipgloss.NewStyle().Foreground(lipgloss.Color("74")),  // steel blue
			Success:     lipgloss.NewStyle().Foreground(green),                 // muted green
			Failed:      lipgloss.NewStyle().Foreground(lipgloss.Color("167")), // muted red
			Aborted:     lipgloss.NewStyle().Foreground(dim),
			Unstable:    lipgloss.NewStyle().Foreground(lipgloss.Color("179")), // muted amber
			PausedInput: lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true),
		},
		ProgressBar: ProgressBarStyles{
			Filled:      lipgloss.NewStyle().Foreground(lipgloss.Color("74")).Background(lipgloss.Color("238")),
			Empty:       lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Background(lipgloss.Color("238")),
			Overrun:     lipgloss.NewStyle().Foreground(lipgloss.Color("179")).Background(lipgloss.Color("238")),
			FilledText:  lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("74")),
			EmptyText:   lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238")),
			OverrunText: lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("179")),
			// Selected: brighter bar colors on selected row bg (62).
			SelFilled:      lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Background(lipgloss.Color("62")),
			SelEmpty:       lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Background(lipgloss.Color("62")),
			SelOverrun:     lipgloss.NewStyle().Foreground(lipgloss.Color("222")).Background(lipgloss.Color("62")),
			SelFilledText:  lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("111")),
			SelEmptyText:   lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")),
			SelOverrunText: lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("222")),
		},
		Log: LogStyles{
			Normal:           lipgloss.NewStyle().Foreground(bright),
			Dim:              lipgloss.NewStyle().Foreground(dim),
			Error:            lipgloss.NewStyle().Foreground(lipgloss.Color("167")), // muted red
			Warning:          lipgloss.NewStyle().Foreground(lipgloss.Color("179")), // muted amber
			Trunc:            lipgloss.NewStyle().Foreground(lipgloss.Color("240")), // dark gray
			CurrentHighlight: lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("167")).Bold(true),
		},
		Stage: StageStyles{
			GhostDim: lipgloss.NewStyle().Foreground(lipgloss.Color("240")), // dark gray for predicted stages
		},
		Weather: WeatherStyles{
			Sun:      lipgloss.NewStyle().Foreground(lipgloss.Color("220")), // bright yellow
			Unstable: lipgloss.NewStyle().Foreground(lipgloss.Color("214")), // amber/orange
			Storm:    lipgloss.NewStyle().Foreground(lipgloss.Color("69")),  // slate blue
			None:     lipgloss.NewStyle().Foreground(lipgloss.Color("240")), // dim gray
		},
		NavTag: NavTagStyles{
			Active:   lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("75")).Bold(true).Padding(0, 1),
			Ancestor: lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238")).Padding(0, 1),
		},
		Popup: PopupStyles{
			Title:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")),                                 // orange
			Accent: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("214")), // orange bg, black fg
			Hint:   lipgloss.NewStyle().Foreground(dim),                                                              // dim
			Label:  lipgloss.NewStyle().Bold(true).Foreground(bright),                                                // bright
			Desc:   lipgloss.NewStyle().Foreground(dim).Italic(true),                                                 // dim italic
			Normal: lipgloss.NewStyle().Foreground(bright),                                                           // bright
		},
		PanelBorder: lipgloss.NewStyle().Foreground(lipgloss.Color("62")), // purple
		Border:      lipgloss.RoundedBorder(),
	}
}
