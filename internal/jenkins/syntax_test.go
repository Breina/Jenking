package jenkins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Breina/Jenking/internal/jenkins/pipelinesyntax"
)

func TestApplyUserGDSL_AttachesToExistingGlobal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cumuli.gdsl"), []byte(`
contributor(context(ctype: 'maven')) {
    method(name: 'setVersion', type: 'Object', params: [version:'java.lang.String'], doc: 'Set Maven version')
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := UserGDSLDir
	UserGDSLDir = dir
	defer func() { UserGDSLDir = prev }()

	sym := &pipelinesyntax.Symbols{
		Globals: []pipelinesyntax.GlobalVar{{Name: "maven", Doc: "Maven helpers"}},
	}
	ApplyUserGDSL(sym)

	if len(sym.Globals) != 1 || len(sym.Globals[0].Members) != 1 {
		t.Fatalf("expected 1 member attached, got %+v", sym.Globals)
	}
	if sym.Globals[0].Members[0].Name != "setVersion" {
		t.Errorf("wrong member: %+v", sym.Globals[0].Members[0])
	}
}

func TestApplyUserGDSL_AddsNewReceiver(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cumuli.gdsl"), []byte(`
contributor(context(ctype: 'cumuli')) {
    method(name: 'deploy', type: 'Object', params: [env:'java.lang.String'], doc: 'Deploy')
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := UserGDSLDir
	UserGDSLDir = dir
	defer func() { UserGDSLDir = prev }()

	sym := &pipelinesyntax.Symbols{} // no globals from Jenkins
	ApplyUserGDSL(sym)

	if len(sym.Globals) != 1 || sym.Globals[0].Name != "cumuli" || len(sym.Globals[0].Members) != 1 {
		t.Fatalf("expected synthesised cumuli global, got %+v", sym.Globals)
	}
}

func TestApplyUserGDSL_IdempotentOnRepeatCalls(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cumuli.gdsl"), []byte(`
contributor(context(ctype: 'maven')) {
    method(name: 'goal', type: 'Object', params: [name:'java.lang.String'], doc: 'Run')
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := UserGDSLDir
	UserGDSLDir = dir
	defer func() { UserGDSLDir = prev }()

	sym := &pipelinesyntax.Symbols{
		Globals: []pipelinesyntax.GlobalVar{{Name: "maven"}},
	}
	ApplyUserGDSL(sym)
	ApplyUserGDSL(sym)
	ApplyUserGDSL(sym)

	if len(sym.Globals[0].Members) != 1 {
		t.Errorf("expected 1 member after 3 applies, got %d: %+v",
			len(sym.Globals[0].Members), sym.Globals[0].Members)
	}
}

func TestApplyUserGDSL_PicksUpEditedFile(t *testing.T) {
	dir := t.TempDir()
	gdsl := filepath.Join(dir, "lib.gdsl")
	prev := UserGDSLDir
	UserGDSLDir = dir
	defer func() { UserGDSLDir = prev }()

	sym := &pipelinesyntax.Symbols{
		Globals: []pipelinesyntax.GlobalVar{{Name: "maven"}},
	}

	if err := os.WriteFile(gdsl, []byte(`
contributor(context(ctype: 'maven')) {
    method(name: 'goal', type: 'Object', params: [name:'java.lang.String'], doc: '')
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ApplyUserGDSL(sym)
	if len(sym.Globals[0].Members) != 1 {
		t.Fatalf("first apply: expected 1 member, got %+v", sym.Globals[0].Members)
	}

	// Edit the file: rename goal → tag.
	if err := os.WriteFile(gdsl, []byte(`
contributor(context(ctype: 'maven')) {
    method(name: 'tag', type: 'Object', params: [name:'java.lang.String'], doc: '')
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ApplyUserGDSL(sym)
	if len(sym.Globals[0].Members) != 1 || sym.Globals[0].Members[0].Name != "tag" {
		t.Errorf("edit not picked up; got %+v", sym.Globals[0].Members)
	}
}
