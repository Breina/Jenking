package theme

import "github.com/charmbracelet/lipgloss"

// Theme holds all styles used across the TUI.
type Theme struct {
	Header      HeaderStyles
	Breadcrumb  BreadcrumbStyles
	Search      SearchStyles
	Table       TableStyles
	StatusBar   StatusBarStyles
	BuildStatus BuildStatusStyles
	ProgressBar ProgressBarStyles
	Border      lipgloss.Border
}

type HeaderStyles struct {
	Title     lipgloss.Style
	URL       lipgloss.Style
	Label     lipgloss.Style
	Value     lipgloss.Style
	Connected lipgloss.Style
	Logo      lipgloss.Style
}

type BreadcrumbStyles struct {
	Segment   lipgloss.Style // legacy — kept for fallback
	Separator lipgloss.Style // legacy — kept for fallback
	Badge     lipgloss.Style
	ViewType  lipgloss.Style // accent(75), bold
	Context   lipgloss.Style // bright(252)
	BuildNum  lipgloss.Style // green(71)
	Paren     lipgloss.Style // dim(245) — also used for / and :
	Count     lipgloss.Style // dim(245)
}

type SearchStyles struct {
	Match lipgloss.Style
}

type TableStyles struct {
	Header   lipgloss.Style
	Row      lipgloss.Style
	Selected lipgloss.Style
}

type StatusBarStyles struct {
	Bar     lipgloss.Style
	Key     lipgloss.Style
	Help    lipgloss.Style
	Input   lipgloss.Style
	Error   lipgloss.Style
	Command lipgloss.Style
}

type BuildStatusStyles struct {
	Running  lipgloss.Style
	Success  lipgloss.Style
	Failed   lipgloss.Style
	Aborted  lipgloss.Style
	Unstable lipgloss.Style
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

// WithDeuteranopiaFilter returns a copy of t with red/green semantic colors
// replaced by blue/orange, which are distinguishable regardless of red-green
// colour blindness (deuteranopia / protanopia).
//
// Only the four most critical semantic colours are remapped:
//   - Success  green (71)  → blue  (75)
//   - Failed   red   (167) → orange (208)
//   - Connected green (71) → blue  (75)
//   - BuildNum  green (71) → blue  (75)
//
// Everything else (running steel-blue, overrun amber, dim/accent) is unchanged
// because those colours are already safe or are not red/green.
func WithDeuteranopiaFilter(t Theme) Theme {
	cb := t
	blue := lipgloss.Color("75")
	orange := lipgloss.Color("208")

	cb.BuildStatus.Success = t.BuildStatus.Success.Copy().Foreground(blue)
	cb.BuildStatus.Failed = t.BuildStatus.Failed.Copy().Foreground(orange)
	cb.Header.Connected = t.Header.Connected.Copy().Foreground(blue)
	cb.Breadcrumb.BuildNum = t.Breadcrumb.BuildNum.Copy().Foreground(blue)

	return cb
}

// Default returns the default color theme.
func Default() Theme {
	green := lipgloss.Color("71")
	dim := lipgloss.Color("245")
	bright := lipgloss.Color("252")
	accent := lipgloss.Color("75")

	return Theme{
		Header: HeaderStyles{
			Title:     lipgloss.NewStyle().Bold(true).Foreground(accent),
			URL:       lipgloss.NewStyle().Foreground(bright),
			Label:     lipgloss.NewStyle().Foreground(dim),
			Value:     lipgloss.NewStyle().Foreground(bright),
			Connected: lipgloss.NewStyle().Foreground(green),
			Logo:      lipgloss.NewStyle().Foreground(accent).Bold(true),
		},
		Breadcrumb: BreadcrumbStyles{
			Segment:   lipgloss.NewStyle().Bold(true).Foreground(bright),
			Separator: lipgloss.NewStyle().Foreground(dim),
			Badge:     lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("179")).Bold(true),
			ViewType:  lipgloss.NewStyle().Bold(true).Foreground(accent),
			Context:   lipgloss.NewStyle().Foreground(bright),
			BuildNum:  lipgloss.NewStyle().Foreground(green),
			Paren:     lipgloss.NewStyle().Foreground(dim),
			Count:     lipgloss.NewStyle().Foreground(dim),
		},
		Search: SearchStyles{
			Match: lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("179")).Bold(true),
		},
		Table: TableStyles{
			Header:   lipgloss.NewStyle().Bold(true).Foreground(accent).Padding(0, 1),
			Row:      lipgloss.NewStyle().Foreground(bright).Padding(0, 1),
			Selected: lipgloss.NewStyle().Background(lipgloss.Color("62")).Foreground(lipgloss.Color("230")).Bold(true).Padding(0, 1),
		},
		StatusBar: StatusBarStyles{
			Bar:     lipgloss.NewStyle().Foreground(bright),
			Key:     lipgloss.NewStyle().Bold(true).Foreground(accent),
			Help:    lipgloss.NewStyle().Foreground(dim),
			Input:   lipgloss.NewStyle().Foreground(lipgloss.Color("230")),
			Error:   lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true),
			Command: lipgloss.NewStyle().Foreground(lipgloss.Color("230")),
		},
		BuildStatus: BuildStatusStyles{
			Running:  lipgloss.NewStyle().Foreground(lipgloss.Color("74")),  // steel blue
			Success:  lipgloss.NewStyle().Foreground(green),                 // muted green
			Failed:   lipgloss.NewStyle().Foreground(lipgloss.Color("167")), // muted red
			Aborted:  lipgloss.NewStyle().Foreground(dim),
			Unstable: lipgloss.NewStyle().Foreground(lipgloss.Color("179")), // muted amber
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
		Border: lipgloss.RoundedBorder(),
	}
}
