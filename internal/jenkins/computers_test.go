package jenkins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListNodes(t *testing.T) {
	const body = `{"computer":[
		{"displayName":"built-in","offline":false,"temporarilyOffline":false,
		 "executors":[{"currentExecutable":{"building":true}},{"currentExecutable":null}],
		 "monitorData":{
		   "hudson.node_monitors.DiskSpaceMonitor":{"size":48318382080},
		   "hudson.node_monitors.SwapSpaceMonitor":{"availablePhysicalMemory":3221225472},
		   "hudson.node_monitors.ResponseTimeMonitor":{"average":12}}},
		{"displayName":"agent-1","offline":true,"temporarilyOffline":false,"offlineCauseReason":"disconnected",
		 "executors":[{"currentExecutable":null}],
		 "monitorData":{"hudson.node_monitors.DiskSpaceMonitor":null}},
		{"displayName":"agent-2","offline":false,"temporarilyOffline":true,
		 "executors":[{"currentExecutable":{"building":true}},{"currentExecutable":{"building":true}}],
		 "monitorData":{}}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "admin", "token123", false)
	nodes, err := client.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("want 3 nodes, got %d", len(nodes))
	}

	want := []struct {
		name    string
		offline bool
		num     int
		busy    int
	}{
		{"built-in", false, 2, 1},
		{"agent-1", true, 1, 0},
		{"agent-2", true, 2, 2}, // temporarilyOffline folds into Offline
	}
	for i, w := range want {
		n := nodes[i]
		if n.Name != w.name || n.Offline != w.offline || n.NumExecutors != w.num || n.BusyExecutors != w.busy {
			t.Errorf("node %d = %+v, want %+v", i, n, w)
		}
	}

	if nodes[0].FreeDiskBytes != 48318382080 || nodes[0].FreeMemBytes != 3221225472 || nodes[0].ResponseMs != 12 {
		t.Errorf("built-in monitor data = %+v", nodes[0])
	}
	if nodes[1].OfflineCause != "disconnected" {
		t.Errorf("agent-1 offline cause = %q", nodes[1].OfflineCause)
	}
	if nodes[2].FreeDiskBytes != 0 {
		t.Errorf("agent-2 should have zero disk when monitor absent, got %d", nodes[2].FreeDiskBytes)
	}
}
