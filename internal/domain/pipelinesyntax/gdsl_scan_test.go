package pipelinesyntax

import "testing"

// Tests target the explicit-state scanner used by splitGDSLEntries.
// Each case exercises a different transition through the FSM
// (default <-> single <-> triple) and bracket-depth tracking.

func TestScanToBlockBoundary_PlainBraceAtDepth0(t *testing.T) {
	src := "name: 'X', type: 'Y' }"
	got := scanToBlockBoundary(src, 0, len(src))
	if got != len(src)-1 {
		t.Fatalf("expected boundary at the `}`, got %d (src len %d)", got, len(src))
	}
}

func TestScanToBlockBoundary_BraceInsideSingleQuoteIgnored(t *testing.T) {
	// The `}` is inside a single-quoted string and must not terminate the scan.
	src := "name: 'a } b', type: 'Y' }"
	got := scanToBlockBoundary(src, 0, len(src))
	if got != len(src)-1 {
		t.Fatalf("brace inside single-quote leaked: got %d, want %d", got, len(src)-1)
	}
}

func TestScanToBlockBoundary_BraceInsideTripleQuoteIgnored(t *testing.T) {
	// Triple-quote opens before single-quote rules apply; braces inside are skipped.
	src := "doc: '''nested } stuff''' }"
	got := scanToBlockBoundary(src, 0, len(src))
	if got != len(src)-1 {
		t.Fatalf("brace inside triple-quote leaked: got %d, want %d", got, len(src)-1)
	}
}

func TestScanToBlockBoundary_EscapedQuoteStaysInSingle(t *testing.T) {
	// `\'` inside a single-quoted string must not close it.
	src := `name: 'it\'s ok } still in string', x }`
	got := scanToBlockBoundary(src, 0, len(src))
	if got != len(src)-1 {
		t.Fatalf("escaped quote leaked state: got %d, want %d", got, len(src)-1)
	}
}

func TestScanToBlockBoundary_BraceInsideNestedBracketsDecrements(t *testing.T) {
	// A `}` at non-zero brace depth must NOT terminate; the scan continues
	// until depth returns to zero.
	src := "params: [a:'b', c:{inner}] }"
	got := scanToBlockBoundary(src, 0, len(src))
	if got != len(src)-1 {
		t.Fatalf("got %d, want %d (final brace)", got, len(src)-1)
	}
}

func TestScanToBlockBoundary_NoBoundaryReturnsEnd(t *testing.T) {
	src := "name: 'X', type: 'Y'"
	got := scanToBlockBoundary(src, 0, len(src))
	if got != len(src) {
		t.Fatalf("expected end (%d), got %d", len(src), got)
	}
}

func TestScanToBlockBoundary_StartOffsetRespected(t *testing.T) {
	// Boundary scanning should ignore the prefix before `start`.
	prefix := "method name: "
	rest := "'X' }"
	src := prefix + rest
	got := scanToBlockBoundary(src, len(prefix), len(src))
	if got != len(src)-1 {
		t.Fatalf("start-offset path: got %d, want %d", got, len(src)-1)
	}
}

func TestStepDefault_BracketDepthTransitions(t *testing.T) {
	ctx := scanCtx{state: stateDefault}
	src := "([{"
	for i := 0; i < len(src); i++ {
		_, b := stepDefault(src, i, &ctx)
		if b >= 0 {
			t.Fatalf("unexpected boundary at %d", i)
		}
	}
	if ctx.depthParen != 1 || ctx.depthBracket != 1 || ctx.depthBrace != 1 {
		t.Fatalf("depths wrong: %+v", ctx)
	}
}

func TestStepDefault_TripleQuoteTransitionsToTriple(t *testing.T) {
	ctx := scanCtx{state: stateDefault}
	next, _ := stepDefault("'''", 0, &ctx)
	if ctx.state != stateTriple {
		t.Fatalf("expected stateTriple, got %v", ctx.state)
	}
	if next != 3 {
		t.Fatalf("expected next=3, got %d", next)
	}
}

func TestStepDefault_SingleQuoteTransitionsToSingle(t *testing.T) {
	ctx := scanCtx{state: stateDefault}
	next, _ := stepDefault("'x", 0, &ctx)
	if ctx.state != stateSingle {
		t.Fatalf("expected stateSingle, got %v", ctx.state)
	}
	if next != 1 {
		t.Fatalf("expected next=1, got %d", next)
	}
}

func TestStepSingle_EscapeSkipsNext(t *testing.T) {
	ctx := scanCtx{state: stateSingle}
	next := stepSingle(`\'`, 0, &ctx)
	if ctx.state != stateSingle {
		t.Fatalf("escape should keep stateSingle, got %v", ctx.state)
	}
	if next != 2 {
		t.Fatalf("escape should advance by 2, got %d", next)
	}
}

func TestStepSingle_ClosingQuoteReturnsDefault(t *testing.T) {
	ctx := scanCtx{state: stateSingle}
	next := stepSingle("'", 0, &ctx)
	if ctx.state != stateDefault {
		t.Fatalf("expected stateDefault after closing quote, got %v", ctx.state)
	}
	if next != 1 {
		t.Fatalf("expected next=1, got %d", next)
	}
}

func TestStepTriple_ClosingTripleReturnsDefault(t *testing.T) {
	ctx := scanCtx{state: stateTriple}
	next := stepTriple("'''", 0, &ctx)
	if ctx.state != stateDefault {
		t.Fatalf("expected stateDefault, got %v", ctx.state)
	}
	if next != 3 {
		t.Fatalf("expected next=3, got %d", next)
	}
}

func TestStepTriple_NonClosingByteAdvancesOne(t *testing.T) {
	ctx := scanCtx{state: stateTriple}
	next := stepTriple("x'''", 0, &ctx)
	if ctx.state != stateTriple {
		t.Fatalf("non-closing byte should keep stateTriple")
	}
	if next != 1 {
		t.Fatalf("expected next=1, got %d", next)
	}
}
