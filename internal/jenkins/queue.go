package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

type jsonQueueList struct {
	Items []jsonQueueItem `json:"items"`
}

type jsonQueueItem struct {
	ID           int64                `json:"id"`
	InQueueSince int64                `json:"inQueueSince"`
	Why          *string              `json:"why"`
	Blocked      bool                 `json:"blocked"`
	Buildable    bool                 `json:"buildable"`
	Stuck        bool                 `json:"stuck"`
	Pending      bool                 `json:"pending"`
	Task         jsonQueueTask        `json:"task"`
	Executable   *jsonQueueExecutable `json:"executable"`
	Actions      []jsonAction         `json:"actions"`
}

type jsonQueueTask struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// jsonQueueExecutable is non-nil once Jenkins has assigned the item to an
// executor and created its run — at that point the build exists and the
// running-builds view owns it, so we drop it from the queue.
type jsonQueueExecutable struct {
	Number int `json:"number"`
}

const queueTreeParam = `items[id,inQueueSince,why,blocked,buildable,stuck,pending,task[name,url],executable[number],actions[parameters[name,value],causes[shortDescription,userName,userId]]]`

// ListQueue returns the builds currently waiting in the Jenkins build queue.
// The queue is global to the instance; callers scope it client-side.
func (c *Client) ListQueue(ctx context.Context) ([]jmodel.QueueItem, error) {
	data, err := c.get(ctx, "/queue/api/json?tree="+queueTreeParam)
	if err != nil {
		return nil, fmt.Errorf("listing queue: %w", err)
	}
	var list jsonQueueList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parsing queue: %w", err)
	}
	items := make([]jmodel.QueueItem, 0, len(list.Items))
	for i := range list.Items {
		// An item with an executable has already been handed to an executor —
		// it is (or is about to be) a running build, so the running view owns
		// it. Dropping it here avoids a queued/running duplicate during handoff.
		if list.Items[i].Executable != nil && list.Items[i].Executable.Number > 0 {
			continue
		}
		items = append(items, list.Items[i].toDomain(c.baseURL))
	}
	return items, nil
}

// CancelQueueItem removes a waiting item from the build queue by its queue id.
func (c *Client) CancelQueueItem(ctx context.Context, id int64) error {
	return c.post(ctx, fmt.Sprintf("/queue/cancelItem?id=%d", id), nil)
}

func (j *jsonQueueItem) toDomain(baseURL string) jmodel.QueueItem {
	why := ""
	if j.Why != nil {
		why = *j.Why
	}
	var params map[string]string
	for _, a := range j.Actions {
		if len(a.Parameters) == 0 {
			continue
		}
		params = make(map[string]string, len(a.Parameters))
		for i := range a.Parameters {
			p := &a.Parameters[i]
			params[p.Name] = p.stringValue()
		}
		break
	}
	return jmodel.QueueItem{
		ID:              j.ID,
		JobPath:         parseTaskURL(baseURL, j.Task.URL),
		DisplayName:     j.Task.Name,
		Why:             why,
		Blocked:         j.Blocked,
		Buildable:       j.Buildable,
		Stuck:           j.Stuck,
		Pending:         j.Pending,
		InQueueSince:    millisToTime(j.InQueueSince),
		Params:          params,
		Cause:           extractCause(j.Actions),
		TriggeredBy:     extractUserID(j.Actions),
		TriggeredByName: extractUserName(j.Actions),
	}
}

// parseTaskURL extracts the slash-separated job path from a queue task URL,
// which (unlike a build URL) carries no trailing build number.
// Example: "https://ci/job/FolderA/job/Pipeline/" → "FolderA/Pipeline".
func parseTaskURL(baseURL, rawURL string) string {
	path := strings.TrimPrefix(rawURL, baseURL)
	path = strings.Trim(path, "/")
	if path == "" {
		return ""
	}
	segments := strings.Split(path, "/")
	var jobParts []string
	for i := 0; i < len(segments); i++ {
		if segments[i] == "job" && i+1 < len(segments) {
			seg, _ := url.PathUnescape(segments[i+1])
			jobParts = append(jobParts, seg)
			i++
		}
	}
	return strings.Join(jobParts, "/")
}
