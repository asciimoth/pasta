package pasta_test

import (
	"reflect"
	"testing"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/configer/hujson"
	"github.com/asciimoth/pasta/pasta"
)

func TestWorkspaceUndoRedoNodeAndLinkMutationsRestorePublicState(t *testing.T) {
	var restoredValues []string
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	mustAddUndoFactoryClass(t, w, "example.com/Source", []pasta.Port{{
		Direction: "right",
		Name:      "out",
		Types:     []string{"example.com/typeA"},
	}}, &restoredValues)
	mustAddUndoFactoryClass(t, w, "example.com/Target", []pasta.Port{{
		Direction: "left",
		Name:      "in",
		Types:     []string{"example.com/typeA"},
	}}, &restoredValues)

	sourceID, err := w.AddNodeByClass("example.com/Source", "source")
	if err != nil {
		t.Fatalf("AddNodeByClass source: %v", err)
	}
	targetID, err := w.AddNodeByClass("example.com/Target", "target")
	if err != nil {
		t.Fatalf("AddNodeByClass target: %v", err)
	}
	if err := w.SetNodePosition(sourceID, "10 20"); err != nil {
		t.Fatalf("SetNodePosition: %v", err)
	}
	w.Undo()
	if got := w.Snapshot().Nodes[sourceID].Position; got != "" {
		t.Fatalf("position after undo = %q, want empty", got)
	}
	w.Redo()
	if got := w.Snapshot().Nodes[sourceID].Position; got != "10 20" {
		t.Fatalf("position after redo = %q, want 10 20", got)
	}
	source := w.Snapshot().Nodes[sourceID]
	target := w.Snapshot().Nodes[targetID]
	linkID, _, err := w.AddLink(source.RightPorts[0], target.LeftPorts[0])
	if err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	withLink := w.Snapshot()
	withLinkConfig := mustSaveUndoHuJSON(t, w)

	var notifications []pasta.WorkspaceNotification
	w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		notifications = append(notifications, notification)
	})
	notifications = nil

	w.RemoveLink(linkID)
	withoutLink := w.Snapshot()
	w.Undo()
	assertWorkspaceSnapshot(t, w, withLink)
	if got := mustSaveUndoHuJSON(t, w); got != withLinkConfig {
		t.Fatalf("config after undo remove link:\n%s\nwant:\n%s", got, withLinkConfig)
	}
	w.Redo()
	assertWorkspaceSnapshot(t, w, withoutLink)

	w.Undo()
	assertWorkspaceSnapshot(t, w, withLink)
	notifications = nil
	w.RemoveNode(sourceID)
	withoutSource := w.Snapshot()
	w.Undo()
	assertWorkspaceSnapshot(t, w, withLink)
	if got := mustSaveUndoHuJSON(t, w); got != withLinkConfig {
		t.Fatalf("config after undo remove node:\n%s\nwant:\n%s", got, withLinkConfig)
	}
	if !reflect.DeepEqual(restoredValues, []string{"", "", "saved"}) {
		t.Fatalf("factory restored values = %#v", restoredValues)
	}
	assertHasNotification(t, notifications, pasta.NotificationNodeRemoved, sourceID)
	assertHasNotification(t, notifications, pasta.NotificationNodeAdded, sourceID)
	assertHasNotification(t, notifications, pasta.NotificationLinkAdded, linkID)

	w.Redo()
	assertWorkspaceSnapshot(t, w, withoutSource)
	w.Undo()
	assertWorkspaceSnapshot(t, w, withLink)
}

func TestWorkspaceUndoRedoIgnoresNonTopologyChangesAndLimitsHistory(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	for i := 0; i < 65; i++ {
		if _, err := w.AddNode(&workspaceNode{}, "example.com/Node"); err != nil {
			t.Fatalf("AddNode %d: %v", i, err)
		}
	}
	for i := 0; i < 65; i++ {
		w.Undo()
	}
	if got := len(w.Snapshot().Nodes); got != 1 {
		t.Fatalf("nodes after undoing 65 adds with 64-entry history = %d, want 1", got)
	}

	nodeID, err := w.AddNode(&workspaceNode{}, "example.com/Typed")
	if err != nil {
		t.Fatalf("AddNode typed: %v", err)
	}
	if err := w.SetNodePrimary(nodeID, "example.com/typeA"); err != nil {
		t.Fatalf("SetNodePrimary: %v", err)
	}
	keeperID, err := w.AddNode(&workspaceNode{}, "example.com/Keeper")
	if err != nil {
		t.Fatalf("AddNode keeper: %v", err)
	}
	w.Undo()
	if _, ok := w.NodeSnapshot(keeperID); ok {
		t.Fatalf("Undo after primary change should consume later add-node entry and remove node %d", keeperID)
	}
	snapshot := w.Snapshot()
	if got := snapshot.Nodes[nodeID].PrimaryType; got != "example.com/typeA" {
		t.Fatalf("primary after undo = %q, want example.com/typeA", got)
	}
}

func TestWorkspaceRestoreConstructorStartsWithEmptyUndoRedoHistory(t *testing.T) {
	cfg := configer.NewMemory(map[string]any{
		"source": map[string]any{
			"Class": "example.com/Source",
			"Links": []any{"out -> [target] in"},
		},
		"target": map[string]any{
			"Class": "example.com/Target",
		},
	})
	w, err := pasta.WorkspaceFromConfig([]pasta.NodeClass{
		testNodeClass{
			name: "example.com/Source",
			params: pasta.NodeClassParams{InitialPorts: []pasta.Port{{
				Direction: "right",
				Name:      "out",
				Types:     []string{pasta.AnyType},
			}}},
		},
		testNodeClass{
			name: "example.com/Target",
			params: pasta.NodeClassParams{InitialPorts: []pasta.Port{{
				Direction: "left",
				Name:      "in",
				Types:     []string{pasta.AnyType},
			}}},
		},
	}, cfg, &StringLoggerFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig: %v", err)
	}
	before := w.Snapshot()
	w.Undo()
	assertWorkspaceSnapshot(t, w, before)
	w.Redo()
	assertWorkspaceSnapshot(t, w, before)
}

func mustAddUndoFactoryClass(t *testing.T, w *pasta.Workspace, class string, ports []pasta.Port, restoredValues *[]string) {
	t.Helper()
	err := w.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{
			name: class,
			params: pasta.NodeClassParams{
				InitialPorts: ports,
			},
		},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			value := ""
			if cfg != nil {
				if got, err := cfg.Get(configer.Path{"value"}); err == nil {
					value, _ = got.(string)
				}
			}
			*restoredValues = append(*restoredValues, value)
			return &saveNode{onSave: func(cfg configer.Config) error {
				return cfg.Set(configer.Path{"value"}, "saved")
			}}, nil
		},
	})
	if err != nil {
		t.Fatalf("AddNodeClass %s: %v", class, err)
	}
}

func mustSaveUndoHuJSON(t *testing.T, w *pasta.Workspace) string {
	t.Helper()
	cfg, err := hujson.Parse([]byte(`{}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return string(cfg.Pack())
}
