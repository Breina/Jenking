package usecase

import (
	"context"
	"strings"

	"github.com/Breina/Jenking/internal/navmsg"
)

// maxJobPathDepth bounds the folder walk CanonicalJobPath performs, so a
// pathological input can never fan out into an unbounded number of listings.
const maxJobPathDepth = 8

// CanonicalJobPath maps a user-supplied job path onto the canonical form the
// Jenkins API needs: slash-separated segments, each percent-encoded, so a job
// or branch whose own name contains a slash stays a single segment.
//
// Paths emitted by this tool are already canonical and are returned untouched
// after one cheap existence probe. A decoded path typed by hand (or copied from
// a display string) is ambiguous — "Code/team/repo/main" could split at any
// slash — so it is resolved by walking the folder tree and matching decoded
// names greedily. When nothing matches, the input is returned unchanged and the
// caller surfaces its own error.
func (d Deps) CanonicalJobPath(ctx context.Context, jobPath string) string {
	if jobPath == "" || d.Client == nil {
		return jobPath
	}
	// Listing a path doubles as an existence check: a real job or folder
	// answers (a leaf job simply has no children), a bogus one 404s.
	if _, err := d.Client.ListJobs(ctx, jobPath); err == nil {
		return jobPath
	}
	tokens := strings.Split(navmsg.DecodePath(jobPath), "/")
	if len(tokens) > maxJobPathDepth {
		return jobPath
	}
	if resolved, ok := d.walkJobPath(ctx, "", tokens); ok {
		return resolved
	}
	return jobPath
}

// walkJobPath descends from prefix, consuming the tokens that a child's decoded
// name accounts for — one token for an ordinary name, several for a name that
// contains slashes. Returns the canonical path once every token is consumed.
func (d Deps) walkJobPath(ctx context.Context, prefix string, tokens []string) (string, bool) {
	if len(tokens) == 0 {
		return prefix, true
	}
	children, err := d.Client.ListJobs(ctx, prefix)
	if err != nil {
		return "", false
	}
	for _, child := range children {
		nameTokens := strings.Split(navmsg.DecodeName(child.Name), "/")
		if !consumes(tokens, nameTokens) {
			continue
		}
		next := child.Name
		if prefix != "" {
			next = prefix + "/" + child.Name
		}
		if resolved, ok := d.walkJobPath(ctx, next, tokens[len(nameTokens):]); ok {
			return resolved, true
		}
	}
	return "", false
}

// consumes reports whether want is a prefix of tokens, comparing segments
// case-insensitively (Jenkins names are case-sensitive, but a path typed by
// hand often is not).
func consumes(tokens, want []string) bool {
	if len(want) > len(tokens) {
		return false
	}
	for i, w := range want {
		if !strings.EqualFold(tokens[i], w) {
			return false
		}
	}
	return true
}
