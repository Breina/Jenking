package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// changesTree requests both the pipeline (changeSets, plural array) and the
// freestyle (changeSet, singular object) shapes so the parser is agnostic to
// the job type — Jenkins populates exactly one of them per build.
const changesTree = "changeSets[items[commitId,msg,timestamp,author[fullName],authorEmail,affectedPaths]]," +
	"changeSet[items[commitId,msg,timestamp,author[fullName],authorEmail,affectedPaths]]"

// maxFindBuilds bounds the single tree request FindCommit issues.
const maxFindBuilds = 50

type jsonChangeAuthor struct {
	FullName string `json:"fullName"`
}

type jsonChangeItem struct {
	CommitID      string           `json:"commitId"`
	Msg           string           `json:"msg"`
	Timestamp     int64            `json:"timestamp"`
	Author        jsonChangeAuthor `json:"author"`
	AuthorEmail   string           `json:"authorEmail"`
	AffectedPaths []string         `json:"affectedPaths"`
}

func (i jsonChangeItem) toDomain() jmodel.Change {
	var ts time.Time
	if i.Timestamp > 0 {
		ts = time.UnixMilli(i.Timestamp)
	}
	return jmodel.Change{
		CommitID:      i.CommitID,
		Author:        i.Author.FullName,
		AuthorEmail:   i.AuthorEmail,
		Message:       i.Msg,
		Timestamp:     ts,
		AffectedPaths: i.AffectedPaths,
	}
}

type jsonChangeSet struct {
	Items []jsonChangeItem `json:"items"`
}

// jsonChangeSources carries both changeSet shapes. Embedded into per-build
// responses so the same accessors work at the build and build-list levels.
type jsonChangeSources struct {
	ChangeSets []jsonChangeSet `json:"changeSets"`
	ChangeSet  jsonChangeSet   `json:"changeSet"`
}

// items flattens the singular and plural change sets in change order.
func (s jsonChangeSources) items() []jsonChangeItem {
	items := append([]jsonChangeItem(nil), s.ChangeSet.Items...)
	for _, cs := range s.ChangeSets {
		items = append(items, cs.Items...)
	}
	return items
}

func (s jsonChangeSources) toChanges() []jmodel.Change {
	items := s.items()
	out := make([]jmodel.Change, len(items))
	for i, it := range items {
		out[i] = it.toDomain()
	}
	return out
}

// GetChanges returns the SCM commits recorded for a single build.
func (c *Client) GetChanges(ctx context.Context, jobPath string, number int) ([]jmodel.Change, error) {
	path := fmt.Sprintf("%s/%d/api/json?tree=%s", jmodel.JobPathToURL(jobPath), number, url.QueryEscape(changesTree))
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("get changes: %w", err)
	}
	var resp jsonChangeSources
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing changes response: %w", err)
	}
	return resp.toChanges(), nil
}

type jsonFindBuild struct {
	Number int `json:"number"`
	jsonChangeSources
}

type jsonFindResponse struct {
	Builds []jsonFindBuild `json:"builds"`
}

// FindCommit scans the job's recent builds (a single tree request) for a commit
// whose id starts with commitPrefix, returning one hit per matching build.
func (c *Client) FindCommit(ctx context.Context, jobPath, commitPrefix string, maxBuilds int) ([]jmodel.BuildCommitHit, error) {
	if maxBuilds <= 0 || maxBuilds > maxFindBuilds {
		maxBuilds = maxFindBuilds
	}
	tree := fmt.Sprintf("builds[number,changeSets[items[commitId]],changeSet[items[commitId]]]{0,%d}", maxBuilds)
	path := jmodel.JobPathToURL(jobPath) + "/api/json?tree=" + url.QueryEscape(tree)
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("find commit: %w", err)
	}
	var resp jsonFindResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing find-commit response: %w", err)
	}

	prefix := strings.ToLower(strings.TrimSpace(commitPrefix))
	var hits []jmodel.BuildCommitHit
	for _, b := range resp.Builds {
		for _, it := range b.items() {
			if prefix != "" && strings.HasPrefix(strings.ToLower(it.CommitID), prefix) {
				hits = append(hits, jmodel.BuildCommitHit{BuildNumber: b.Number, CommitID: it.CommitID})
				break
			}
		}
	}
	return hits, nil
}
