package widget

// VimPolicy gates the per-build vim runtime + validation behaviour. Set once
// at startup from preferences.vim_integration in config.yaml. Read directly
// by DescribeView to avoid threading a 4th parameter through eight call
// sites of NewDescribeView.
//
// Default is "everything on" so users get the rich editor experience out of
// the box. Set via SetVimPolicy() during app init if config disagrees.
type VimPolicy struct {
	Enabled         bool
	PrefetchSymbols bool
	ValidateOnSave  bool
}

var vimPolicy = VimPolicy{
	Enabled:         true,
	PrefetchSymbols: true,
	ValidateOnSave:  true,
}

// SetVimPolicy overrides the package-level vim integration flags. Safe to
// call once at startup before any DescribeView is constructed.
func SetVimPolicy(p VimPolicy) {
	vimPolicy = p
}

// VimPolicySnapshot returns the current policy (useful for tests).
func VimPolicySnapshot() VimPolicy { return vimPolicy }
