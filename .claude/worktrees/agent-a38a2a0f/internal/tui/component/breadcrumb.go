package component

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/brecht/jenkins-tui/internal/tui/theme"
)

// BreadcrumbSegment is used by the view package (via view.BreadcrumbSegment) and
// rendered directly by the component package.
type BreadcrumbSegment struct {
	ViewType string
	Context  []BreadcrumbPart
}

// BreadcrumbPart is one piece of the context portion of a breadcrumb segment.
type BreadcrumbPart struct {
	Text       string
	IsBuildNum bool
	Separator  string // ":" instead of "/" before this part; empty = default "/"
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

	title := st.ViewType.Render(s.ViewType)

	if len(s.Context) > 0 {
		title += st.Paren.Render("(")
		for i, part := range s.Context {
			if i > 0 {
				sep := "/"
				if part.Separator != "" {
					sep = part.Separator
				}
				title += st.Paren.Render(sep)
			}
			text := shortenPart(part.Text, maxContextPartWidth)
			if part.IsBuildNum {
				title += st.BuildNum.Render("#" + text)
			} else {
				title += st.Context.Render(text)
			}
		}
		title += st.Paren.Render(")")
	} else {
		title += st.Paren.Render("()")
	}

	if b.tail {
		title += st.Count.Render("[tail]")
	} else if b.count > 0 {
		title += st.Count.Render(fmt.Sprintf("[%d]", b.count))
	}

	if b.searchAnnotation != "" {
		title += " " + st.Badge.Render(" /"+b.searchAnnotation+" ")
	}

	return title
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
		title += lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(fmt.Sprintf(" [%d]", b.count))
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
