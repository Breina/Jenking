package jenkins

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Breina/Jenking/internal/jenkins/pipelinesyntax"
)

// UserGDSLDir is the directory scanned for user-authored GDSL files. Each
// `*.gdsl` file inside may declare `contributor(context(ctype: 'X')) { … }`
// blocks whose `method`/`property` entries are merged onto Symbols.Globals
// as members of the named global.
//
// Defaults to $XDG_CONFIG_HOME/jenking/symbols (falling back to
// ~/.config/jenking/symbols). Set by main during startup. NOTE: server
// pipeline-syntax data is cached per build, but user GDSL is re-read on
// every symbol lookup so edits take effect on the next describe-view open.
var UserGDSLDir = defaultUserGDSLDir()

func defaultUserGDSLDir() string {
	if base, err := os.UserConfigDir(); err == nil {
		return filepath.Join(base, "jenking", "symbols")
	}
	return ""
}

// ApplyJobParameters fetches the job's parameter definitions and exposes each
// one as a member of the `params` global, so `params.RELEASE_TAG` etc. get
// proper completion. Param definitions live on the JOB (not per-build), so
// this is fast — one API call per describe-view open.
//
// Best-effort: a transport error or a non-parameterised job leaves sym
// untouched.
func (c *Client) ApplyJobParameters(ctx context.Context, sym *pipelinesyntax.Symbols, jobPath string) {
	if sym == nil || c == nil {
		return
	}
	defs, err := c.GetJobParameters(ctx, jobPath)
	if err != nil || len(defs) == 0 {
		return
	}
	members := make([]pipelinesyntax.Member, 0, len(defs))
	for _, d := range defs {
		menu := fmt.Sprintf("params.%s : %s", d.Name, d.Type)
		doc := d.Description
		if d.Default != "" {
			if doc != "" {
				doc += "\n\n"
			}
			doc += "Default: " + d.Default
		}
		if len(d.Choices) > 0 {
			if doc != "" {
				doc += "\n\n"
			}
			doc += "Choices: " + strings.Join(d.Choices, ", ")
		}
		members = append(members, pipelinesyntax.Member{
			Name: d.Name, Signature: menu, Doc: doc,
		})
	}
	for i := range sym.Globals {
		if sym.Globals[i].Name == "params" {
			sym.Globals[i].Members = append(sym.Globals[i].Members, members...)
			return
		}
	}
	sym.Globals = append(sym.Globals, pipelinesyntax.GlobalVar{
		Name: "params", Doc: "Build parameters", Members: members,
	})
}

// ApplyUserGDSL re-reads UserGDSLDir and overlays its members onto sym.
// Idempotent — repeated calls don't duplicate entries, because each call
// resets Members on user-declared globals before re-attaching the freshly
// loaded set. Caller invokes this on every Symbols read so config edits are
// picked up without invalidating the per-build cache.
func ApplyUserGDSL(sym *pipelinesyntax.Symbols) {
	if sym == nil {
		return
	}
	members, err := pipelinesyntax.LoadUserGDSLDir(UserGDSLDir)
	if err != nil || len(members) == 0 {
		// Still need to clear any previously-attached members on cached
		// Globals so deleted GDSL files don't linger.
		clearPriorUserMembers(sym, nil)
		return
	}
	clearPriorUserMembers(sym, members)

	seen := make(map[string]bool, len(sym.Globals))
	for _, g := range sym.Globals {
		seen[g.Name] = true
	}
	for recv, ms := range members {
		attached := false
		for i := range sym.Globals {
			if sym.Globals[i].Name == recv {
				sym.Globals[i].Members = append(sym.Globals[i].Members, ms...)
				attached = true
				break
			}
		}
		if attached || seen[recv] {
			continue
		}
		sym.Globals = append(sym.Globals, pipelinesyntax.GlobalVar{
			Name:    recv,
			Doc:     "User-declared global (via " + UserGDSLDir + ").",
			Members: ms,
		})
		seen[recv] = true
	}
}

// clearPriorUserMembers resets the Members slice on each global that EITHER
// will be re-populated this round OR has nothing left to attach. Server-side
// data (Jenkins gdsl/globals) never produces Members today, so this is safe
// — only user GDSL ever writes to that field.
func clearPriorUserMembers(sym *pipelinesyntax.Symbols, fresh map[string][]pipelinesyntax.Member) {
	for i := range sym.Globals {
		sym.Globals[i].Members = nil
	}
	_ = fresh
}

// FetchPipelineSyntax pulls the per-build gdsl + globals pages in parallel and
// returns a merged Symbols set. Either fetch may fail independently — we
// surface a Symbols populated with whatever succeeded plus the joined error,
// so callers can decide whether partial data is good enough.
//
// Side effect: a debug snapshot of the raw responses + parse counts is
// always written to $XDG_CACHE_HOME/jenking/syntax-debug/. Independent of
// the user's slog log level, so failed fetches can be diagnosed without
// having to flip log_level in config.
func (c *Client) FetchPipelineSyntax(ctx context.Context, jobPath string, buildNumber int) (*pipelinesyntax.Symbols, error) {
	// Jenkins exposes pipeline-syntax per job and at the controller level.
	// Per-build does NOT exist (observed 404 on this Jenkins). Per-job is
	// scoped to whichever libraries are loaded by that job; controller-level
	// always exists once the plugin is installed.
	jobURL := JobPathToURL(jobPath)
	_ = buildNumber
	gdslCandidates := []string{
		jobURL + "/pipeline-syntax/gdsl",
		"/pipeline-syntax/gdsl",
	}
	globalsCandidates := []string{
		jobURL + "/pipeline-syntax/globals",
		"/pipeline-syntax/globals",
	}

	var (
		wg         sync.WaitGroup
		gdslURL    string
		gdslBytes  []byte
		gdslErr    error
		globalsURL string
		htmlBytes  []byte
		globalsErr error
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		gdslURL, gdslBytes, gdslErr = c.fetchFirstAvailable(ctx, gdslCandidates)
	}()
	go func() {
		defer wg.Done()
		globalsURL, htmlBytes, globalsErr = c.fetchFirstAvailable(ctx, globalsCandidates)
	}()
	wg.Wait()

	sym := &pipelinesyntax.Symbols{
		DSLKeywords: pipelinesyntax.DefaultDSLKeywords,
		FetchedAt:   time.Now(),
	}
	var gdslGlobals []pipelinesyntax.GlobalVar
	if gdslErr == nil {
		sym.Steps, gdslGlobals = pipelinesyntax.ParseGDSL(string(gdslBytes))
	}
	if globalsErr == nil {
		sym.Globals = pipelinesyntax.ParseGlobals(string(htmlBytes))
	}
	// Supplement HTML globals with any properties only emitted by gdsl.
	// HTML doc wins when both sources have an entry.
	seen := make(map[string]bool, len(sym.Globals))
	for _, g := range sym.Globals {
		seen[g.Name] = true
	}
	for _, g := range gdslGlobals {
		if !seen[g.Name] {
			sym.Globals = append(sym.Globals, g)
			seen[g.Name] = true
		}
	}

	// User GDSL is NOT merged here — the result of this function is cached
	// per build, and we want user-edits to a .gdsl file to take effect
	// without busting that cache. The view layer calls ApplyUserGDSL after
	// every cache lookup or fresh fetch.

	if gdslErr != nil && globalsErr != nil {
		return sym, errors.Join(
			fmt.Errorf("gdsl: %w", gdslErr),
			fmt.Errorf("globals: %w", globalsErr),
		)
	}
	if gdslErr != nil {
		return sym, fmt.Errorf("gdsl: %w", gdslErr)
	}
	dumpSyntaxFetch(c.baseURL, jobPath, buildNumber,
		gdslURL, gdslBytes, gdslErr,
		globalsURL, htmlBytes, globalsErr,
		sym)

	if globalsErr != nil {
		return sym, fmt.Errorf("globals: %w", globalsErr)
	}
	return sym, nil
}

// fetchFirstAvailable walks a list of candidate URLs in order and returns
// the first one that succeeds (along with its body). Returns the LAST URL
// + error if all candidates fail — that's the controller-level fallback,
// so its error is the most diagnostic.
func (c *Client) fetchFirstAvailable(ctx context.Context, urls []string) (string, []byte, error) {
	var lastURL string
	var lastErr error
	for _, u := range urls {
		body, err := c.get(ctx, u)
		if err == nil {
			return u, body, nil
		}
		lastURL = u
		lastErr = err
		// Only retry on 404. Auth failures / network errors stop the chain.
		if !strings.Contains(err.Error(), "HTTP 404") {
			return u, nil, err
		}
	}
	return lastURL, nil, lastErr
}

// dumpSyntaxFetch writes a snapshot of the last pipeline-syntax fetch to
// $XDG_CACHE_HOME/jenking/syntax-debug/ regardless of the user's slog
// level. Three files per call:
//
//	gdsl.txt      — raw response body or "ERR: <msg>"
//	globals.html  — raw response body or "ERR: <msg>"
//	stats.txt     — request URLs, byte counts, parse counts, timestamp
//
// Overwrites on every call. Best-effort: filesystem errors are swallowed.
func dumpSyntaxFetch(
	baseURL, jobPath string, buildNumber int,
	gdslURL string, gdslBytes []byte, gdslErr error,
	globalsURL string, globalsBytes []byte, globalsErr error,
	sym *pipelinesyntax.Symbols,
) {
	root, err := os.UserCacheDir()
	if err != nil {
		return
	}
	dir := filepath.Join(root, "jenking", "syntax-debug")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}

	writeBody := func(name string, body []byte, err error) {
		path := filepath.Join(dir, name)
		if err != nil {
			_ = os.WriteFile(path, []byte("ERR: "+err.Error()+"\n"), 0o600)
			return
		}
		_ = os.WriteFile(path, body, 0o600)
	}
	writeBody("gdsl.txt", gdslBytes, gdslErr)
	writeBody("globals.html", globalsBytes, globalsErr)

	var b strings.Builder
	fmt.Fprintf(&b, "fetched_at:  %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "base_url:    %s\n", baseURL)
	fmt.Fprintf(&b, "job_path:    %s\n", jobPath)
	fmt.Fprintf(&b, "build:       %d\n", buildNumber)
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "gdsl_url:    %s%s\n", baseURL, gdslURL)
	if gdslErr != nil {
		fmt.Fprintf(&b, "gdsl_error:  %s\n", gdslErr.Error())
	} else {
		fmt.Fprintf(&b, "gdsl_bytes:  %d\n", len(gdslBytes))
	}
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "globals_url: %s%s\n", baseURL, globalsURL)
	if globalsErr != nil {
		fmt.Fprintf(&b, "globals_err: %s\n", globalsErr.Error())
	} else {
		fmt.Fprintf(&b, "globals_bytes: %d\n", len(globalsBytes))
	}
	if sym != nil {
		fmt.Fprintf(&b, "\nparsed_steps:   %d\n", len(sym.Steps))
		fmt.Fprintf(&b, "parsed_globals: %d\n", len(sym.Globals))
	}
	_ = os.WriteFile(filepath.Join(dir, "stats.txt"), []byte(b.String()), 0o600)
}
