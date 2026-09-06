package pipelinesyntax

import "testing"

func TestAttachParamsGlobal_CreatesGlobal(t *testing.T) {
	sym := &Symbols{}
	AttachParamsGlobal(sym, []Member{{Name: "TAG", Signature: "params.TAG : string"}})
	if len(sym.Globals) != 1 || sym.Globals[0].Name != "params" {
		t.Fatalf("expected a new params global, got %+v", sym.Globals)
	}
	if len(sym.Globals[0].Members) != 1 || sym.Globals[0].Members[0].Name != "TAG" {
		t.Errorf("member not attached: %+v", sym.Globals[0].Members)
	}
}

func TestAttachParamsGlobal_AppendsToExisting(t *testing.T) {
	sym := &Symbols{Globals: []GlobalVar{
		{Name: "env"},
		{Name: "params", Members: []Member{{Name: "OLD"}}},
	}}
	AttachParamsGlobal(sym, []Member{{Name: "NEW"}})
	if len(sym.Globals) != 2 {
		t.Fatalf("should not add a global, got %d", len(sym.Globals))
	}
	p := sym.Globals[1]
	if len(p.Members) != 2 || p.Members[1].Name != "NEW" {
		t.Errorf("expected OLD+NEW members, got %+v", p.Members)
	}
}

func TestAttachParamsGlobal_NoopOnEmpty(t *testing.T) {
	sym := &Symbols{}
	AttachParamsGlobal(sym, nil)
	AttachParamsGlobal(nil, []Member{{Name: "X"}})
	if len(sym.Globals) != 0 {
		t.Errorf("empty inputs must be no-ops, got %+v", sym.Globals)
	}
}
