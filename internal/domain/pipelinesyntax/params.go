package pipelinesyntax

// AttachParamsGlobal appends members to the built-in `params` global, creating
// it if the symbol set does not already expose one. Pure: the caller builds
// the Member values from job parameter definitions (that conversion lives in
// the app layer, which may import both jmodel and this package — jmodel already
// imports this package, so the dependency cannot run the other way).
func AttachParamsGlobal(sym *Symbols, members []Member) {
	if sym == nil || len(members) == 0 {
		return
	}
	for i := range sym.Globals {
		if sym.Globals[i].Name == "params" {
			sym.Globals[i].Members = append(sym.Globals[i].Members, members...)
			return
		}
	}
	sym.Globals = append(sym.Globals, GlobalVar{
		Name: "params", Doc: "Build parameters", Members: members,
	})
}
