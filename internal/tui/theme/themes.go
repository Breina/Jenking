package theme

import "github.com/charmbracelet/lipgloss"

const (
	ThemeDefault    ThemeID = "default"
	ThemeRoyal      ThemeID = "royal"
	ThemeJenkins    ThemeID = "jenkins"
	ThemeMatrix     ThemeID = "matrix"
	ThemeDracula    ThemeID = "dracula"
	ThemeSolarized  ThemeID = "solarized"
	ThemeNord       ThemeID = "nord"
	ThemeGruvbox    ThemeID = "gruvbox"
	ThemeCatppuccin ThemeID = "catppuccin"
)

// ThemeInfo describes a theme for display in the theme picker.
type ThemeInfo struct {
	ID   ThemeID
	Name string
	Desc string
}

// AllThemes lists every available theme in display order.
var AllThemes = []ThemeInfo{
	{ThemeDefault, "Default", "original Jenking palette"},
	{ThemeRoyal, "Royal", "gold & velvet"},
	{ThemeJenkins, "Jenkins", "classic Jenkins website"},
	{ThemeMatrix, "Matrix", "green on black, unicode glyphs"},
	{ThemeDracula, "Dracula", "purple & pink on dark"},
	{ThemeSolarized, "Solarized", "Ethan Schoonover's dark palette"},
	{ThemeNord, "Nord", "Arctic blues & aurora"},
	{ThemeGruvbox, "Gruvbox", "warm retro groove"},
	{ThemeCatppuccin, "Catppuccin", "pastel mocha"},
}

// ByID returns the base Theme for the given ID. Unknown IDs return Default.
func ByID(id ThemeID) Theme {
	switch id {
	case ThemeRoyal:
		return Royal()
	case ThemeJenkins:
		return Jenkins()
	case ThemeMatrix:
		return Matrix()
	case ThemeDracula:
		return Dracula()
	case ThemeSolarized:
		return Solarized()
	case ThemeNord:
		return Nord()
	case ThemeGruvbox:
		return Gruvbox()
	case ThemeCatppuccin:
		return Catppuccin()
	default:
		return Default()
	}
}

// palette holds the key colours from which a full theme is derived.
type palette struct {
	accent   lipgloss.Color // headers, table headers, key bindings, logo
	green    lipgloss.Color // success, connected
	dim      lipgloss.Color // labels, hints, dim text
	bright   lipgloss.Color // regular text, values
	running  lipgloss.Color // running status
	failed   lipgloss.Color // failed/error (red)
	unstable lipgloss.Color // warnings, amber
	selBG    lipgloss.Color // selected row background
	selFG    lipgloss.Color // selected row foreground (0 = derive from bright)
	border   lipgloss.Color // panel border
	crown    lipgloss.Color // logo crown
	popup    lipgloss.Color // popup title/border
	progFill lipgloss.Color // progress bar filled (0 = use running)
	progBG   lipgloss.Color // progress bar background
	progOver lipgloss.Color // progress bar overrun (0 = use unstable)
	wSun     lipgloss.Color // weather sun
	wWarn    lipgloss.Color // weather unstable
	wStorm   lipgloss.Color // weather storm
	navActFG lipgloss.Color // nav tag active foreground
	navActBG lipgloss.Color // nav tag active background (0 = use accent)
	navAncFG lipgloss.Color // nav tag ancestor foreground (0 = use bright)
	navAncBG lipgloss.Color // nav tag ancestor background
}

func c(idx string) lipgloss.Color { return lipgloss.Color(idx) }

// fromPalette builds a full Theme from the key colours in p.
func fromPalette(id ThemeID, name string, p palette) Theme {
	if p.selFG == "" {
		p.selFG = p.bright
	}
	if p.progFill == "" {
		p.progFill = p.running
	}
	if p.progBG == "" {
		p.progBG = c("238")
	}
	if p.progOver == "" {
		p.progOver = p.unstable
	}
	if p.navActBG == "" {
		p.navActBG = p.accent
	}
	if p.navAncFG == "" {
		p.navAncFG = p.bright
	}
	if p.navAncBG == "" {
		p.navAncBG = c("238")
	}

	dark := c("232") // black for badge/search fg

	return Theme{
		ID:   id,
		Name: name,
		Header: HeaderStyles{
			Title:        lipgloss.NewStyle().Bold(true).Foreground(p.accent),
			URL:          lipgloss.NewStyle().Foreground(p.bright),
			Label:        lipgloss.NewStyle().Foreground(p.dim),
			Value:        lipgloss.NewStyle().Foreground(p.bright),
			Connected:    lipgloss.NewStyle().Foreground(p.green),
			Disconnected: lipgloss.NewStyle().Foreground(p.failed),
			RunningBadge: lipgloss.NewStyle().Foreground(p.unstable),
			Logo:         lipgloss.NewStyle().Foreground(p.accent).Bold(true),
			Crown:        lipgloss.NewStyle().Foreground(p.crown).Bold(true),
		},
		Breadcrumb: BreadcrumbStyles{
			Segment:   lipgloss.NewStyle().Bold(true).Foreground(p.bright),
			Separator: lipgloss.NewStyle().Foreground(p.dim),
			Badge:     lipgloss.NewStyle().Foreground(dark).Background(p.unstable).Bold(true),
			ViewType:  lipgloss.NewStyle().Bold(true).Foreground(p.accent),
			Filter:    lipgloss.NewStyle().Foreground(p.dim).Italic(true),
			Context:   lipgloss.NewStyle().Foreground(p.bright),
			BuildNum:  lipgloss.NewStyle().Foreground(p.green),
			Paren:     lipgloss.NewStyle().Foreground(p.dim),
			Count:     lipgloss.NewStyle().Foreground(p.dim),
		},
		Search: SearchStyles{
			Match:        lipgloss.NewStyle().Foreground(dark).Background(p.unstable).Bold(true),
			CurrentMatch: lipgloss.NewStyle().Foreground(dark).Background(lipgloss.Color("255")).Bold(true),
		},
		Table: TableStyles{
			Header:   lipgloss.NewStyle().Bold(true).Foreground(p.accent).Padding(0, 1),
			Row:      lipgloss.NewStyle().Foreground(p.bright).Padding(0, 1),
			Selected: lipgloss.NewStyle().Background(p.selBG).Foreground(p.selFG).Bold(true).Padding(0, 1),
		},
		StatusBar: StatusBarStyles{
			Bar:     lipgloss.NewStyle().Foreground(p.bright),
			Key:     lipgloss.NewStyle().Bold(true).Foreground(p.accent),
			Help:    lipgloss.NewStyle().Foreground(p.dim),
			Input:   lipgloss.NewStyle().Foreground(p.bright),
			Error:   lipgloss.NewStyle().Foreground(p.failed).Bold(true),
			Command: lipgloss.NewStyle().Foreground(p.bright),
		},
		BuildStatus: BuildStatusStyles{
			Running:  lipgloss.NewStyle().Foreground(p.running),
			Success:  lipgloss.NewStyle().Foreground(p.green),
			Failed:   lipgloss.NewStyle().Foreground(p.failed),
			Aborted:  lipgloss.NewStyle().Foreground(p.dim),
			Unstable: lipgloss.NewStyle().Foreground(p.unstable),
		},
		ProgressBar: ProgressBarStyles{
			Filled:      lipgloss.NewStyle().Foreground(p.progFill).Background(p.progBG),
			Empty:       lipgloss.NewStyle().Foreground(p.progBG).Background(p.progBG),
			Overrun:     lipgloss.NewStyle().Foreground(p.progOver).Background(p.progBG),
			FilledText:  lipgloss.NewStyle().Foreground(dark).Background(p.progFill),
			EmptyText:   lipgloss.NewStyle().Foreground(p.bright).Background(p.progBG),
			OverrunText: lipgloss.NewStyle().Foreground(dark).Background(p.progOver),
			// Selected variants: brighter colours on selected-row background.
			SelFilled:      lipgloss.NewStyle().Foreground(p.progFill).Background(p.selBG),
			SelEmpty:       lipgloss.NewStyle().Foreground(p.selBG).Background(p.selBG),
			SelOverrun:     lipgloss.NewStyle().Foreground(p.progOver).Background(p.selBG),
			SelFilledText:  lipgloss.NewStyle().Foreground(dark).Background(p.progFill),
			SelEmptyText:   lipgloss.NewStyle().Foreground(p.selFG).Background(p.selBG),
			SelOverrunText: lipgloss.NewStyle().Foreground(dark).Background(p.progOver),
		},
		Log: LogStyles{
			Normal:  lipgloss.NewStyle().Foreground(p.bright),
			Dim:     lipgloss.NewStyle().Foreground(p.dim),
			Error:   lipgloss.NewStyle().Foreground(p.failed),
			Warning: lipgloss.NewStyle().Foreground(p.unstable),
			Trunc:   lipgloss.NewStyle().Foreground(p.dim),
		},
		Stage: StageStyles{
			GhostDim: lipgloss.NewStyle().Foreground(p.dim),
		},
		Weather: WeatherStyles{
			Sun:      lipgloss.NewStyle().Foreground(p.wSun),
			Unstable: lipgloss.NewStyle().Foreground(p.wWarn),
			Storm:    lipgloss.NewStyle().Foreground(p.wStorm),
			None:     lipgloss.NewStyle().Foreground(p.dim),
		},
		NavTag: NavTagStyles{
			Active:   lipgloss.NewStyle().Foreground(p.navActFG).Background(p.navActBG).Bold(true).Padding(0, 1),
			Ancestor: lipgloss.NewStyle().Foreground(p.navAncFG).Background(p.navAncBG).Padding(0, 1),
		},
		Popup: PopupStyles{
			Title:  lipgloss.NewStyle().Bold(true).Foreground(p.popup),
			Accent: lipgloss.NewStyle().Bold(true).Foreground(dark).Background(p.popup),
			Hint:   lipgloss.NewStyle().Foreground(p.dim),
			Label:  lipgloss.NewStyle().Bold(true).Foreground(p.bright),
			Desc:   lipgloss.NewStyle().Foreground(p.dim).Italic(true),
			Normal: lipgloss.NewStyle().Foreground(p.bright),
		},
		PanelBorder: lipgloss.NewStyle().Foreground(p.border),
		Border:      lipgloss.RoundedBorder(),
	}
}

// Royal returns a gold-and-velvet theme.
func Royal() Theme {
	return fromPalette(ThemeRoyal, "Royal", palette{
		accent:   c("178"), // gold
		green:    c("106"), // olive green
		dim:      c("101"), // dark khaki
		bright:   c("223"), // cream
		running:  c("178"), // gold
		failed:   c("160"), // crimson
		unstable: c("172"), // amber-gold
		selBG:    c("53"),  // velvet purple
		selFG:    c("220"), // bright gold
		border:   c("136"), // dark gold
		crown:    c("220"), // bright gold
		popup:    c("220"), // bright gold
		progFill: c("178"),
		progBG:   c("238"),
		progOver: c("172"),
		wSun:     c("220"),
		wWarn:    c("172"),
		wStorm:   c("97"),  // dull purple
		navActFG: c("232"), // black on gold
		navActBG: c("178"),
		navAncFG: c("223"),
		navAncBG: c("58"), // dark olive
	})
}

// Jenkins returns a theme based on the Jenkins website colour scheme.
func Jenkins() Theme {
	return fromPalette(ThemeJenkins, "Jenkins", palette{
		accent:   c("67"),  // steel blue (Jenkins primary)
		green:    c("115"), // sage green (Jenkins green)
		dim:      c("243"), // gray
		bright:   c("254"), // near-white
		running:  c("67"),  // steel blue
		failed:   c("160"), // red
		unstable: c("172"), // amber
		selBG:    c("24"),  // dark teal
		selFG:    c("254"),
		border:   c("24"),  // dark teal
		crown:    c("172"), // amber (the butler)
		popup:    c("172"), // amber
		progFill: c("67"),
		progBG:   c("238"),
		wSun:     c("220"),
		wWarn:    c("172"),
		wStorm:   c("67"),
		navActFG: c("254"),
		navActBG: c("67"),
		navAncBG: c("237"),
	})
}

// Matrix returns a green-on-black theme with unicode glyph icon replacements.
func Matrix() Theme {
	neon := c("46") // bright neon green
	med := c("34")  // medium green
	dim := c("22")  // dark green
	lime := c("82") // lime green
	bg := c("233")  // near-black
	black := c("0") // black

	t := Theme{
		ID:   ThemeMatrix,
		Name: "Matrix",
		Icons: Icons{
			StatusRunning:   "\u221e", // ∞
			StatusSuccess:   "\u221a", // √
			StatusFailed:    "\u2205", // ∅
			StatusAborted:   "\u25aa", // ▪
			StatusUnstable:  "\u25b3", // △
			StatusSkipped:   "\u25c7", // ◇
			StatusNotBuilt:  "\u25ab", // ▫
			StatusUnknown:   "\u25cb", // ○
			WeatherSun:      "\u25b3", // △
			WeatherUnstable: "\u25c7", // ◇
			WeatherStorm:    "\u25bd", // ▽
			TypeFolder:      "\u25b8", // ▸ (keep)
			TypePipeline:    "\u220f", // ∏
			TypeMultiBranch: "\u2295", // ⊕
			TypeFreeStyle:   "\u2217", // ∗
			Warning:         "\u25b3", // △
			Error:           "\u2205", // ∅
		},
		Header: HeaderStyles{
			Title:        lipgloss.NewStyle().Bold(true).Foreground(neon),
			URL:          lipgloss.NewStyle().Foreground(med),
			Label:        lipgloss.NewStyle().Foreground(dim),
			Value:        lipgloss.NewStyle().Foreground(med),
			Connected:    lipgloss.NewStyle().Foreground(med),
			Disconnected: lipgloss.NewStyle().Foreground(neon).Bold(true),
			RunningBadge: lipgloss.NewStyle().Foreground(neon),
			Logo:         lipgloss.NewStyle().Foreground(neon).Bold(true),
			Crown:        lipgloss.NewStyle().Foreground(neon).Bold(true),
		},
		Breadcrumb: BreadcrumbStyles{
			Segment:   lipgloss.NewStyle().Bold(true).Foreground(med),
			Separator: lipgloss.NewStyle().Foreground(dim),
			Badge:     lipgloss.NewStyle().Foreground(black).Background(med).Bold(true),
			ViewType:  lipgloss.NewStyle().Bold(true).Foreground(neon),
			Filter:    lipgloss.NewStyle().Foreground(dim).Italic(true),
			Context:   lipgloss.NewStyle().Foreground(med),
			BuildNum:  lipgloss.NewStyle().Foreground(med),
			Paren:     lipgloss.NewStyle().Foreground(dim),
			Count:     lipgloss.NewStyle().Foreground(dim),
		},
		Search: SearchStyles{
			Match:        lipgloss.NewStyle().Foreground(black).Background(med).Bold(true),
			CurrentMatch: lipgloss.NewStyle().Foreground(black).Background(lipgloss.Color("255")).Bold(true),
		},
		Table: TableStyles{
			Header:   lipgloss.NewStyle().Bold(true).Foreground(neon).Padding(0, 1),
			Row:      lipgloss.NewStyle().Foreground(med).Padding(0, 1),
			Selected: lipgloss.NewStyle().Background(dim).Foreground(neon).Bold(true).Padding(0, 1),
		},
		StatusBar: StatusBarStyles{
			Bar:     lipgloss.NewStyle().Foreground(med),
			Key:     lipgloss.NewStyle().Bold(true).Foreground(neon),
			Help:    lipgloss.NewStyle().Foreground(dim),
			Input:   lipgloss.NewStyle().Foreground(neon),
			Error:   lipgloss.NewStyle().Foreground(neon).Bold(true),
			Command: lipgloss.NewStyle().Foreground(neon),
		},
		BuildStatus: BuildStatusStyles{
			Running:  lipgloss.NewStyle().Foreground(neon).Bold(true),
			Success:  lipgloss.NewStyle().Foreground(med),
			Failed:   lipgloss.NewStyle().Foreground(neon).Bold(true).Italic(true),
			Aborted:  lipgloss.NewStyle().Foreground(dim),
			Unstable: lipgloss.NewStyle().Foreground(lime),
		},
		ProgressBar: ProgressBarStyles{
			Filled:         lipgloss.NewStyle().Foreground(med).Background(bg),
			Empty:          lipgloss.NewStyle().Foreground(bg).Background(bg),
			Overrun:        lipgloss.NewStyle().Foreground(lime).Background(bg),
			FilledText:     lipgloss.NewStyle().Foreground(black).Background(med),
			EmptyText:      lipgloss.NewStyle().Foreground(med).Background(bg),
			OverrunText:    lipgloss.NewStyle().Foreground(black).Background(lime),
			SelFilled:      lipgloss.NewStyle().Foreground(neon).Background(dim),
			SelEmpty:       lipgloss.NewStyle().Foreground(dim).Background(dim),
			SelOverrun:     lipgloss.NewStyle().Foreground(lime).Background(dim),
			SelFilledText:  lipgloss.NewStyle().Foreground(black).Background(neon),
			SelEmptyText:   lipgloss.NewStyle().Foreground(neon).Background(dim),
			SelOverrunText: lipgloss.NewStyle().Foreground(black).Background(lime),
		},
		Log: LogStyles{
			Normal:  lipgloss.NewStyle().Foreground(med),
			Dim:     lipgloss.NewStyle().Foreground(dim).Italic(true),
			Error:   lipgloss.NewStyle().Foreground(neon).Bold(true),
			Warning: lipgloss.NewStyle().Foreground(lime),
			Trunc:   lipgloss.NewStyle().Foreground(dim),
		},
		Stage: StageStyles{
			GhostDim: lipgloss.NewStyle().Foreground(dim),
		},
		Weather: WeatherStyles{
			Sun:      lipgloss.NewStyle().Foreground(med),
			Unstable: lipgloss.NewStyle().Foreground(lime),
			Storm:    lipgloss.NewStyle().Foreground(neon).Bold(true),
			None:     lipgloss.NewStyle().Foreground(dim),
		},
		NavTag: NavTagStyles{
			Active:   lipgloss.NewStyle().Foreground(black).Background(med).Bold(true).Padding(0, 1),
			Ancestor: lipgloss.NewStyle().Foreground(med).Background(bg).Padding(0, 1),
		},
		Popup: PopupStyles{
			Title:  lipgloss.NewStyle().Bold(true).Foreground(neon),
			Accent: lipgloss.NewStyle().Bold(true).Foreground(black).Background(neon),
			Hint:   lipgloss.NewStyle().Foreground(dim),
			Label:  lipgloss.NewStyle().Bold(true).Foreground(med),
			Desc:   lipgloss.NewStyle().Foreground(dim).Italic(true),
			Normal: lipgloss.NewStyle().Foreground(med),
		},
		PanelBorder: lipgloss.NewStyle().Foreground(dim),
		Border:      lipgloss.RoundedBorder(),
	}
	return t
}

// Dracula returns a purple-and-pink theme inspired by the Dracula palette.
func Dracula() Theme {
	return fromPalette(ThemeDracula, "Dracula", palette{
		accent:   c("141"), // purple
		green:    c("84"),  // green
		dim:      c("60"),  // dark purple-gray
		bright:   c("253"), // foreground
		running:  c("117"), // cyan
		failed:   c("203"), // red
		unstable: c("215"), // orange
		selBG:    c("61"),  // comment/selection
		selFG:    c("253"),
		border:   c("61"),
		crown:    c("228"), // yellow
		popup:    c("212"), // pink
		progFill: c("117"),
		progBG:   c("237"),
		wSun:     c("228"),
		wWarn:    c("215"),
		wStorm:   c("117"),
		navActFG: c("232"),
		navActBG: c("141"),
		navAncBG: c("237"),
	})
}

// Solarized returns a theme based on Ethan Schoonover's Solarized Dark palette.
func Solarized() Theme {
	return fromPalette(ThemeSolarized, "Solarized", palette{
		accent:   c("33"),  // blue
		green:    c("64"),  // green
		dim:      c("240"), // base01
		bright:   c("246"), // base0
		running:  c("37"),  // cyan
		failed:   c("124"), // red
		unstable: c("136"), // yellow
		selBG:    c("237"), // base02
		selFG:    c("247"), // base1
		border:   c("240"),
		crown:    c("136"), // yellow
		popup:    c("166"), // orange
		progFill: c("37"),
		progBG:   c("236"),
		wSun:     c("136"),
		wWarn:    c("166"),
		wStorm:   c("33"),
		navActFG: c("234"),
		navActBG: c("33"),
		navAncBG: c("237"),
	})
}

// Nord returns a theme inspired by the Nord arctic colour palette.
func Nord() Theme {
	return fromPalette(ThemeNord, "Nord", palette{
		accent:   c("110"), // frost blue
		green:    c("144"), // aurora green
		dim:      c("240"), // polar night
		bright:   c("254"), // snow storm
		running:  c("110"), // frost
		failed:   c("131"), // aurora red
		unstable: c("222"), // aurora yellow
		selBG:    c("238"), // polar night 2
		selFG:    c("254"),
		border:   c("60"),  // frost dark
		crown:    c("222"), // aurora yellow
		popup:    c("139"), // aurora purple
		progFill: c("110"),
		progBG:   c("237"),
		wSun:     c("222"),
		wWarn:    c("173"), // aurora orange
		wStorm:   c("110"),
		navActFG: c("236"),
		navActBG: c("110"),
		navAncBG: c("238"),
	})
}

// Gruvbox returns a warm retro theme inspired by the Gruvbox palette.
func Gruvbox() Theme {
	return fromPalette(ThemeGruvbox, "Gruvbox", palette{
		accent:   c("109"), // blue
		green:    c("142"), // green
		dim:      c("243"), // gray
		bright:   c("223"), // foreground
		running:  c("109"), // blue
		failed:   c("167"), // red
		unstable: c("214"), // yellow
		selBG:    c("237"), // bg1
		selFG:    c("223"),
		border:   c("239"), // bg2
		crown:    c("214"), // yellow
		popup:    c("208"), // orange
		progFill: c("109"),
		progBG:   c("236"),
		wSun:     c("214"),
		wWarn:    c("208"),
		wStorm:   c("109"),
		navActFG: c("235"),
		navActBG: c("109"),
		navAncBG: c("237"),
	})
}

// Catppuccin returns a pastel theme inspired by Catppuccin Mocha.
func Catppuccin() Theme {
	return fromPalette(ThemeCatppuccin, "Catppuccin", palette{
		accent:   c("141"), // mauve
		green:    c("151"), // green
		dim:      c("243"), // overlay
		bright:   c("189"), // text
		running:  c("111"), // blue
		failed:   c("211"), // red
		unstable: c("222"), // yellow
		selBG:    c("238"), // surface0
		selFG:    c("189"),
		border:   c("60"),  // surface1
		crown:    c("222"), // yellow
		popup:    c("209"), // peach
		progFill: c("111"),
		progBG:   c("237"),
		wSun:     c("222"),
		wWarn:    c("209"),
		wStorm:   c("111"),
		navActFG: c("234"),
		navActBG: c("141"),
		navAncBG: c("238"),
	})
}
