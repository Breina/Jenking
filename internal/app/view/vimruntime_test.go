package view

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/Breina/Jenking/internal/domain/pipelinesyntax"
)

func TestRenderSyntaxOverlay(t *testing.T) {
	sym := &pipelinesyntax.Symbols{
		DSLKeywords: []string{"pipeline", "stages"},
		Steps:       []pipelinesyntax.Step{{Name: "sh"}, {Name: "cumuliDeploy"}},
		Globals:     []pipelinesyntax.GlobalVar{{Name: "env"}},
	}
	out := renderSyntaxOverlay(sym)

	mustContain(t, out, `syntax match jenkingDSL /\<\(pipeline\|stages\)\>/`)
	mustContain(t, out, `syntax match jenkingStep /\<\(sh\|cumuliDeploy\)\>/`)
	mustContain(t, out, `syntax match jenkingGlobal /\<\(env\)\>/`)
	mustContain(t, out, "highlight default link jenkingDSL      Statement")
	mustContain(t, out, "highlight default link jenkingStep     Function")
	mustContain(t, out, "highlight default link jenkingGlobal   Constant")
}

func TestRenderSyntaxOverlay_FiltersUnsafe(t *testing.T) {
	sym := &pipelinesyntax.Symbols{
		Steps: []pipelinesyntax.Step{
			{Name: "sh"},
			{Name: "evil-name"},   // hyphen → not a valid vim keyword
			{Name: "sh"},          // duplicate
			{Name: ""},            // empty
			{Name: "1starts-bad"}, // hyphen, drop
		},
	}
	out := renderSyntaxOverlay(sym)

	if !strings.Contains(out, `syntax match jenkingStep /\<\(sh\)\>/`) {
		t.Errorf("expected 'sh' on its own (no duplicate), got:\n%s", out)
	}
	if strings.Contains(out, "evil-name") {
		t.Errorf("unsafe name leaked through: %s", out)
	}
}

func TestRenderAutoloadVim_EmbedsMembers(t *testing.T) {
	sym := &pipelinesyntax.Symbols{
		Globals: []pipelinesyntax.GlobalVar{
			{
				Name: "maven",
				Doc:  "Maven helpers.",
				Members: []pipelinesyntax.Member{
					{Name: "setVersion", Signature: "setVersion(version)", Doc: "Set the project version."},
					{Name: "goal", Signature: "goal(name)", Doc: "Run a Maven goal."},
				},
			},
		},
	}
	out := renderAutoloadVim(sym, nil)
	mustContain(t, out, `let s:members["maven"] = []`)
	mustContain(t, out, `call add(s:members["maven"], {'word': "setVersion"`)
	mustContain(t, out, `'menu': "setVersion(version)"`)
	mustContain(t, out, `call add(s:receivers, "maven")`)
	mustContain(t, out, "function! jenking#DotTrigger")
	mustContain(t, out, "has_key(s:members, l:receiver)")
}

func TestRenderAutoloadVim_EmbedsSymbols(t *testing.T) {
	sym := &pipelinesyntax.Symbols{
		Steps: []pipelinesyntax.Step{
			{
				Name: "sh",
				Params: []pipelinesyntax.Param{
					{Name: "script", Type: "java.lang.String", Named: true},
				},
				Doc: "Run a shell script.",
			},
		},
		Globals: []pipelinesyntax.GlobalVar{{Name: "env", Doc: "Env vars."}},
	}
	rt := &vimRuntime{dir: "/tmp/x", bufferTmp: "/tmp/x/buffer.groovy", errorsQF: "/tmp/x/errors.qf"}
	out := renderAutoloadVim(sym, rt)

	mustContain(t, out, `call add(s:symbols, {'word': "sh"`)
	mustContain(t, out, `'menu': "sh(script: String)"`)
	mustContain(t, out, `'info': "Run a shell script."`)
	mustContain(t, out, "'kind': 'f'")
	mustContain(t, out, `'word': "env"`)
	mustContain(t, out, "'kind': 'v'")
	mustContain(t, out, "function! jenking#Complete")
	mustContain(t, out, "function! jenking#WriteBuffer")
}

func TestRenderPluginVim_WiresGlobalsAndAutocmds(t *testing.T) {
	rt := &vimRuntime{dir: "/tmp/x", bufferTmp: "/tmp/x/buffer.groovy", errorsQF: "/tmp/x/errors.qf"}
	out := renderPluginVim(rt)

	mustContain(t, out, `let g:jenking_buffer = "/tmp/x/buffer.groovy"`)
	mustContain(t, out, `let g:jenking_errors = "/tmp/x/errors.qf"`)
	mustContain(t, out, "call jenking#Setup()")
	mustContain(t, out, "call jenking#WriteBuffer()")
}

func TestVimStr_Escapes(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`it's fine`, `"it's fine"`},         // single quotes pass through untouched
		{`he said "hi"`, `"he said \"hi\""`}, // double quotes escaped
		{"line one\nline two", `"line one\nline two"`},
		{`back\slash`, `"back\\slash"`},
		{"tab\there", `"tab\there"`},
		{"drop\x01ctrl", `"dropctrl"`}, // control bytes dropped
	}
	for _, c := range cases {
		if got := vimStr(c.in); got != c.want {
			t.Errorf("vimStr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestApplyVimArgs_OnlyForVim(t *testing.T) {
	rt := &vimRuntime{dir: "/tmp/x"}

	for _, bin := range []string{"vim", "nvim", "gvim", "vi"} {
		cmd := exec.Command(bin, "/tmp/file.groovy")
		got := applyVimArgs(cmd, rt)
		joined := strings.Join(got.Args, " ")
		if !strings.Contains(joined, "set rtp^=/tmp/x") {
			t.Errorf("%s: expected rtp injection, got %v", bin, got.Args)
		}
		if !strings.Contains(joined, "runtime! plugin/jenking.vim") {
			t.Errorf("%s: expected runtime! injection, got %v", bin, got.Args)
		}
		if got.Args[len(got.Args)-1] != "/tmp/file.groovy" {
			t.Errorf("%s: file path should stay last, got %v", bin, got.Args)
		}
	}

	for _, bin := range []string{"nano", "emacs", "code"} {
		cmd := exec.Command(bin, "/tmp/file.groovy")
		got := applyVimArgs(cmd, rt)
		if len(got.Args) != 2 {
			t.Errorf("%s: non-vim editor should be untouched, got %v", bin, got.Args)
		}
	}
}

func TestBuildRuntimeKey_Stable(t *testing.T) {
	a := buildRuntimeKey("Code/git%2Fomv%2Fomv-master", 286)
	b := buildRuntimeKey("Code/git%2Fomv%2Fomv-master", 286)
	c := buildRuntimeKey("Code/git%2Fomv%2Fomv-master", 287)
	if a != b {
		t.Errorf("same input → different keys: %q vs %q", a, b)
	}
	if a == c {
		t.Errorf("different build number should yield a different key")
	}
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected to contain %q, output was:\n%s", needle, haystack)
	}
}
