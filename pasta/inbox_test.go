package pasta_test

import (
	"strings"
	"testing"

	"github.com/asciimoth/pasta/pasta"
)

func TestWorkspaceInboxDelivery(t *testing.T) {
	logf := &StringLoggerFactory{}
	w := pasta.NewWorkspace(logf)
	node := &workspaceNode{}
	nodeID, err := w.AddNode(node, "example.com/InboxNode")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	before := w.Snapshot()

	w.SendInbox(pasta.InboxMessage{
		ReceiverNode: nodeID,
		Payload:      "worker-done",
	})

	if got := logf.Result(); !strings.Contains(got, "inbox receiver=1 payload=worker-done") {
		t.Fatalf("inbox message was not delivered:\n%s", got)
	}
	assertWorkspaceSnapshot(t, w, before)
}

func TestWorkspaceDropsInboxWhenReceiverRemovedBeforeDelivery(t *testing.T) {
	logf := &StringLoggerFactory{}
	w := pasta.NewWorkspace(logf)
	node := &workspaceNode{}
	nodeID, err := w.AddNode(node, "example.com/InboxNode")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	w.Lock()
	w.SendInboxLocked(pasta.InboxMessage{
		ReceiverNode: nodeID,
		Payload:      "late",
	})
	w.RemoveNodeLocked(nodeID)
	w.Unlock()

	if got := logf.Result(); strings.Contains(got, "payload=late") {
		t.Fatalf("inbox message was delivered after receiver removal:\n%s", got)
	}
}
