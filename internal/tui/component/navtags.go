package component

import (
	"strings"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// navHierarchy defines the canonical left-to-right order of navigation tags.
var navHierarchy = []string{"jobs", "builds", "stages", "log"}

// navAlternates maps view types that share a position with a canonical entry.
var navAlternates = map[string]string{
	"tests":     "stages",
	"artifacts": "stages",
	"matrix":    "log",
	"describe":  "stages",
}

// NavTags renders k9s-style navigation tags at the bottom of the TUI.
type NavTags struct {
	theme    theme.Theme
	viewType string
	rooted   bool // true = root-scoped view, show only the active tag (no ancestors)
}

// NewNavTags creates a new NavTags component.
func NewNavTags(t theme.Theme) NavTags {
	return NavTags{theme: t, viewType: "jobs"}
}

// SetTheme updates the theme used for rendering.
func (n *NavTags) SetTheme(t theme.Theme) { n.theme = t }

// SetViewType sets the current view's ViewType (e.g. "builds+running").
func (n *NavTags) SetViewType(vt string) { n.viewType = vt }

// SetRooted marks whether the current view is root-scoped (e.g. builds(*)).
// Root-scoped views show only the active tag, no ancestors.
func (n *NavTags) SetRooted(r bool) { n.rooted = r }

// baseType returns the bare view name (kept for backward compatibility).
func baseType(vt string) string {
	return vt
}

// View renders the navigation tag line.
func (n NavTags) View() string {
	base := baseType(n.viewType)
	if base == "" {
		base = "jobs"
	}

	// Resolve alternate to its canonical position.
	canonical := base
	if alt, ok := navAlternates[base]; ok {
		canonical = alt
	}

	// Find the index of the current view in the hierarchy.
	activeIdx := -1
	for i, h := range navHierarchy {
		if h == canonical {
			activeIdx = i
			break
		}
	}
	if activeIdx < 0 {
		activeIdx = 0
	}

	// Root-scoped views (e.g. builds(*)) show only the active tag.
	startIdx := 0
	if n.rooted {
		startIdx = activeIdx
	}

	// Build the tag trail from startIdx to activeIdx.
	// Replace the entry at activeIdx with the actual base type if it differs
	// (e.g., "tests" instead of "log").
	var tags []string
	for i := startIdx; i <= activeIdx; i++ {
		label := navHierarchy[i]
		if i == activeIdx && base != canonical {
			label = base
		}

		var rendered string
		if i == activeIdx {
			rendered = n.theme.NavTag.Active.Render("<" + n.viewType + ">")
		} else {
			rendered = n.theme.NavTag.Ancestor.Render("<" + label + ">")
		}
		tags = append(tags, rendered)
	}

	return " " + strings.Join(tags, " ")
}
