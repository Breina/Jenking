package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/domain/jmodel"
	"github.com/Breina/Jenking/internal/domain/pipelinesyntax"
	"github.com/Breina/Jenking/internal/jenkins"
)

// symbolCacheKey namespaces the per-build pipeline-syntax cache. The v5 prefix
// invalidates older entries when the parser changes shape (bumped after a parser
// bug poisoned v4 caches with empty Symbols).
func symbolCacheKey(jobPath string, buildNum int) string {
	return fmt.Sprintf("v5/%s#%d", jobPath, buildNum)
}

// GetPipelineSymbols returns the per-build pipeline-syntax symbol set (steps,
// globals, DSL keywords) used to drive Jenkinsfile authoring. It prefers the
// cache (in-memory then disk); on a miss it hits Jenkins and populates both
// layers. Builds are immutable so the raw fetch is cache-forever; the user-GDSL
// and job-parameter overlays are re-applied on every read so config edits take
// effect without busting the cache.
//
// The returned Symbols may be non-nil even when err is non-nil: a partial fetch
// (one of gdsl/globals succeeded) still yields useful data. Callers that need a
// hard success should treat an empty result plus a non-nil err as failure.
func (d Deps) GetPipelineSymbols(ctx context.Context, jobPath string, buildNum int) (*pipelinesyntax.Symbols, error) {
	key := symbolCacheKey(jobPath, buildNum)
	if sym, hit := loadCachedSymbols(d.Store, key); hit {
		d.overlaySymbols(ctx, sym, jobPath)
		return sym, nil
	}
	sym, err := d.Client.FetchPipelineSyntax(ctx, jobPath, buildNum)
	// Cache only server data with content — a 0/0 result is nearly always a
	// transient auth/network failure, and persisting it would mask real data on
	// every future open. Overlays are applied on read, never cached.
	if sym != nil && (len(sym.Steps) > 0 || len(sym.Globals) > 0) {
		storeSymbols(d.Store, key, sym)
	}
	d.overlaySymbols(ctx, sym, jobPath)
	return sym, err
}

// overlaySymbols layers user-authored GDSL members and the job's parameter
// definitions onto sym. Best-effort and idempotent.
func (d Deps) overlaySymbols(ctx context.Context, sym *pipelinesyntax.Symbols, jobPath string) {
	if sym == nil {
		return
	}
	jenkins.ApplyUserGDSL(sym)
	d.applyJobParams(ctx, sym, jobPath)
}

// applyJobParams overlays the job's parameter definitions onto the `params`
// global so completion and signature popups reflect the current build
// parameters. Best-effort: a transport error or a non-parameterised job leaves
// sym untouched.
func (d Deps) applyJobParams(ctx context.Context, sym *pipelinesyntax.Symbols, jobPath string) {
	if sym == nil {
		return
	}
	defs, err := d.Client.GetJobParameters(ctx, jobPath)
	if err != nil || len(defs) == 0 {
		return
	}
	members := make([]pipelinesyntax.Member, 0, len(defs))
	for _, def := range defs {
		menu := fmt.Sprintf("params.%s : %s", def.Name, def.Type)
		doc := def.Description
		if def.Default != "" {
			if doc != "" {
				doc += "\n\n"
			}
			doc += "Default: " + def.Default
		}
		if len(def.Choices) > 0 {
			if doc != "" {
				doc += "\n\n"
			}
			doc += "Choices: " + strings.Join(def.Choices, ", ")
		}
		members = append(members, pipelinesyntax.Member{
			Name: def.Name, Signature: menu, Doc: doc,
		})
	}
	pipelinesyntax.AttachParamsGlobal(sym, members)
}

// Lint validates a Jenkinsfile against the controller's declarative
// pipeline-model-converter and returns the structured result.
func (d Deps) Lint(ctx context.Context, script string) (jmodel.ValidationResult, error) {
	return d.Client.ValidateJenkinsfile(ctx, script)
}

// loadCachedSymbols tries the in-memory cache first, falling back to disk; hot
// disk hits get promoted into memory before returning. hit=false when neither
// layer has the key.
func loadCachedSymbols(store *cache.Store, key string) (*pipelinesyntax.Symbols, bool) {
	if store == nil || store.Symbols == nil {
		return nil, false
	}
	if e := store.Symbols.Get(key); e != nil && e.Value != nil {
		return e.Value, true
	}
	if store.Disk == nil {
		return nil, false
	}
	sym, err := store.Disk.LoadSymbols(key)
	if err != nil {
		return nil, false
	}
	store.Symbols.Put(key, sym)
	return sym, true
}

// storeSymbols writes pipeline symbols to both cache layers (memory and disk).
// Caller is responsible for the empty-content guard.
func storeSymbols(store *cache.Store, key string, sym *pipelinesyntax.Symbols) {
	if store == nil || store.Symbols == nil {
		return
	}
	store.Symbols.Put(key, sym)
	if store.Disk != nil {
		_ = store.Disk.SaveSymbols(key, sym)
	}
}
