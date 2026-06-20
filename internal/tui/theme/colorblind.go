package theme

import (
	"math"
	"strconv"

	"github.com/charmbracelet/lipgloss"
)

// ColorblindnessType identifies the kind of colour vision deficiency to compensate for.
type ColorblindnessType string

const (
	ColorblindnessNone          ColorblindnessType = "none"
	ColorblindnessDeuteranopia  ColorblindnessType = "deuteranopia"
	ColorblindnessProtanopia    ColorblindnessType = "protanopia"
	ColorblindnessTritanopia    ColorblindnessType = "tritanopia"
	ColorblindnessAchromatopsia ColorblindnessType = "achromatopsia"
)

// AllColorblindnessTypes lists every supported type in display order.
var AllColorblindnessTypes = []ColorblindnessType{
	ColorblindnessNone,
	ColorblindnessDeuteranopia,
	ColorblindnessProtanopia,
	ColorblindnessTritanopia,
	ColorblindnessAchromatopsia,
}

// ansiSRGB holds canonical sRGB values for xterm 256 ANSI indices 0–15.
var ansiSRGB = [16][3]uint8{
	{0, 0, 0},       // 0  Black
	{128, 0, 0},     // 1  Maroon
	{0, 128, 0},     // 2  Green
	{128, 128, 0},   // 3  Olive
	{0, 0, 128},     // 4  Navy
	{128, 0, 128},   // 5  Purple
	{0, 128, 128},   // 6  Teal
	{192, 192, 192}, // 7  Silver
	{128, 128, 128}, // 8  Grey
	{255, 0, 0},     // 9  Red
	{0, 255, 0},     // 10 Lime
	{255, 255, 0},   // 11 Yellow
	{0, 0, 255},     // 12 Blue
	{255, 0, 255},   // 13 Fuchsia
	{0, 255, 255},   // 14 Aqua
	{255, 255, 255}, // 15 White
}

// xtermToSRGB decodes an xterm 256 palette index to an sRGB triplet.
func xtermToSRGB(idx int) (uint8, uint8, uint8) {
	switch {
	case idx < 16:
		c := ansiSRGB[idx]
		return c[0], c[1], c[2]
	case idx < 232:
		i := idx - 16
		b := i % 6
		g := (i / 6) % 6
		r := i / 36
		levels := [6]uint8{0, 95, 135, 175, 215, 255}
		return levels[r], levels[g], levels[b]
	default: // 232–255 grayscale
		v := uint8(8 + (idx-232)*10)
		return v, v, v
	}
}

// srgbToLinear converts a sRGB uint8 channel value to linear light [0,1].
func srgbToLinear(c uint8) float64 {
	s := float64(c) / 255.0
	if s <= 0.04045 {
		return s / 12.92
	}
	return math.Pow((s+0.055)/1.055, 2.4)
}

// linearToSRGB converts a linear light value [0,1] back to a sRGB uint8.
func linearToSRGB(l float64) uint8 {
	l = clamp01(l)
	var s float64
	if l <= 0.0031308 {
		s = l * 12.92
	} else {
		s = 1.055*math.Pow(l, 1.0/2.4) - 0.055
	}
	return uint8(math.Round(s * 255))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// transformLinear applies daltonization in linear RGB space.
// Inputs and outputs are in [0,1].
func transformLinear(r, g, b float64, t ColorblindnessType) (float64, float64, float64) {
	switch t {
	case ColorblindnessDeuteranopia:
		// Missing M (green) cones — red/green confusion.
		// errR = 0.701*(R−G): positive for reds, negative for greens.
		// Use |errR| so the blue channel increases for BOTH extremes:
		//   red   → magenta/pink  (B added to a high-R colour)
		//   green → cyan          (B added to a high-G colour)
		// Neutral colours (R≈G) get no shift.
		sr := 0.299*r + 0.701*g
		errR := r - sr
		return r, g, clamp01(b + 0.7*math.Abs(errR))

	case ColorblindnessProtanopia:
		// Missing L (red) cones.
		sr := 0.109*r + 0.891*g
		sg := 0.109*r + 0.891*g
		errR := r - sr
		errG := g - sg
		return r, clamp01(g + 0.7*errR), clamp01(b + 0.7*errG)

	case ColorblindnessTritanopia:
		// Missing S (blue) cones.
		sb := 0.7*g + 0.3*b
		errB := b - sb
		return clamp01(r + 0.7*errB), clamp01(g + 0.7*errB), b

	case ColorblindnessAchromatopsia:
		// No colour vision — collapse to luminance.
		y := 0.2126*r + 0.7152*g + 0.0722*b
		return y, y, y

	default:
		return r, g, b
	}
}

// transformRGB applies a colorblindness correction to an sRGB triplet.
func transformRGB(r, g, b uint8, t ColorblindnessType) (uint8, uint8, uint8) {
	lr := srgbToLinear(r)
	lg := srgbToLinear(g)
	lb := srgbToLinear(b)
	lr, lg, lb = transformLinear(lr, lg, lb, t)
	return linearToSRGB(lr), linearToSRGB(lg), linearToSRGB(lb)
}

// rgbToXterm maps an sRGB triplet to the nearest xterm-256 palette index.
func rgbToXterm(r, g, b uint8) int {
	bestIdx := 0
	bestDist := math.MaxFloat64
	for i := 0; i < 256; i++ {
		cr, cg, cb := xtermToSRGB(i)
		dr := float64(r) - float64(cr)
		dg := float64(g) - float64(cg)
		db := float64(b) - float64(cb)
		d := dr*dr + dg*dg + db*db
		if d < bestDist {
			bestDist = d
			bestIdx = i
		}
	}
	return bestIdx
}

// TransformColor applies a colorblindness correction to a lipgloss.Color.
// Supports "#RRGGBB" hex strings and decimal xterm 256 indices ("0"–"255").
// Corrected colors are returned as "#RRGGBB" true-color hex.
// Empty strings and unrecognised formats are returned unchanged.
func TransformColor(c lipgloss.Color, t ColorblindnessType) lipgloss.Color {
	if t == ColorblindnessNone {
		return c
	}
	s := string(c)
	if s == "" {
		return c
	}

	var r, g, b uint8

	if len(s) == 7 && s[0] == '#' {
		v, err := strconv.ParseUint(s[1:], 16, 32)
		if err != nil {
			return c
		}
		r = uint8(v >> 16)
		g = uint8((v >> 8) & 0xff)
		b = uint8(v & 0xff)
	} else {
		idx, err := strconv.Atoi(s)
		if err != nil || idx < 0 || idx > 255 {
			return c
		}
		r, g, b = xtermToSRGB(idx)
	}

	nr, ng, nb := transformRGB(r, g, b, t)
	return lipgloss.Color(strconv.Itoa(rgbToXterm(nr, ng, nb)))
}

// extractColor type-asserts a lipgloss.TerminalColor to lipgloss.Color.
// Returns an empty string if nil or an unsupported type.
func extractColor(tc lipgloss.TerminalColor) lipgloss.Color {
	if tc == nil {
		return ""
	}
	if c, ok := tc.(lipgloss.Color); ok {
		return c
	}
	return ""
}

// applyToStyle applies fn to the Foreground and Background of a lipgloss.Style.
// BorderForeground/BorderBackground are not exposed by lipgloss v1.1.0 getters,
// but none of the theme styles use them directly.
func applyToStyle(s lipgloss.Style, fn func(lipgloss.Color) lipgloss.Color) lipgloss.Style {
	if fg := extractColor(s.GetForeground()); fg != "" {
		s = s.Foreground(fn(fg))
	}
	if bg := extractColor(s.GetBackground()); bg != "" {
		s = s.Background(fn(bg))
	}
	return s
}

// styleField describes a single lipgloss.Style field on Theme. It carries a
// getter (from the source theme) and a setter (into the destination theme),
// allowing ApplyColorblindFilter to iterate over a flat table rather than
// repeat per-field assignments.
type styleField struct {
	get func(*Theme) lipgloss.Style
	set func(*Theme, lipgloss.Style)
}

// themeStyleFields lists every Style field on Theme that carries a colour.
// Border (lipgloss.Border) is intentionally absent — it stores no colours.
var themeStyleFields = []styleField{
	// Header
	{func(t *Theme) lipgloss.Style { return t.Header.Title }, func(t *Theme, s lipgloss.Style) { t.Header.Title = s }},
	{func(t *Theme) lipgloss.Style { return t.Header.URL }, func(t *Theme, s lipgloss.Style) { t.Header.URL = s }},
	{func(t *Theme) lipgloss.Style { return t.Header.Label }, func(t *Theme, s lipgloss.Style) { t.Header.Label = s }},
	{func(t *Theme) lipgloss.Style { return t.Header.Value }, func(t *Theme, s lipgloss.Style) { t.Header.Value = s }},
	{func(t *Theme) lipgloss.Style { return t.Header.Connected }, func(t *Theme, s lipgloss.Style) { t.Header.Connected = s }},
	{func(t *Theme) lipgloss.Style { return t.Header.Disconnected }, func(t *Theme, s lipgloss.Style) { t.Header.Disconnected = s }},
	{func(t *Theme) lipgloss.Style { return t.Header.RunningBadge }, func(t *Theme, s lipgloss.Style) { t.Header.RunningBadge = s }},
	{func(t *Theme) lipgloss.Style { return t.Header.Logo }, func(t *Theme, s lipgloss.Style) { t.Header.Logo = s }},
	{func(t *Theme) lipgloss.Style { return t.Header.Crown }, func(t *Theme, s lipgloss.Style) { t.Header.Crown = s }},
	// Breadcrumb
	{func(t *Theme) lipgloss.Style { return t.Breadcrumb.Segment }, func(t *Theme, s lipgloss.Style) { t.Breadcrumb.Segment = s }},
	{func(t *Theme) lipgloss.Style { return t.Breadcrumb.Separator }, func(t *Theme, s lipgloss.Style) { t.Breadcrumb.Separator = s }},
	{func(t *Theme) lipgloss.Style { return t.Breadcrumb.Badge }, func(t *Theme, s lipgloss.Style) { t.Breadcrumb.Badge = s }},
	{func(t *Theme) lipgloss.Style { return t.Breadcrumb.ViewType }, func(t *Theme, s lipgloss.Style) { t.Breadcrumb.ViewType = s }},
	{func(t *Theme) lipgloss.Style { return t.Breadcrumb.Filter }, func(t *Theme, s lipgloss.Style) { t.Breadcrumb.Filter = s }},
	{func(t *Theme) lipgloss.Style { return t.Breadcrumb.Context }, func(t *Theme, s lipgloss.Style) { t.Breadcrumb.Context = s }},
	{func(t *Theme) lipgloss.Style { return t.Breadcrumb.BuildNum }, func(t *Theme, s lipgloss.Style) { t.Breadcrumb.BuildNum = s }},
	{func(t *Theme) lipgloss.Style { return t.Breadcrumb.Paren }, func(t *Theme, s lipgloss.Style) { t.Breadcrumb.Paren = s }},
	{func(t *Theme) lipgloss.Style { return t.Breadcrumb.Count }, func(t *Theme, s lipgloss.Style) { t.Breadcrumb.Count = s }},
	// Search
	{func(t *Theme) lipgloss.Style { return t.Search.Match }, func(t *Theme, s lipgloss.Style) { t.Search.Match = s }},
	// Table
	{func(t *Theme) lipgloss.Style { return t.Table.Header }, func(t *Theme, s lipgloss.Style) { t.Table.Header = s }},
	{func(t *Theme) lipgloss.Style { return t.Table.Row }, func(t *Theme, s lipgloss.Style) { t.Table.Row = s }},
	{func(t *Theme) lipgloss.Style { return t.Table.Selected }, func(t *Theme, s lipgloss.Style) { t.Table.Selected = s }},
	// StatusBar
	{func(t *Theme) lipgloss.Style { return t.StatusBar.Bar }, func(t *Theme, s lipgloss.Style) { t.StatusBar.Bar = s }},
	{func(t *Theme) lipgloss.Style { return t.StatusBar.Key }, func(t *Theme, s lipgloss.Style) { t.StatusBar.Key = s }},
	{func(t *Theme) lipgloss.Style { return t.StatusBar.Help }, func(t *Theme, s lipgloss.Style) { t.StatusBar.Help = s }},
	{func(t *Theme) lipgloss.Style { return t.StatusBar.Input }, func(t *Theme, s lipgloss.Style) { t.StatusBar.Input = s }},
	{func(t *Theme) lipgloss.Style { return t.StatusBar.Error }, func(t *Theme, s lipgloss.Style) { t.StatusBar.Error = s }},
	{func(t *Theme) lipgloss.Style { return t.StatusBar.Command }, func(t *Theme, s lipgloss.Style) { t.StatusBar.Command = s }},
	// BuildStatus
	{func(t *Theme) lipgloss.Style { return t.BuildStatus.Running }, func(t *Theme, s lipgloss.Style) { t.BuildStatus.Running = s }},
	{func(t *Theme) lipgloss.Style { return t.BuildStatus.Success }, func(t *Theme, s lipgloss.Style) { t.BuildStatus.Success = s }},
	{func(t *Theme) lipgloss.Style { return t.BuildStatus.Failed }, func(t *Theme, s lipgloss.Style) { t.BuildStatus.Failed = s }},
	{func(t *Theme) lipgloss.Style { return t.BuildStatus.Aborted }, func(t *Theme, s lipgloss.Style) { t.BuildStatus.Aborted = s }},
	{func(t *Theme) lipgloss.Style { return t.BuildStatus.Unstable }, func(t *Theme, s lipgloss.Style) { t.BuildStatus.Unstable = s }},
	{func(t *Theme) lipgloss.Style { return t.BuildStatus.PausedInput }, func(t *Theme, s lipgloss.Style) { t.BuildStatus.PausedInput = s }},
	// ProgressBar
	{func(t *Theme) lipgloss.Style { return t.ProgressBar.Filled }, func(t *Theme, s lipgloss.Style) { t.ProgressBar.Filled = s }},
	{func(t *Theme) lipgloss.Style { return t.ProgressBar.Empty }, func(t *Theme, s lipgloss.Style) { t.ProgressBar.Empty = s }},
	{func(t *Theme) lipgloss.Style { return t.ProgressBar.Overrun }, func(t *Theme, s lipgloss.Style) { t.ProgressBar.Overrun = s }},
	{func(t *Theme) lipgloss.Style { return t.ProgressBar.FilledText }, func(t *Theme, s lipgloss.Style) { t.ProgressBar.FilledText = s }},
	{func(t *Theme) lipgloss.Style { return t.ProgressBar.EmptyText }, func(t *Theme, s lipgloss.Style) { t.ProgressBar.EmptyText = s }},
	{func(t *Theme) lipgloss.Style { return t.ProgressBar.OverrunText }, func(t *Theme, s lipgloss.Style) { t.ProgressBar.OverrunText = s }},
	{func(t *Theme) lipgloss.Style { return t.ProgressBar.SelFilled }, func(t *Theme, s lipgloss.Style) { t.ProgressBar.SelFilled = s }},
	{func(t *Theme) lipgloss.Style { return t.ProgressBar.SelEmpty }, func(t *Theme, s lipgloss.Style) { t.ProgressBar.SelEmpty = s }},
	{func(t *Theme) lipgloss.Style { return t.ProgressBar.SelOverrun }, func(t *Theme, s lipgloss.Style) { t.ProgressBar.SelOverrun = s }},
	{func(t *Theme) lipgloss.Style { return t.ProgressBar.SelFilledText }, func(t *Theme, s lipgloss.Style) { t.ProgressBar.SelFilledText = s }},
	{func(t *Theme) lipgloss.Style { return t.ProgressBar.SelEmptyText }, func(t *Theme, s lipgloss.Style) { t.ProgressBar.SelEmptyText = s }},
	{func(t *Theme) lipgloss.Style { return t.ProgressBar.SelOverrunText }, func(t *Theme, s lipgloss.Style) { t.ProgressBar.SelOverrunText = s }},
	// Log
	{func(t *Theme) lipgloss.Style { return t.Log.Normal }, func(t *Theme, s lipgloss.Style) { t.Log.Normal = s }},
	{func(t *Theme) lipgloss.Style { return t.Log.Dim }, func(t *Theme, s lipgloss.Style) { t.Log.Dim = s }},
	{func(t *Theme) lipgloss.Style { return t.Log.Error }, func(t *Theme, s lipgloss.Style) { t.Log.Error = s }},
	{func(t *Theme) lipgloss.Style { return t.Log.Warning }, func(t *Theme, s lipgloss.Style) { t.Log.Warning = s }},
	{func(t *Theme) lipgloss.Style { return t.Log.Trunc }, func(t *Theme, s lipgloss.Style) { t.Log.Trunc = s }},
	// Stage
	{func(t *Theme) lipgloss.Style { return t.Stage.GhostDim }, func(t *Theme, s lipgloss.Style) { t.Stage.GhostDim = s }},
	// Weather
	{func(t *Theme) lipgloss.Style { return t.Weather.Sun }, func(t *Theme, s lipgloss.Style) { t.Weather.Sun = s }},
	{func(t *Theme) lipgloss.Style { return t.Weather.Unstable }, func(t *Theme, s lipgloss.Style) { t.Weather.Unstable = s }},
	{func(t *Theme) lipgloss.Style { return t.Weather.Storm }, func(t *Theme, s lipgloss.Style) { t.Weather.Storm = s }},
	{func(t *Theme) lipgloss.Style { return t.Weather.None }, func(t *Theme, s lipgloss.Style) { t.Weather.None = s }},
	// NavTag
	{func(t *Theme) lipgloss.Style { return t.NavTag.Active }, func(t *Theme, s lipgloss.Style) { t.NavTag.Active = s }},
	{func(t *Theme) lipgloss.Style { return t.NavTag.Ancestor }, func(t *Theme, s lipgloss.Style) { t.NavTag.Ancestor = s }},
	// Popup
	{func(t *Theme) lipgloss.Style { return t.Popup.Title }, func(t *Theme, s lipgloss.Style) { t.Popup.Title = s }},
	{func(t *Theme) lipgloss.Style { return t.Popup.Accent }, func(t *Theme, s lipgloss.Style) { t.Popup.Accent = s }},
	{func(t *Theme) lipgloss.Style { return t.Popup.Hint }, func(t *Theme, s lipgloss.Style) { t.Popup.Hint = s }},
	{func(t *Theme) lipgloss.Style { return t.Popup.Label }, func(t *Theme, s lipgloss.Style) { t.Popup.Label = s }},
	{func(t *Theme) lipgloss.Style { return t.Popup.Desc }, func(t *Theme, s lipgloss.Style) { t.Popup.Desc = s }},
	{func(t *Theme) lipgloss.Style { return t.Popup.Normal }, func(t *Theme, s lipgloss.Style) { t.Popup.Normal = s }},
	// PanelBorder
	{func(t *Theme) lipgloss.Style { return t.PanelBorder }, func(t *Theme, s lipgloss.Style) { t.PanelBorder = s }},
}

// ApplyColorblindFilter returns a copy of base with all theme colors adjusted
// for the given colorblindness type. Returns base unchanged for ColorblindnessNone.
// Border (lipgloss.Border) is not touched — it contains no colors.
func ApplyColorblindFilter(base Theme, cbType ColorblindnessType) Theme {
	if cbType == ColorblindnessNone {
		return base
	}
	fn := func(c lipgloss.Color) lipgloss.Color {
		return TransformColor(c, cbType)
	}
	t := base
	for _, f := range themeStyleFields {
		f.set(&t, applyToStyle(f.get(&base), fn))
	}
	return t
}
