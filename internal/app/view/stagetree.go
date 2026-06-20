package view

import (
	"strings"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// pipelineTreePrefix wraps buildTreePrefix with an extra root level so all
// stages appear as children of the synthetic Pipeline row.
func pipelineTreePrefix(stages []jmodel.Stage, idx int) string {
	s := stages[idx]

	// Is this the last depth-0 stage (or nested under the last depth-0)?
	lastTopLevel := true
	for j := idx + 1; j < len(stages); j++ {
		if stages[j].Depth == 0 {
			lastTopLevel = false
			break
		}
	}

	var rootConnector string
	if s.Depth == 0 {
		if lastTopLevel {
			rootConnector = "└─"
		} else {
			rootConnector = "├─"
		}
		return rootConnector
	}

	// For nested stages, prepend the Pipeline continuation line.
	var pipelineCont string
	if lastTopLevel {
		pipelineCont = "  " // Pipeline branch ended
	} else {
		pipelineCont = "│ " // Pipeline branch continues
	}
	return pipelineCont + buildTreePrefix(stages, idx)
}

// parentStageName returns the name of the nearest ancestor stage (one depth
// shallower) preceding idx, or "" if the stage is top-level. Used to give the
// breadcrumb a disambiguating parent for non-unique leaf names.
func parentStageName(stages []jmodel.Stage, idx int) string {
	if idx < 0 || idx >= len(stages) {
		return ""
	}
	parentDepth := stages[idx].Depth - 1
	if parentDepth < 0 {
		return ""
	}
	for j := idx - 1; j >= 0; j-- {
		if stages[j].Depth == parentDepth {
			return stages[j].Name
		}
		if stages[j].Depth < parentDepth {
			return ""
		}
	}
	return ""
}

// buildTreePrefix generates tree-drawing characters for a stage.
// Parallel branches use heavy box-drawing (┃, ┣━, ┗━) to visually
// distinguish them from sequential branches (│, ├─, └─).
func buildTreePrefix(stages []jmodel.Stage, idx int) string {
	s := stages[idx]
	if s.Depth == 0 {
		return ""
	}
	isLast := !hasSiblingAfter(stages, idx, s.Depth)
	parentIsParallel := isParentParallel(stages, idx)

	var buf strings.Builder
	for d := 1; d < s.Depth; d++ {
		if hasSiblingAfter(stages, idx, d) {
			if isAncestorParentParallel(stages, idx, d) {
				buf.WriteString("┃ ")
			} else {
				buf.WriteString("│ ")
			}
		} else {
			buf.WriteString("  ")
		}
	}
	buf.WriteString(treeBranchGlyph(parentIsParallel, isLast))
	return buf.String()
}

// hasSiblingAfter returns true when stages[idx+1:] still contains a stage at
// exactly the given depth before falling below it — meaning the row at idx
// is not the last sibling at that depth.
func hasSiblingAfter(stages []jmodel.Stage, idx, depth int) bool {
	for j := idx + 1; j < len(stages); j++ {
		if stages[j].Depth < depth {
			return false
		}
		if stages[j].Depth == depth {
			return true
		}
	}
	return false
}

// treeBranchGlyph returns the leaf box-drawing glyph for a stage row. Parallel
// branches use the heavy variant (┗━/┣━) to set them apart from sequential
// branches (└─/├─). isLast picks the leaf-terminator over the tee.
func treeBranchGlyph(parallel, isLast bool) string {
	switch {
	case parallel && isLast:
		return "┗━"
	case parallel:
		return "┣━"
	case isLast:
		return "└─"
	default:
		return "├─"
	}
}

// isParentParallel checks if the direct parent stage of stages[idx] is parallel.
func isParentParallel(stages []jmodel.Stage, idx int) bool {
	for j := idx - 1; j >= 0; j-- {
		if stages[j].Depth < stages[idx].Depth {
			return stages[j].Depth == stages[idx].Depth-1 && stages[j].Parallel
		}
	}
	return false
}

// isAncestorParentParallel checks if the ancestor at the given depth
// has a parallel parent.
func isAncestorParentParallel(stages []jmodel.Stage, idx, depth int) bool {
	for j := idx - 1; j >= 0; j-- {
		if stages[j].Depth < depth {
			break
		}
		if stages[j].Depth == depth {
			return isParentParallel(stages, j)
		}
	}
	return false
}
