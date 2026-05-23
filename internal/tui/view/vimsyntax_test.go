//go:build vimcheck

package view

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Breina/Jenking/internal/jenkins/pipelinesyntax"
)

func TestGeneratedVimscriptParses(t *testing.T) {
	if _, err := exec.LookPath("vim"); err != nil {
		t.Skip("vim not installed")
	}
	dir := t.TempDir()
	// Doc text mirrors what Cumulus-style GDSL produces — multi-line with
	// apostrophes, embedded code samples in backticks + braces, and
	// occasional `}` braces. The vim runtime must source cleanly despite
	// all of that.
	mavenDoc := "Stelt de Maven projectversie in.\n\n" +
		"Parameters:\n" +
		"- 'version': de nieuwe versie (bv. 1.2.3)\n\n" +
		"Voorbeeld:\n" +
		"maven.setVersion('1.2.3')\n" +
		"Code with } a brace and \"double\" quotes."
	sym := &pipelinesyntax.Symbols{
		DSLKeywords: []string{"pipeline", "stages"},
		Steps: []pipelinesyntax.Step{
			{Name: "sh", Doc: "shell's doc with apostrophe"},
			{Name: "cumuliDeploy"},
		},
		Globals: []pipelinesyntax.GlobalVar{
			{Name: "env"},
			{
				Name: "maven",
				Doc:  "Maven helpers from Cumulus.",
				Members: []pipelinesyntax.Member{
					{Name: "setVersion", Signature: "setVersion(version: String)", Doc: mavenDoc},
				},
			},
		},
	}
	rt := &vimRuntime{
		dir:       dir,
		bufferTmp: filepath.Join(dir, "buffer.groovy"),
		errorsQF:  filepath.Join(dir, "errors.qf"),
	}
	if err := os.MkdirAll(filepath.Join(dir, "autoload"), 0o700); err != nil {
		t.Fatal(err)
	}
	autoloadFile := filepath.Join(dir, "autoload", "jenking.vim")
	if err := os.WriteFile(autoloadFile, []byte(renderAutoloadVim(sym, rt)), 0o600); err != nil {
		t.Fatal(err)
	}
	pluginFile := filepath.Join(dir, "plugin.vim")
	if err := os.WriteFile(pluginFile, []byte(renderPluginVim(rt)), 0o600); err != nil {
		t.Fatal(err)
	}
	synFile := filepath.Join(dir, "groovy.vim")
	if err := os.WriteFile(synFile, []byte(renderSyntaxOverlay(sym)), 0o600); err != nil {
		t.Fatal(err)
	}
	// plugin/jenking.vim references jenking#Setup which auto-loads from
	// autoload/jenking.vim — point vim's rtp at the temp dir so the
	// reference resolves the same way it will in production.
	for _, f := range []string{autoloadFile, pluginFile, synFile} {
		errFile := filepath.Join(dir, filepath.Base(f)+".err")
		// vim's exit code in -e/-s mode is unreliable, so we detect parse failure
		// by writing v:exception to a sentinel file from inside a try/catch.
		vimCmd := `try | source ` + f + ` | catch | call writefile([v:exception], '` + errFile + `') | endtry | qa!`
		out, _ := exec.Command("vim", "-u", "NONE", "--not-a-term",
			"--cmd", "set rtp^="+dir, "-c", vimCmd).CombinedOutput()
		if data, rerr := os.ReadFile(errFile); rerr == nil && len(data) > 0 {
			t.Errorf("vim refused to source %s:\nERR: %s", f, data)
			_ = out
		}
	}
}
