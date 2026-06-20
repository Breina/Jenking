package component

import (
	"strings"

	"github.com/Breina/Jenking/internal/tui/theme"
)

// navChains declares, per view type, the full left-to-right ancestor trail of
// navigation tags ending in the active view. The labels are exactly what is
// rendered; the last entry is the active tag, the rest are dimmed ancestors.
//
// This replaces the old single linear hierarchy + alternates model: each view
// owns its trail, so sibling leaves at the same depth can have different
// parents (e.g. "stagelog" sits under "stages", "artifact" under "artifacts")
// rather than all collapsing onto one canonical chain.
var navChains = map[string][]string{
	"jobs":      {"jobs"},
	"builds":    {"jobs", "builds"},
	"stages":    {"jobs", "builds", "stages"},
	"stagelog":  {"jobs", "builds", "stages", "log"},
	"matrix":    {"jobs", "builds", "stages", "matrix"},
	"tests":     {"jobs", "builds", "tests"},
	"describe":  {"jobs", "builds", "describe"},
	"log":       {"jobs", "builds", "log"},
	"artifacts": {"jobs", "builds", "artifacts"},
	"artifact":  {"jobs", "builds", "artifacts", "file"},
}

// NavTags renders k9s-style navigation tags at the bottom of the TUI.
type NavTags struct {
	theme    theme.Theme
	viewType string
	rooted   bool     // true = root-scoped view, show only the active tag (no ancestors)
	chain    []string // explicit trail override (context-dependent views); nil = derive from viewType
}

// NewNavTags creates a new NavTags component.
func NewNavTags(t theme.Theme) NavTags {
	return NavTags{theme: t, viewType: "jobs"}
}

// SetTheme updates the theme used for rendering.
func (n *NavTags) SetTheme(t theme.Theme) { n.theme = t }

// SetViewType sets the current view's ViewType (e.g. "stagelog", "artifact").
func (n *NavTags) SetViewType(vt string) { n.viewType = vt }

// SetRooted marks whether the current view is root-scoped (e.g. builds(*)).
// Root-scoped views show only the active tag, no ancestors.
func (n *NavTags) SetRooted(r bool) { n.rooted = r }

// SetChain overrides the derived trail with an explicit one (used by views
// whose ancestor chain depends on where they were opened, e.g. metadata). Pass
// nil to fall back to the viewType-derived chain.
func (n *NavTags) SetChain(chain []string) { n.chain = chain }

// chainFor returns the ancestor trail for a view type, falling back to a lone
// tag bearing the view's own name for types without a declared chain.
func chainFor(vt string) []string {
	if vt == "" {
		vt = "jobs"
	}
	if chain, ok := navChains[vt]; ok {
		return chain
	}
	return []string{vt}
}

// NavChainFor returns the ancestor trail for a view type. Exposed so callers
// can build a context-dependent chain (parent trail + own tag).
func NavChainFor(vt string) []string { return chainFor(vt) }

// View renders the navigation tag line.
func (n NavTags) View() string {
	// An explicit chain override always renders in full (no root truncation).
	if len(n.chain) > 0 {
		return renderTags(n.theme, n.chain, 0)
	}
	chain := chainFor(n.viewType)

	// Root-scoped views (e.g. builds(*)) show only the active (last) tag.
	start := 0
	if n.rooted {
		start = len(chain) - 1
	}
	return renderTags(n.theme, chain, start)
}

func renderTags(t theme.Theme, chain []string, start int) string {
	var tags []string
	for i := start; i < len(chain); i++ {
		if i == len(chain)-1 {
			tags = append(tags, t.NavTag.Active.Render("<"+chain[i]+">"))
		} else {
			tags = append(tags, t.NavTag.Ancestor.Render("<"+chain[i]+">"))
		}
	}
	return " " + strings.Join(tags, " ")
}
