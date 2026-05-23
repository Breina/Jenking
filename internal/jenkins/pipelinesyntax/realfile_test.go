package pipelinesyntax

import (
	"os"
	"testing"
)

// TestParseUserGDSL_RealCumulusFile runs the parser against the actual
// /home/brecht/.config/jenking/symbols/Cumulus-test.gdsl file if it exists
// on the developer's machine. Skipped in CI / on other machines.
func TestParseUserGDSL_RealCumulusFile(t *testing.T) {
	const path = "/home/brecht/.config/jenking/symbols/Cumulus-test.gdsl"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("real file not present (%s); skipping", path)
	}
	got := ParseUserGDSL(string(data))
	if len(got) == 0 {
		t.Fatalf("real Cumulus file parsed to 0 receivers")
	}
	t.Logf("parsed %d receivers:", len(got))
	for recv, ms := range got {
		t.Logf("  %s — %d members", recv, len(ms))
	}
	// Sanity: known-good receivers from the file.
	for _, want := range []string{"maven", "git", "helm", "buildkit"} {
		if _, ok := got[want]; !ok {
			t.Errorf("expected receiver %q not found in parsed result", want)
		}
	}
	// Sanity: maven should have setVersion + goal.
	mavenNames := map[string]bool{}
	for _, m := range got["maven"] {
		mavenNames[m.Name] = true
	}
	for _, want := range []string{"setVersion", "goal", "version", "podSpec"} {
		if !mavenNames[want] {
			t.Errorf("expected maven member %q not found", want)
		}
	}
}
