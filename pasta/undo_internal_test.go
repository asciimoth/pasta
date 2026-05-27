package pasta

import (
	"reflect"
	"testing"

	"github.com/asciimoth/configer/configer"
)

type undoInternalNodeClass struct {
	name         string
	unique       bool
	factoryCalls *int
}

func (c undoInternalNodeClass) ClassName() string { return c.name }
func (c undoInternalNodeClass) ShortDescription() string {
	return ""
}
func (c undoInternalNodeClass) LongDescription() string {
	return ""
}
func (c undoInternalNodeClass) DefaultNodeParams() NodeClassParams {
	return NodeClassParams{Unique: c.unique}
}
func (c undoInternalNodeClass) NewNode(configer.Config, ...*NodeClassState) (Node, error) {
	if c.factoryCalls != nil {
		*c.factoryCalls += 1
	}
	return &internalNode{}, nil
}

func TestWorkspaceUndoRedoDropsUniqueClassRestoreWhenDuplicateExists(t *testing.T) {
	var factoryCalls int
	w := NewWorkspace(&nopLoggerFactory{})
	if err := w.AddNodeClass(undoInternalNodeClass{
		name:         "example.com/UniqueUndoInternal",
		unique:       true,
		factoryCalls: &factoryCalls,
	}); err != nil {
		t.Fatalf("AddNodeClass: %v", err)
	}
	firstID, err := w.AddNodeByClass("example.com/UniqueUndoInternal", "first")
	if err != nil {
		t.Fatalf("AddNodeByClass first: %v", err)
	}

	w.Lock()
	firstRecord, _ := w.nodes.Get(firstID)
	removedFirst := w.undoRemovedNodeEntry(firstID, firstRecord)
	w.Unlock()
	w.undoRecordingDisabled += 1
	w.RemoveNode(firstID)
	secondID, err := w.AddNodeByClass("example.com/UniqueUndoInternal", "second")
	w.undoRecordingDisabled -= 1
	if err != nil {
		t.Fatalf("AddNodeByClass second: %v", err)
	}
	before := w.Snapshot()
	callsBefore := factoryCalls

	w.Lock()
	w.undoLog = []undoEntry{removedFirst}
	w.Unlock()
	w.Undo()
	assertInternalSnapshot(t, w, before)
	if factoryCalls != callsBefore {
		t.Fatalf("factory calls after rejected undo = %d, want %d", factoryCalls, callsBefore)
	}
	nodes, err := w.NodesByClass("example.com/UniqueUndoInternal")
	if err != nil {
		t.Fatalf("NodesByClass after undo: %v", err)
	}
	if !reflect.DeepEqual(nodes, []uint64{secondID}) {
		t.Fatalf("nodes after rejected undo = %v, want [%d]", nodes, secondID)
	}

	w.Lock()
	w.redoLog = []undoEntry{removedFirst}
	w.Unlock()
	w.Redo()
	assertInternalSnapshot(t, w, before)
	if factoryCalls != callsBefore {
		t.Fatalf("factory calls after rejected redo = %d, want %d", factoryCalls, callsBefore)
	}
	nodes, err = w.NodesByClass("example.com/UniqueUndoInternal")
	if err != nil {
		t.Fatalf("NodesByClass after redo: %v", err)
	}
	if !reflect.DeepEqual(nodes, []uint64{secondID}) {
		t.Fatalf("nodes after rejected redo = %v, want [%d]", nodes, secondID)
	}
}

func TestWorkspaceUndoRedoGroupBestEffort(t *testing.T) {
	w := NewWorkspace(&nopLoggerFactory{})
	leftNode, err := w.AddNode(&internalNode{}, "example.com/GroupUndo", "left")
	if err != nil {
		t.Fatalf("AddNode left: %v", err)
	}
	rightNode, err := w.AddNode(&internalNode{}, "example.com/GroupUndo", "right")
	if err != nil {
		t.Fatalf("AddNode right: %v", err)
	}
	leftPort, err := w.AddPort(Port{
		Node:      leftNode,
		Direction: "right",
		Name:      "out",
		Types:     []string{"example.com/typeA"},
	})
	if err != nil {
		t.Fatalf("AddPort left: %v", err)
	}
	rightPort, err := w.AddPort(Port{
		Node:      rightNode,
		Direction: "left",
		Name:      "in",
		Types:     []string{"example.com/typeA"},
	})
	if err != nil {
		t.Fatalf("AddPort right: %v", err)
	}
	link, _, err := w.AddLink(leftPort, rightPort)
	if err != nil {
		t.Fatalf("AddLink: %v", err)
	}

	w.Lock()
	w.undoLog = []undoEntry{undoGroup{Entries: []undoEntry{
		undoAddedLink{ID: 999999},
		undoAddedLink{ID: link},
		undoAddedNode{ID: 888888},
	}}}
	w.Unlock()
	w.Undo()
	if _, ok := w.LinkSnapshot(link); ok {
		t.Fatalf("link %d survived grouped undo", link)
	}
	if _, ok := w.NodeSnapshot(leftNode); !ok {
		t.Fatalf("node %d was removed by failed grouped child", leftNode)
	}

	w.Redo()
	if _, _, ok := w.LinkByPorts(leftPort, rightPort); !ok {
		t.Fatalf("link %d was not restored by grouped redo", link)
	}
}

func assertInternalSnapshot(t *testing.T, w *Workspace, want WorkspaceSnapshot) {
	t.Helper()
	got := w.Snapshot()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot = %#v, want %#v", got, want)
	}
}
