package component

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// BreadcrumbSegment mirrors view.BreadcrumbSegment so the component package
// doesn't import the view package.
type BreadcrumbSegment struct {
	ViewType      string
	Context       []BreadcrumbPart
	Running       bool             // filter active — rendered as "running " prefix
	Mine          bool             // filter active — rendered as "my " prefix
	Failed        bool             // filter active — rendered as "failed " prefix
	ResolvedParts []BreadcrumbPart // resolved #last info, shown after " → " arrow
}

// BreadcrumbPart is one piece of the context portion of a breadcrumb segment.
type BreadcrumbPart struct {
	Text       string
	IsBuildNum bool
	// NoHashPrefix suppresses the leading "#" for an IsBuildNum part. Used when
	// the build is identified by a custom display name rather than a number.
	NoHashPrefix bool
	Separator    string // ":" instead of "/" before this part; empty = default "/"
}

const maxContextPartWidth = 30

// Breadcrumb renders the navigation path as the panel title.
type Breadcrumb struct {
	theme            theme.Theme
	segments         []string           // legacy
	segment          *BreadcrumbSegment // new k9s-style
	count            int
	tail             bool // show [tail] instead of [count]
	searchAnnotation string
}

// NewBreadcrumb creates a new breadcrumb component.
func NewBreadcrumb(t theme.Theme) Breadcrumb {
	return Breadcrumb{
		theme:    t,
		segments: []string{"Dashboard"},
	}
}

// SetTheme updates the theme used for rendering.
func (b *Breadcrumb) SetTheme(t theme.Theme) {
	b.theme = t
}

// SetSegments updates the breadcrumb path (legacy).
func (b *Breadcrumb) SetSegments(segments []string) {
	b.segments = segments
}

// SetSegment sets the k9s-style breadcrumb segment.
func (b *Breadcrumb) SetSegment(s *BreadcrumbSegment) {
	b.segment = s
}

// SetCount sets the item count shown next to the title.
func (b *Breadcrumb) SetCount(n int) {
	b.count = n
}

// SetTail enables [tail] badge instead of [count].
func (b *Breadcrumb) SetTail(tail bool) {
	b.tail = tail
}

// SetSearchAnnotation sets the search badge shown after the breadcrumb.
func (b *Breadcrumb) SetSearchAnnotation(s string) {
	b.searchAnnotation = s
}

// View renders the breadcrumb as a title string (used inside bordered content panel).
func (b Breadcrumb) View() string {
	if b.segment != nil {
		return b.renderSegment()
	}
	return b.renderLegacy()
}

// renderSegment renders the new k9s-style breadcrumb.
func (b Breadcrumb) renderSegment() string {
	s := b.segment
	st := b.theme.Breadcrumb

	title := b.renderFilterPrefix() + st.ViewType.Render(s.ViewType)
	title += b.renderContext()
	title += b.renderCount()

	if b.searchAnnotation != "" {
		title += " " + st.Badge.Render(" /"+b.searchAnnotation+" ")
	}
	return title
}

// renderFilterPrefix builds the "my running failed " filter prefix.
func (b Breadcrumb) renderFilterPrefix() string {
	s := b.segment
	activeColor := b.theme.Search.Match.GetBackground()
	filterStyle := lipgloss.NewStyle().Foreground(activeColor).Bold(true)
	var prefix string
	if s.Mine {
		prefix += filterStyle.Render("my") + " "
	}
	if s.Running {
		prefix += filterStyle.Render("running") + " "
	}
	if s.Failed {
		prefix += filterStyle.Render("failed") + " "
	}
	return prefix
}

// renderContext builds the "(part/part → resolved)" portion of the title.
func (b Breadcrumb) renderContext() string {
	s := b.segment
	st := b.theme.Breadcrumb
	if len(s.Context) == 0 {
		return st.Paren.Render("()")
	}
	out := st.Paren.Render("(") + b.renderParts(s.Context)
	if len(s.ResolvedParts) > 0 {
		out += st.Paren.Render(" → ") + b.renderParts(s.ResolvedParts)
	}
	out += st.Paren.Render(")")
	return out
}

// renderParts renders a sequence of BreadcrumbParts joined by separators.
func (b Breadcrumb) renderParts(parts []BreadcrumbPart) string {
	st := b.theme.Breadcrumb
	var out string
	for i, part := range parts {
		if i > 0 {
			sep := "/"
			if part.Separator != "" {
				sep = part.Separator
			}
			out += st.Paren.Render(sep)
		}
		text := shortenPart(part.Text, maxContextPartWidth)
		if part.IsBuildNum {
			prefix := "#"
			if part.NoHashPrefix {
				prefix = ""
			}
			out += st.BuildNum.Render(prefix + text)
		} else {
			out += st.Context.Render(text)
		}
	}
	return out
}

// renderCount builds the "[tail]" or "[N]" trailing badge.
func (b Breadcrumb) renderCount() string {
	st := b.theme.Breadcrumb
	if b.tail {
		return st.Count.Render("[tail]")
	}
	if b.count > 0 {
		return st.Count.Render(fmt.Sprintf("[%d]", b.count))
	}
	return ""
}

// renderLegacy renders the old trail-style breadcrumb.
func (b Breadcrumb) renderLegacy() string {
	sep := b.theme.Breadcrumb.Separator.Render(" > ")
	parts := make([]string, len(b.segments))
	for i, s := range b.segments {
		parts[i] = b.theme.Breadcrumb.Segment.Render(s)
	}
	title := strings.Join(parts, sep)
	if b.count > 0 {
		title += b.theme.Breadcrumb.Count.Render(fmt.Sprintf(" [%d]", b.count))
	}
	if b.searchAnnotation != "" {
		title += " " + b.theme.Breadcrumb.Badge.Render(" /"+b.searchAnnotation+" ")
	}
	return title
}

// shortenPart shortens a context part to fit maxWidth. If the text contains
// a slash and exceeds maxWidth, retain only the part after the last slash.
// If still too long, front-truncate with "…".
func shortenPart(s string, maxWidth int) string {
	runes := []rune(s)
	if len(runes) <= maxWidth {
		return s
	}
	// If the text contains a slash, try extracting after last slash.
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		short := s[idx+1:]
		if len([]rune(short)) <= maxWidth {
			return short
		}
		runes = []rune(short)
	}
	// Front-truncate: keep the last (maxWidth-1) runes, prefix with …
	return "…" + string(runes[len(runes)-(maxWidth-1):])
}
