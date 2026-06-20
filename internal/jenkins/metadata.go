package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// GetJobMetadata fetches a job's raw Jenkins JSON to the given depth and
// returns it as a generic tree. It decodes into `any` rather than typed
// structs and never branches on `_class`, so it stays agnostic to which
// plugins are installed.
func (c *Client) GetJobMetadata(ctx context.Context, jobPath string, depth int) (jmodel.MetaNode, error) {
	return c.fetchMetaTree(ctx, jmodel.JobPathToURL(jobPath), depth)
}

// GetBuildMetadata fetches a single build's raw Jenkins JSON to the given
// depth and returns it as a generic tree.
func (c *Client) GetBuildMetadata(ctx context.Context, jobPath string, number, depth int) (jmodel.MetaNode, error) {
	base := fmt.Sprintf("%s/%d", jmodel.JobPathToURL(jobPath), number)
	return c.fetchMetaTree(ctx, base, depth)
}

// objectMetadataClass is the core SCM-API action carrying the agnostic
// project/branch web URL (not a per-platform plugin).
const objectMetadataClass = "jenkins.scm.api.metadata.ObjectMetadataAction"

// GetJobSCMURL returns the job's SCM project/branch web URL
// (ObjectMetadataAction.objectUrl), or "" when the job has none. It uses a
// narrow tree query so it's cheap enough to prefetch in the background.
func (c *Client) GetJobSCMURL(ctx context.Context, jobPath string) (string, error) {
	path := jmodel.JobPathToURL(jobPath) + "/api/json?tree=actions[_class,objectUrl]"
	data, err := c.get(ctx, path)
	if err != nil {
		return "", fmt.Errorf("get scm url: %w", err)
	}
	var resp struct {
		Actions []struct {
			Class     string `json:"_class"`
			ObjectURL string `json:"objectUrl"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("parsing scm url: %w", err)
	}
	for _, a := range resp.Actions {
		if a.Class == objectMetadataClass && a.ObjectURL != "" {
			return a.ObjectURL, nil
		}
	}
	return "", nil
}

// fetchMetaTree GETs `<apiBase>/api/json?depth=N` and builds a MetaNode tree.
func (c *Client) fetchMetaTree(ctx context.Context, apiBase string, depth int) (jmodel.MetaNode, error) {
	path := fmt.Sprintf("%s/api/json?depth=%d", apiBase, depth)

	data, err := c.get(ctx, path)
	if err != nil {
		return jmodel.MetaNode{}, fmt.Errorf("get metadata: %w", err)
	}

	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return jmodel.MetaNode{}, fmt.Errorf("parsing metadata: %w", err)
	}
	return buildMetaTree("", root), nil
}

// buildMetaTree converts a decoded JSON value into a MetaNode. Objects recurse
// by key (sorted for determinism), arrays by index ("[i]"), and scalars become
// leaves. A nil value yields an empty leaf. No value transformation is applied.
func buildMetaTree(key string, v any) jmodel.MetaNode {
	switch val := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		children := make([]jmodel.MetaNode, 0, len(keys))
		for _, k := range keys {
			children = append(children, buildMetaTree(k, val[k]))
		}
		return jmodel.MetaNode{Key: key, Container: true, Children: children}
	case []any:
		children := make([]jmodel.MetaNode, 0, len(val))
		for i, elem := range val {
			children = append(children, buildMetaTree(fmt.Sprintf("[%d]", i), elem))
		}
		return jmodel.MetaNode{Key: key, Container: true, Children: children}
	case nil:
		return jmodel.MetaNode{Key: key, Value: ""}
	default:
		return jmodel.MetaNode{Key: key, Value: fmt.Sprintf("%v", val)}
	}
}
