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

	// Header
	t.Header.Title = applyToStyle(base.Header.Title, fn)
	t.Header.URL = applyToStyle(base.Header.URL, fn)
	t.Header.Label = applyToStyle(base.Header.Label, fn)
	t.Header.Value = applyToStyle(base.Header.Value, fn)
	t.Header.Connected = applyToStyle(base.Header.Connected, fn)
	t.Header.Disconnected = applyToStyle(base.Header.Disconnected, fn)
	t.Header.RunningBadge = applyToStyle(base.Header.RunningBadge, fn)
	t.Header.Logo = applyToStyle(base.Header.Logo, fn)
	t.Header.Crown = applyToStyle(base.Header.Crown, fn)

	// Breadcrumb
	t.Breadcrumb.Segment = applyToStyle(base.Breadcrumb.Segment, fn)
	t.Breadcrumb.Separator = applyToStyle(base.Breadcrumb.Separator, fn)
	t.Breadcrumb.Badge = applyToStyle(base.Breadcrumb.Badge, fn)
	t.Breadcrumb.ViewType = applyToStyle(base.Breadcrumb.ViewType, fn)
	t.Breadcrumb.Filter = applyToStyle(base.Breadcrumb.Filter, fn)
	t.Breadcrumb.Context = applyToStyle(base.Breadcrumb.Context, fn)
	t.Breadcrumb.BuildNum = applyToStyle(base.Breadcrumb.BuildNum, fn)
	t.Breadcrumb.Paren = applyToStyle(base.Breadcrumb.Paren, fn)
	t.Breadcrumb.Count = applyToStyle(base.Breadcrumb.Count, fn)

	// Search
	t.Search.Match = applyToStyle(base.Search.Match, fn)

	// Table
	t.Table.Header = applyToStyle(base.Table.Header, fn)
	t.Table.Row = applyToStyle(base.Table.Row, fn)
	t.Table.Selected = applyToStyle(base.Table.Selected, fn)

	// StatusBar
	t.StatusBar.Bar = applyToStyle(base.StatusBar.Bar, fn)
	t.StatusBar.Key = applyToStyle(base.StatusBar.Key, fn)
	t.StatusBar.Help = applyToStyle(base.StatusBar.Help, fn)
	t.StatusBar.Input = applyToStyle(base.StatusBar.Input, fn)
	t.StatusBar.Error = applyToStyle(base.StatusBar.Error, fn)
	t.StatusBar.Command = applyToStyle(base.StatusBar.Command, fn)

	// BuildStatus
	t.BuildStatus.Running = applyToStyle(base.BuildStatus.Running, fn)
	t.BuildStatus.Success = applyToStyle(base.BuildStatus.Success, fn)
	t.BuildStatus.Failed = applyToStyle(base.BuildStatus.Failed, fn)
	t.BuildStatus.Aborted = applyToStyle(base.BuildStatus.Aborted, fn)
	t.BuildStatus.Unstable = applyToStyle(base.BuildStatus.Unstable, fn)

	// ProgressBar
	t.ProgressBar.Filled = applyToStyle(base.ProgressBar.Filled, fn)
	t.ProgressBar.Empty = applyToStyle(base.ProgressBar.Empty, fn)
	t.ProgressBar.Overrun = applyToStyle(base.ProgressBar.Overrun, fn)
	t.ProgressBar.FilledText = applyToStyle(base.ProgressBar.FilledText, fn)
	t.ProgressBar.EmptyText = applyToStyle(base.ProgressBar.EmptyText, fn)
	t.ProgressBar.OverrunText = applyToStyle(base.ProgressBar.OverrunText, fn)
	t.ProgressBar.SelFilled = applyToStyle(base.ProgressBar.SelFilled, fn)
	t.ProgressBar.SelEmpty = applyToStyle(base.ProgressBar.SelEmpty, fn)
	t.ProgressBar.SelOverrun = applyToStyle(base.ProgressBar.SelOverrun, fn)
	t.ProgressBar.SelFilledText = applyToStyle(base.ProgressBar.SelFilledText, fn)
	t.ProgressBar.SelEmptyText = applyToStyle(base.ProgressBar.SelEmptyText, fn)
	t.ProgressBar.SelOverrunText = applyToStyle(base.ProgressBar.SelOverrunText, fn)

	// Log
	t.Log.Normal = applyToStyle(base.Log.Normal, fn)
	t.Log.Dim = applyToStyle(base.Log.Dim, fn)
	t.Log.Error = applyToStyle(base.Log.Error, fn)
	t.Log.Warning = applyToStyle(base.Log.Warning, fn)
	t.Log.Trunc = applyToStyle(base.Log.Trunc, fn)

	// Stage
	t.Stage.GhostDim = applyToStyle(base.Stage.GhostDim, fn)

	// Weather
	t.Weather.Sun = applyToStyle(base.Weather.Sun, fn)
	t.Weather.Unstable = applyToStyle(base.Weather.Unstable, fn)
	t.Weather.Storm = applyToStyle(base.Weather.Storm, fn)
	t.Weather.None = applyToStyle(base.Weather.None, fn)

	// NavTag
	t.NavTag.Active = applyToStyle(base.NavTag.Active, fn)
	t.NavTag.Ancestor = applyToStyle(base.NavTag.Ancestor, fn)

	// Popup
	t.Popup.Title = applyToStyle(base.Popup.Title, fn)
	t.Popup.Accent = applyToStyle(base.Popup.Accent, fn)
	t.Popup.Hint = applyToStyle(base.Popup.Hint, fn)
	t.Popup.Label = applyToStyle(base.Popup.Label, fn)
	t.Popup.Desc = applyToStyle(base.Popup.Desc, fn)
	t.Popup.Normal = applyToStyle(base.Popup.Normal, fn)

	// PanelBorder
	t.PanelBorder = applyToStyle(base.PanelBorder, fn)

	// Border: no colors stored in lipgloss.Border — leave untouched.

	return t
}
