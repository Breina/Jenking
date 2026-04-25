package view

import (
	"context"
	"strings"

	"github.com/Breina/Jenking/internal/cache"
	"github.com/Breina/Jenking/internal/jenkins"
)

// nodeLogState tracks progressive fetch state for a single node.
type nodeLogState struct {
	text      string
	nextStart int
	moreData  bool // true if this node may produce more output
}

// NodeLogResult holds the result of a node log poll operation.
type NodeLogResult struct {
	Nodes   map[int]*nodeLogState
	NodeIDs []int
	Running bool
}

// fetchNodeLogs fetches log text for all specified nodes from scratch.
func fetchNodeLogs(ctx context.Context, client jenkins.JenkinsClient, jobPath string, buildNumber int, nodeIDs []int) (map[int]*nodeLogState, error) {
	nodes := make(map[int]*nodeLogState, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		nl, err := client.GetNodeLogProgressive(ctx, jobPath, buildNumber, nodeID, 0)
		if err != nil {
			return nil, err
		}
		nodes[nodeID] = &nodeLogState{
			text:      strings.TrimRight(nl.Text, "\n"),
			nextStart: nl.NextStart,
			moreData:  nl.MoreData,
		}
	}
	return nodes, nil
}

// pollNodeLogs re-fetches stages (to discover new nodes) and incrementally
// fetches new output from any node that still has moreData.
// The nodes map is mutated in place — callers should pass a snapshot.
func pollNodeLogs(ctx context.Context, client jenkins.JenkinsClient, jobPath string, buildNumber int, stageName string, nodes map[int]*nodeLogState, oldNodeIDs []int) NodeLogResult {
	stages, err := client.ListStages(ctx, jobPath, buildNumber)
	if err != nil {
		if ctx.Err() != nil {
			return NodeLogResult{}
		}
		// Transient error — return old state as running.
		return NodeLogResult{Nodes: nodes, NodeIDs: oldNodeIDs, Running: true}
	}

	var nodeIDs []int
	running := false
	if idx, ok := findOwningStage(stages, stageName, oldNodeIDs); ok {
		nodeIDs = stages[idx].NodeIDs
		running = stages[idx].Status == jenkins.BuildStatusRunning
	} else {
		nodeIDs = oldNodeIDs
	}

	for _, nodeID := range nodeIDs {
		ns, exists := nodes[nodeID]
		if !exists {
			nl, err := client.GetNodeLogProgressive(ctx, jobPath, buildNumber, nodeID, 0)
			if err != nil {
				if ctx.Err() != nil {
					return NodeLogResult{}
				}
				continue
			}
			nodes[nodeID] = &nodeLogState{
				text:      strings.TrimRight(nl.Text, "\n"),
				nextStart: nl.NextStart,
				moreData:  nl.MoreData,
			}
		} else if ns.moreData {
			nl, err := client.GetNodeLogProgressive(ctx, jobPath, buildNumber, nodeID, ns.nextStart)
			if err != nil {
				if ctx.Err() != nil {
					return NodeLogResult{}
				}
				continue
			}
			if nl.Text != "" {
				if ns.text != "" {
					ns.text += "\n" + strings.TrimRight(nl.Text, "\n")
				} else {
					ns.text = strings.TrimRight(nl.Text, "\n")
				}
			}
			ns.nextStart = nl.NextStart
			ns.moreData = nl.MoreData
		}
	}

	return NodeLogResult{Nodes: nodes, NodeIDs: nodeIDs, Running: running}
}

// restoreNodesFromCache loads node log state from the cache.
// Returns the nodes map and true if all nodes were found.
func restoreNodesFromCache(store *cache.Store, jobPath string, buildNumber int, nodeIDs []int) (map[int]*nodeLogState, bool) {
	if store == nil {
		return nil, false
	}
	nodes := make(map[int]*nodeLogState, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		key := cache.StageLogKey{JobPath: jobPath, BuildNumber: buildNumber, NodeID: nodeID}
		e := store.NodeLogs.Get(key)
		if e == nil {
			return nil, false
		}
		nodes[nodeID] = &nodeLogState{
			text:      e.Value.Text,
			nextStart: e.Value.NextStart,
			moreData:  e.Value.MoreData,
		}
	}
	return nodes, true
}

// persistNodeLogs writes node log state to the shared cache.
func persistNodeLogs(store *cache.Store, jobPath string, buildNumber int, nodeIDs []int, nodes map[int]*nodeLogState) {
	if store == nil {
		return
	}
	for _, nodeID := range nodeIDs {
		ns, ok := nodes[nodeID]
		if !ok {
			continue
		}
		key := cache.StageLogKey{JobPath: jobPath, BuildNumber: buildNumber, NodeID: nodeID}
		store.NodeLogs.Put(key, cache.NodeLogSnapshot{
			Text:      ns.text,
			NextStart: ns.nextStart,
			MoreData:  ns.moreData,
		})
	}
}

// snapshotNodes creates a deep copy of nodes for safe use in goroutines.
func snapshotNodes(nodes map[int]*nodeLogState) map[int]*nodeLogState {
	cp := make(map[int]*nodeLogState, len(nodes))
	for k, v := range nodes {
		dup := *v
		cp[k] = &dup
	}
	return cp
}

// allNodesTerminal returns true if no node has more data to fetch.
func allNodesTerminal(nodes map[int]*nodeLogState) bool {
	for _, ns := range nodes {
		if ns.moreData {
			return false
		}
	}
	return true
}

// findOwningStage picks the stage whose NodeIDs represent the previewed
// stage. A parent stage's NodeIDs include every descendant's node IDs
// (parseFlowGraphTable collects all rows within a stage's scope), so a
// naive "first overlap" match can silently promote a child preview to
// its parent and prepend the parent's pre-child log rows. To avoid that:
// prefer a candidate whose Name matches stageName, and among candidates
// prefer the one with the smallest NodeIDs set (the tightest container).
func findOwningStage(stages []jenkins.Stage, stageName string, oldNodeIDs []int) (int, bool) {
	if len(stages) == 0 || len(oldNodeIDs) == 0 {
		return 0, false
	}
	oldSet := make(map[int]struct{}, len(oldNodeIDs))
	for _, id := range oldNodeIDs {
		oldSet[id] = struct{}{}
	}
	bestNamed := -1
	bestAny := -1
	for i, s := range stages {
		overlap := false
		for _, nid := range s.NodeIDs {
			if _, ok := oldSet[nid]; ok {
				overlap = true
				break
			}
		}
		if !overlap {
			continue
		}
		if bestAny < 0 || len(s.NodeIDs) < len(stages[bestAny].NodeIDs) {
			bestAny = i
		}
		if s.Name == stageName {
			if bestNamed < 0 || len(s.NodeIDs) < len(stages[bestNamed].NodeIDs) {
				bestNamed = i
			}
		}
	}
	if bestNamed >= 0 {
		return bestNamed, true
	}
	if bestAny >= 0 {
		return bestAny, true
	}
	return 0, false
}

// aggregateNodeLogs joins node logs in nodeID order.
func aggregateNodeLogs(nodeIDs []int, nodes map[int]*nodeLogState) string {
	var parts []string
	for _, id := range nodeIDs {
		if ns, ok := nodes[id]; ok && ns.text != "" {
			parts = append(parts, ns.text)
		}
	}
	return strings.Join(parts, "\n")
}
