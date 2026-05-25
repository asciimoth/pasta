// nolint
package pasta_test

import (
	"reflect"
	"testing"

	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

type captureFormularNode struct {
	workspaceNode
	formularMsgs []any
}

func (n *captureFormularNode) OnFormularMsg(message any) error {
	n.formularMsgs = append(n.formularMsgs, message)
	return n.maybeFail("OnFormularMsg")
}

func TestWorkspaceNodeMenuSubscriptionsAreExplicitAndCached(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	node := &workspaceNode{}
	nodeID, err := w.AddNode(node, "example.com/MenuNode")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	snapshot := formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{
			Type:           formular.MessageMenuSnapshot,
			MenuID:         pasta.NodeMenuID(nodeID),
			MenuGeneration: 1,
		},
		Blocks: []formular.Block{{
			ID:         "state",
			Order:      10,
			Generation: 1,
			Items: []formular.Item{{
				Type:  formular.ItemField,
				ID:    "value",
				Label: "Value",
				Field: &formular.Field{Kind: formular.FieldFloat, Value: float64(3)},
			}},
		}},
	}
	w.SendNodeMenuMsg(nodeID, snapshot)

	var first, second []pasta.WorkspaceNotification
	firstID := w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		first = append(first, notification)
	})
	secondID := w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		second = append(second, notification)
	})
	if len(first) != 1 || first[0].Kind != pasta.NotificationWorkspaceSnapshot {
		t.Fatalf("first initial notifications = %#v", first)
	}
	if len(second) != 1 || second[0].Kind != pasta.NotificationWorkspaceSnapshot {
		t.Fatalf("second initial notifications = %#v", second)
	}

	if !w.SubscribeNodeMenu(nodeID, firstID) {
		t.Fatalf("SubscribeNodeMenu(%d, %d) returned false", nodeID, firstID)
	}
	if len(first) != 2 {
		t.Fatalf("first notifications after menu subscribe = %#v", first)
	}
	forced, ok := first[1].Formular.(formular.MenuSnapshotMessage)
	if !ok {
		t.Fatalf("forced snapshot type = %T", first[1].Formular)
	}
	if !forced.Force || forced.MenuID != pasta.NodeMenuID(nodeID) {
		t.Fatalf("forced snapshot = %#v", forced)
	}
	if len(second) != 1 {
		t.Fatalf("second received node menu without subscription: %#v", second)
	}

	update := formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{
			Type:            formular.MessageBlockSnapshot,
			MenuID:          pasta.NodeMenuID(nodeID),
			MenuGeneration:  1,
			BlockGeneration: 2,
		},
		Block: formular.Block{
			ID:         "state",
			Order:      10,
			Generation: 2,
			Items: []formular.Item{{
				Type:  formular.ItemField,
				ID:    "value",
				Label: "Value",
				Field: &formular.Field{Kind: formular.FieldFloat, Value: float64(5)},
			}},
		},
	}
	w.SendNodeMenuMsg(nodeID, update)
	if len(first) != 3 || first[2].Kind != pasta.NotificationNodeMenu {
		t.Fatalf("first notifications after update = %#v", first)
	}
	if got, ok := first[2].Formular.(formular.BlockSnapshotMessage); !ok || got.Block.Generation != 2 {
		t.Fatalf("menu update = %#v (%T)", first[2].Formular, first[2].Formular)
	}
	if len(second) != 1 {
		t.Fatalf("second received node menu update without subscription: %#v", second)
	}

	if !w.UnsubscribeNodeMenu(nodeID, firstID) {
		t.Fatalf("UnsubscribeNodeMenu(%d, %d) returned false", nodeID, firstID)
	}
	w.SendNodeMenuMsg(nodeID, update)
	if len(first) != 3 {
		t.Fatalf("first received menu after unsubscribe: %#v", first)
	}

	if err := w.ReplaceNode(nodeID, &workspaceNode{}); err != nil {
		t.Fatalf("ReplaceNode: %v", err)
	}
	if !w.SubscribeNodeMenu(nodeID, secondID) {
		t.Fatalf("SubscribeNodeMenu after replacement returned false")
	}
	if len(second) != 1 {
		t.Fatalf("replacement preserved cached menu snapshot: %#v", second)
	}
}

func TestWorkspaceDeliversFrontendFormularMessagesToNode(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	node := &captureFormularNode{}
	nodeID, err := w.AddNode(node, "example.com/MenuNode")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	msg := formular.FieldUpdateMessage{
		MessageBase: formular.MessageBase{
			Type:   formular.MessageFieldUpdate,
			MenuID: pasta.NodeMenuID(nodeID),
		},
		Field: formular.FieldRef{BlockID: "state", FieldID: "value"},
		Value: float64(11),
	}
	w.SendNodeFormularMsg(9999, msg)
	w.SendNodeFormularMsg(nodeID, msg)

	if len(node.formularMsgs) != 1 {
		t.Fatalf("formular messages = %#v", node.formularMsgs)
	}
	if got, ok := node.formularMsgs[0].(formular.FieldUpdateMessage); !ok || !reflect.DeepEqual(got, msg) {
		t.Fatalf("delivered message = %#v (%T), want %#v", node.formularMsgs[0], node.formularMsgs[0], msg)
	}
}

func TestWorkspaceCalculatorMenusRecalculateGraph(t *testing.T) {
	graph, _ := buildCalcGraph(t, []string{
		"c10:sum.a", "c5:sum.b", "c2:sum.c", "c3:sum.d",
		"sum:sub.a", "c4:sub.b", "sub:div.a", "c2:div.b",
		"div:mult.a", "c5:mult.b", "sum:sumResult.in", "sub:subResult.in",
		"div:divResult.in", "mult:final.in",
	})

	state := formular.NewMenuSnapshotState()
	subscriptionID := graph.w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		if notification.Kind == pasta.NotificationNodeMenu {
			state.Apply(notification.Formular)
		}
	})
	for _, node := range graph.nodes {
		if !graph.w.SubscribeNodeMenu(node.id, subscriptionID) {
			t.Fatalf("SubscribeNodeMenu(%d) returned false", node.id)
		}
	}

	wantInitial := map[string]float64{
		"c10": 10, "c5": 5, "c2": 2, "c3": 3, "c4": 4,
		"sum": 20, "sub": 16, "div": 8, "mult": 40,
		"sumResult": 20, "subResult": 16, "divResult": 8, "final": 40,
	}
	assertCalcMenuValues(t, graph, state, wantInitial)

	graph.w.SendNodeFormularMsg(graph.nodes["c10"].id, formular.FieldUpdateMessage{
		MessageBase: formular.MessageBase{
			Type:           formular.MessageFieldUpdate,
			MenuID:         pasta.NodeMenuID(graph.nodes["c10"].id),
			MenuGeneration: 1,
		},
		Field: formular.FieldRef{BlockID: "state", FieldID: "value"},
		Value: float64(20),
	})

	wantRecalculated := map[string]float64{
		"c10": 20, "c5": 5, "c2": 2, "c3": 3, "c4": 4,
		"sum": 30, "sub": 26, "div": 13, "mult": 65,
		"sumResult": 30, "subResult": 26, "divResult": 13, "final": 65,
	}
	if got := graph.states(); !reflect.DeepEqual(got, wantRecalculated) {
		t.Fatalf("states after menu update = %#v, want %#v", got, wantRecalculated)
	}
	assertCalcMenuValues(t, graph, state, wantRecalculated)
}

func assertCalcMenuValues(t *testing.T, graph *calcGraph, state *formular.MenuSnapshotState, want map[string]float64) {
	t.Helper()
	for name, wantValue := range want {
		node := graph.nodes[name]
		snapshot, ok := state.Snapshot(pasta.NodeMenuID(node.id))
		if !ok {
			t.Fatalf("missing menu snapshot for %s", name)
		}
		value, readonly := calcMenuValue(t, snapshot)
		if value != wantValue {
			t.Fatalf("%s menu value = %g, want %g", name, value, wantValue)
		}
		wantReadonly := node.kind != "constant"
		if readonly != wantReadonly {
			t.Fatalf("%s menu readonly = %v, want %v", name, readonly, wantReadonly)
		}
	}
}

func calcMenuValue(t *testing.T, snapshot formular.MenuSnapshotMessage) (float64, bool) {
	t.Helper()
	for _, block := range snapshot.Blocks {
		if block.ID != "state" {
			continue
		}
		for _, item := range block.Items {
			if item.ID == "value" && item.Field != nil {
				value, ok := formularFloat(item.Field.Value)
				if !ok {
					t.Fatalf("menu field value has type %T", item.Field.Value)
				}
				return value, item.Field.Readonly
			}
		}
	}
	t.Fatalf("menu snapshot missing state.value: %#v", snapshot)
	return 0, false
}
