package view

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Breina/Jenking/internal/domain/pipelinesyntax"
)

//go:embed vimruntime_autoload.vim
var autoloadVimBody string

//go:embed vimruntime_autoload_validate.vim
var autoloadVimValidate string

// vimRuntime materialises a per-build vim runtime directory holding:
//   - after/syntax/groovy.vim   — keyword overlays for Jenkins steps + DSL
//   - plugin/jenking.vim        — omnifunc + completion / quickfix wiring
//
// The directory is layered on top of the user's vimrc via `--cmd 'set
// rtp^=…'` so the user's normal config keeps working untouched.
//
// runtimeDir is also where the in-vim validation daemon (Slice 4) drops its
// sentinel + errorformat files — those filenames are pre-declared here so
// both layers agree.
type vimRuntime struct {
	dir       string // absolute path to the runtime root
	bufferTmp string // sentinel buffer.groovy path (BufWritePost target)
	errorsQF  string // quickfix file the TUI writes back
}

// jenkingCacheDir returns the on-disk root for jenking's editor scratch space.
func jenkingCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "jenking", "vim"), nil
}

// buildRuntimeKey returns a stable, fs-safe directory name for a single
// (jobPath, buildNumber) pair.
func buildRuntimeKey(jobPath string, buildNum int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s#%d", jobPath, buildNum)))
	return hex.EncodeToString(sum[:8])
}

// writeVimRuntime regenerates the per-build runtime directory. Idempotent:
// re-running on the same (jobPath, buildNum, sym) overwrites in place.
//
// sym may be nil — in that case the syntax overlay is empty and the omnifunc
// returns no matches, but vim still launches fine.
func writeVimRuntime(jobPath string, buildNum int, sym *pipelinesyntax.Symbols) (*vimRuntime, error) {
	root, err := jenkingCacheDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, buildRuntimeKey(jobPath, buildNum))
	for _, sub := range [][]string{{"after", "syntax"}, {"plugin"}, {"autoload"}} {
		if err := os.MkdirAll(filepath.Join(append([]string{dir}, sub...)...), 0o700); err != nil {
			return nil, err
		}
	}

	if err := os.WriteFile(
		filepath.Join(dir, "after", "syntax", "groovy.vim"),
		[]byte(renderSyntaxOverlay(sym)), 0o600,
	); err != nil {
		return nil, err
	}

	rt := &vimRuntime{
		dir:       dir,
		bufferTmp: filepath.Join(dir, "buffer.groovy"),
		errorsQF:  filepath.Join(dir, "errors.qf"),
	}
	// `jenking#X` style functions must live under autoload/<name>.vim — vim
	// enforces this (E746). plugin/jenking.vim only wires autocmds + globals.
	if err := os.WriteFile(
		filepath.Join(dir, "autoload", "jenking.vim"),
		[]byte(renderAutoloadVim(sym, rt)), 0o600,
	); err != nil {
		return nil, err
	}
	if err := os.WriteFile(
		filepath.Join(dir, "plugin", "jenking.vim"),
		[]byte(renderPluginVim(rt)), 0o600,
	); err != nil {
		return nil, err
	}
	return rt, nil
}

// writeSymbolMatches emits the step/global/member/receiver match blocks for
// a non-nil symbol set.
func writeSymbolMatches(b *strings.Builder, sym *pipelinesyntax.Symbols) {
	stepNames := make([]string, 0, len(sym.Steps))
	for _, s := range sym.Steps {
		stepNames = append(stepNames, s.Name)
	}
	writeMatchAlternation(b, "jenkingStep", stepNames)

	globalNames := make([]string, 0, len(sym.Globals))
	memberSet := map[string]struct{}{}
	for _, g := range sym.Globals {
		globalNames = append(globalNames, g.Name)
		for _, m := range g.Members {
			memberSet[m.Name] = struct{}{}
		}
	}
	writeMatchAlternation(b, "jenkingGlobal", globalNames)

	writeMemberMatch(b, memberSet)
	writeReceiverMatch(b, sym.Globals)
}

// writeMemberMatch emits the dot-prefixed match for library member names. No
// output when the safe-name set is empty.
func writeMemberMatch(b *strings.Builder, memberSet map[string]struct{}) {
	if len(memberSet) == 0 {
		return
	}
	members := make([]string, 0, len(memberSet))
	for m := range memberSet {
		if isVimKeywordSafe(m) {
			members = append(members, m)
		}
	}
	if len(members) == 0 {
		return
	}
	fmt.Fprintf(b, "syntax match jenkingMember /\\.\\@<=\\<\\(%s\\)\\>/\n",
		strings.Join(members, `\|`))
}

// writeReceiverMatch emits the pre-dot match for receivers (globals that
// have scraped members).
func writeReceiverMatch(b *strings.Builder, globals []pipelinesyntax.GlobalVar) {
	recvNames := make([]string, 0)
	for _, g := range globals {
		if len(g.Members) > 0 && isVimKeywordSafe(g.Name) {
			recvNames = append(recvNames, g.Name)
		}
	}
	if len(recvNames) == 0 {
		return
	}
	fmt.Fprintf(b, "syntax match jenkingReceiver /\\<\\(%s\\)\\>\\ze\\./\n",
		strings.Join(recvNames, `\|`))
}

// renderSyntaxOverlay emits Vim `syntax keyword` declarations + highlight
// links for the build's symbol set. Loaded from `after/syntax/groovy.vim`
// so it stacks on top of vim's builtin groovy syntax (or whichever plugin
// the user has) without replacing it.
func renderSyntaxOverlay(sym *pipelinesyntax.Symbols) string {
	var b strings.Builder
	b.WriteString(`" Generated by jenking — per-build Jenkins symbol overlay.
" Do not edit; regenerated on every edit session.
"
" Loaded as after/syntax/groovy.vim so it runs once vim's builtin groovy
" syntax has finished. We use 'syntax match' rather than 'syntax keyword'
" because the builtin syntax may already have classified names like sh or
" archive under its own group — 'syntax match' overrides regardless of
" pre-existing keyword entries, while 'syntax keyword' silently no-ops in
" that case.

`)

	dsl := pipelinesyntax.DefaultDSLKeywords
	if sym != nil && len(sym.DSLKeywords) > 0 {
		dsl = sym.DSLKeywords
	}
	writeMatchAlternation(&b, "jenkingDSL", dsl)

	if sym != nil {
		writeSymbolMatches(&b, sym)
	}

	// Link to distinctive default groups. Most terminal colorschemes give
	// these visibly different colors, so users don't need a custom theme.
	b.WriteString(`
highlight default link jenkingDSL      Statement
highlight default link jenkingStep     Function
highlight default link jenkingGlobal   Constant
highlight default link jenkingMember   Special
highlight default link jenkingReceiver Type
`)
	return b.String()
}

// writeMatchAlternation emits `syntax match <group> /\<\(a\|b\|c\)\>/` lines.
// Chunked so vim's command-line length cap is never tripped. Empty / unsafe
// names are dropped (those vim regex would either misparse or fail on).
func writeMatchAlternation(b *strings.Builder, group string, names []string) {
	if len(names) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(names))
	const perLine = 80
	row := make([]string, 0, perLine)
	flush := func() {
		if len(row) == 0 {
			return
		}
		fmt.Fprintf(b, "syntax match %s /\\<\\(%s\\)\\>/\n", group, strings.Join(row, `\|`))
		row = row[:0]
	}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || !isVimKeywordSafe(n) {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		row = append(row, n)
		if len(row) >= perLine {
			flush()
		}
	}
	flush()
}

// writeKeywordChunks emits `syntax keyword <group> a b c …` lines, chunked
// to stay under vim's command-line length limits. Empty/duplicate names are
// dropped silently.
// isVimKeywordSafe reports whether a name can be passed to `syntax keyword`
// without escaping (alnum + underscore only).
func isVimKeywordSafe(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// emitVimParamList formats a list of parameter names as a Vim list literal.
func emitVimParamList(params []pipelinesyntax.Param) string {
	if len(params) == 0 {
		return "[]"
	}
	names := make([]string, len(params))
	for i, p := range params {
		names[i] = vimStr(p.Name)
	}
	return "[" + strings.Join(names, ", ") + "]"
}

// emitVimStep appends a single step entry (symbol + param list) to b.
func emitVimStep(b *strings.Builder, s pipelinesyntax.Step) {
	fmt.Fprintf(b, "call add(s:symbols, {'word': %s, 'abbr': %s, 'menu': %s, 'info': %s, 'kind': 'f'})\n",
		vimStr(s.Name), vimStr(s.Name), vimStr(s.Signature()), vimStr(s.Doc),
	)
	// Param info keyed by call name. If multiple overloads exist
	// (e.g. positional + namedParams sh), the LATER entry wins —
	// Jenkins gdsl emits namedParams form last, which has richer info.
	fmt.Fprintf(b, "let s:call_params[%s] = %s\n", vimStr(s.Name), emitVimParamList(s.Params))
}

// emitVimGlobal appends a single global entry plus its members to b.
func emitVimGlobal(b *strings.Builder, g pipelinesyntax.GlobalVar) {
	fmt.Fprintf(b, "call add(s:symbols, {'word': %s, 'abbr': %s, 'menu': %s, 'info': %s, 'kind': 'v'})\n",
		vimStr(g.Name), vimStr(g.Name), vimStr("global"), vimStr(g.Doc),
	)
	if len(g.Members) == 0 {
		return
	}
	fmt.Fprintf(b, "let s:members[%s] = []\n", vimStr(g.Name))
	fmt.Fprintf(b, "call add(s:receivers, %s)\n", vimStr(g.Name))
	for _, m := range g.Members {
		menu := m.Signature
		if menu == "" {
			menu = m.Name
		}
		fmt.Fprintf(b, "call add(s:members[%s], {'word': %s, 'abbr': %s, 'menu': %s, 'info': %s, 'kind': 'm'})\n",
			vimStr(g.Name), vimStr(m.Name), vimStr(m.Name), vimStr(menu), vimStr(m.Doc),
		)
		// Param info under "receiver.method" key for in-call mode.
		fmt.Fprintf(b, "let s:call_params[%s] = %s\n",
			vimStr(g.Name+"."+m.Name), emitVimParamList(m.Params))
	}
}

// emitVimSymbols emits all step + global declarations for the given symbol set.
func emitVimSymbols(b *strings.Builder, sym *pipelinesyntax.Symbols) {
	if sym == nil {
		return
	}
	fmt.Fprintf(b, "let s:steps_count = %d\n", len(sym.Steps))
	fmt.Fprintf(b, "let s:globals_count = %d\n", len(sym.Globals))
	for _, s := range sym.Steps {
		emitVimStep(b, s)
	}
	for _, g := range sym.Globals {
		emitVimGlobal(b, g)
	}
}

// renderAutoloadVim emits autoload/jenking.vim. Loaded on demand the first
// time anything calls a `jenking#X` function. The body of pure VimL functions
// lives in vimruntime_autoload.vim (and …_validate.vim) and is embedded; only
// the per-build symbol declarations are emitted here.
func renderAutoloadVim(sym *pipelinesyntax.Symbols, rt *vimRuntime) string {
	var b strings.Builder
	b.WriteString(`" Generated by jenking — autoload/jenking.vim.
let s:symbols = []
let s:steps_count = 0
let s:globals_count = 0
let s:members = {}
let s:receivers = []
let s:call_params = {}
`)
	emitVimSymbols(&b, sym)
	b.WriteString(autoloadVimBody)
	if rt != nil {
		b.WriteString(autoloadVimValidate)
	}
	return b.String()
}

// renderPluginVim emits plugin/jenking.vim — wires globals + autocmds. Real
// behaviour lives under autoload/jenking.vim (referenced by name).
func renderPluginVim(rt *vimRuntime) string {
	var b strings.Builder
	b.WriteString(`" Generated by jenking — plugin/jenking.vim.
if exists('g:loaded_jenking') | finish | endif
let g:loaded_jenking = 1
`)
	if rt != nil {
		fmt.Fprintf(&b, "let g:jenking_runtime_dir = %s\n", vimStr(rt.dir))
		fmt.Fprintf(&b, "let g:jenking_buffer = %s\n", vimStr(rt.bufferTmp))
		fmt.Fprintf(&b, "let g:jenking_errors = %s\n", vimStr(rt.errorsQF))
	}
	b.WriteString(`
augroup JenkingSetup
  autocmd!
  autocmd BufRead,BufNewFile *.groovy,Jenkinsfile,*.jenkinsfile call jenking#Setup()
augroup END
`)
	if rt != nil {
		b.WriteString(`
augroup JenkingValidate
  autocmd!
  autocmd BufWritePost *.groovy call jenking#WriteBuffer()
augroup END

if has('timers')
  call timer_start(1500, function('jenking#ReloadQF'), {'repeat': -1})
endif
`)
	}
	return b.String()
}

// vimStr quotes s as a Vim double-quoted string literal. Double-quoted is
// used (not single-quoted) because user-GDSL doc strings routinely contain
// newlines, single quotes (apostrophes in Dutch / English prose), and code
// samples like 'demo.tar' — none of which Vim's single-quoted strings can
// represent. Vim's double-quoted escapes: \\ \" \n \t \r, plus we drop
// other ASCII control bytes to keep the generated file printable.
func vimStr(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 {
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// applyVimArgs returns a new *exec.Cmd that layers jenking's runtime on top
// of the user's vim/nvim. For non-vim editors (emacs, nano, vscode, …) the
// original cmd is returned unchanged.
//
// Strategy:
//   - --cmd 'set rtp^=…'        runs BEFORE the user's vimrc — adds our runtime
//     to the head of the runtimepath so after/syntax/groovy.vim gets sourced.
//   - -c 'runtime! plugin/jenking.vim'  runs AFTER user vimrc — ensures our
//     Setup() autocmd is wired even if the user's vimrc disabled plugin loading.
func applyVimArgs(cmd *exec.Cmd, rt *vimRuntime) *exec.Cmd {
	if cmd == nil || rt == nil {
		return cmd
	}
	bin := filepath.Base(cmd.Path)
	if !isVimBinary(bin) {
		return cmd
	}
	// Prepend our flags; keep the original positional args (file path last).
	extra := []string{
		"--cmd", "set rtp^=" + rt.dir,
		"-c", "runtime! plugin/jenking.vim",
	}
	newArgs := append([]string{cmd.Args[0]}, extra...)
	newArgs = append(newArgs, cmd.Args[1:]...)
	cmd.Args = newArgs
	return cmd
}

// isVimBinary reports whether the basename of an editor command is one of
// the vim-family editors we know how to inject into.
func isVimBinary(bin string) bool {
	switch strings.ToLower(strings.TrimSuffix(bin, ".exe")) {
	case "vim", "nvim", "gvim", "vi":
		return true
	}
	return false
}
