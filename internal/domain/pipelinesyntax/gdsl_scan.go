package pipelinesyntax

// scanState enumerates the explicit lexer states used by scanToBlockBoundary.
// The original implementation tracked the same information through two
// booleans (inSingle / inTriple) toggled inside a switch — making transitions
// implicit. Named states make the FSM obvious and individually testable.
type scanState int

const (
	stateDefault scanState = iota // outside any string literal
	stateSingle                   // inside a single-quoted '...' literal
	stateTriple                   // inside a triple-quoted '''...''' literal
)

// scanCtx is the running state of the boundary scanner. Each step* function
// reads the byte at i, mutates ctx, and returns the new index plus an
// optional "boundary found" position. When boundary >= 0, the scan stops.
type scanCtx struct {
	state        scanState
	depthParen   int
	depthBracket int
	depthBrace   int
}

// stepTriple handles bytes while inside a triple-quoted string.
// Returns the new index. Transitions: stateTriple -> stateDefault on ”'.
func stepTriple(src string, i int, ctx *scanCtx) int {
	if i+2 < len(src) && src[i] == '\'' && src[i+1] == '\'' && src[i+2] == '\'' {
		ctx.state = stateDefault
		return i + 3
	}
	return i + 1
}

// stepSingle handles bytes while inside a single-quoted string.
// Honours backslash escapes. Transitions: stateSingle -> stateDefault on '.
func stepSingle(src string, i int, ctx *scanCtx) int {
	c := src[i]
	if c == '\\' && i+1 < len(src) {
		return i + 2 // skip escaped char
	}
	if c == '\'' {
		ctx.state = stateDefault
	}
	return i + 1
}

// stepDefault handles bytes outside any string. Tracks bracket depth and
// detects the top-level `}` boundary. Returns (newIndex, boundaryPos).
// boundaryPos >= 0 means the scan should stop and return that position.
func stepDefault(src string, i int, ctx *scanCtx) (int, int) {
	// Triple-quote opening takes precedence over single-quote opening.
	if i+2 < len(src) && src[i] == '\'' && src[i+1] == '\'' && src[i+2] == '\'' {
		ctx.state = stateTriple
		return i + 3, -1
	}
	switch src[i] {
	case '\'':
		ctx.state = stateSingle
	case '(':
		ctx.depthParen++
	case ')':
		ctx.depthParen--
	case '[':
		ctx.depthBracket++
	case ']':
		ctx.depthBracket--
	case '{':
		ctx.depthBrace++
	case '}':
		if ctx.depthParen == 0 && ctx.depthBracket == 0 && ctx.depthBrace == 0 {
			return i + 1, i
		}
		ctx.depthBrace--
	}
	return i + 1, -1
}
