package pasta_test

import (
	"testing"

	"github.com/asciimoth/pasta/pasta"
)

func TestWorkspaceNotificationSubscriptionObservesSnapshotsAndMutations(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	nodeAID, err := w.AddNode(&workspaceNode{}, "example.com/NodeA")
	if err != nil {
		t.Fatalf("AddNode A before subscribe: %v", err)
	}
	initial := pasta.WorkspaceSnapshot{
		Nodes: map[uint64]pasta.NodeSnapshot{
			nodeAID: {Class: "example.com/NodeA"},
		},
	}

	var notifications []pasta.WorkspaceNotification
	subscriptionID := w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		notifications = append(notifications, notification)
	})
	if subscriptionID == 0 {
		t.Fatal("SubscribeNotifications returned zero subscription id")
	}
	if len(notifications) != 1 {
		t.Fatalf("initial notifications = %d, want 1", len(notifications))
	}
	initialNotification := notifications[0]
	if initialNotification.SubscriptionID != subscriptionID {
		t.Fatalf("initial subscription id = %d, want %d", initialNotification.SubscriptionID, subscriptionID)
	}
	if initialNotification.Kind != pasta.NotificationWorkspaceSnapshot {
		t.Fatalf("initial notification kind = %q, want %q", initialNotification.Kind, pasta.NotificationWorkspaceSnapshot)
	}
	if initialNotification.Snapshot == nil || !equalWorkspaceSnapshot(*initialNotification.Snapshot, initial) {
		t.Fatalf("initial snapshot = %#v, want %#v", initialNotification.Snapshot, initial)
	}

	nodeBID, err := w.AddNode(&workspaceNode{}, "example.com/NodeB")
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}
	left, err := w.AddPort(pasta.Port{
		Node:      nodeAID,
		Direction: "left",
		Types:     []string{"example.com/typeA"},
	})
	if err != nil {
		t.Fatalf("AddPort left: %v", err)
	}
	right, err := w.AddPort(pasta.Port{
		Node:      nodeBID,
		Direction: "right",
		Types:     []string{"example.com/typeA"},
	})
	if err != nil {
		t.Fatalf("AddPort right: %v", err)
	}
	link, _, err := w.AddLink(left, right)
	if err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	if err := w.SetNodePrimary(nodeAID, "example.com/typeA"); err != nil {
		t.Fatalf("SetNodePrimary: %v", err)
	}
	if err := w.SetPortName(left, "input"); err != nil {
		t.Fatalf("SetPortName: %v", err)
	}
	w.RemoveLink(link)
	w.RemovePort(left)
	w.RemoveNode(nodeBID)

	want := []notificationMatch{
		{kind: pasta.NotificationWorkspaceSnapshot},
		{kind: pasta.NotificationNodeAdded, id: nodeBID},
		{kind: pasta.NotificationPortAdded, id: left},
		{kind: pasta.NotificationNodeUpdated, id: nodeAID},
		{kind: pasta.NotificationPortAdded, id: right},
		{kind: pasta.NotificationNodeUpdated, id: nodeBID},
		{kind: pasta.NotificationLinkAdded, id: link},
		{kind: pasta.NotificationPortUpdated, id: left},
		{kind: pasta.NotificationPortUpdated, id: right},
		{kind: pasta.NotificationNodeUpdated, id: nodeAID},
		{kind: pasta.NotificationPortUpdated, id: left},
		{kind: pasta.NotificationLinkRemoved, id: link},
		{kind: pasta.NotificationPortUpdated, id: left},
		{kind: pasta.NotificationPortUpdated, id: right},
		{kind: pasta.NotificationPortRemoved, id: left},
		{kind: pasta.NotificationNodeUpdated, id: nodeAID},
		{kind: pasta.NotificationPortRemoved, id: right},
		{kind: pasta.NotificationNodeRemoved, id: nodeBID},
	}
	assertNotificationMatches(t, notifications, want)

	assertNodeNotification(t, notifications[1], pasta.NodeSnapshot{Class: "example.com/NodeB"})
	assertPortNotification(t, notifications[2], pasta.PortSnapshot{
		Node:      nodeAID,
		Direction: "left",
		Types:     []string{"example.com/typeA"},
	})
	assertNodeNotification(t, notifications[3], pasta.NodeSnapshot{
		Class:     "example.com/NodeA",
		LeftPorts: []uint64{left},
	})
	assertLinkNotification(t, notifications[6], pasta.LinkSnapshot{
		Type:          "example.com/typeA",
		LeftPort:      left,
		LeftPortNode:  nodeAID,
		RightPort:     right,
		RightPortNode: nodeBID,
	})
	assertPortNotification(t, notifications[7], pasta.PortSnapshot{
		Node:      nodeAID,
		Direction: "left",
		Types:     []string{"example.com/typeA"},
		Links:     []uint64{link},
	})
	assertNodeNotification(t, notifications[9], pasta.NodeSnapshot{
		Class:       "example.com/NodeA",
		PrimaryType: "example.com/typeA",
		LeftPorts:   []uint64{left},
	})
	assertPortNotification(t, notifications[10], pasta.PortSnapshot{
		Node:      nodeAID,
		Direction: "left",
		Name:      "input",
		Types:     []string{"example.com/typeA"},
		Links:     []uint64{link},
	})
	assertLinkNotification(t, notifications[11], pasta.LinkSnapshot{
		Type:          "example.com/typeA",
		LeftPort:      left,
		LeftPortNode:  nodeAID,
		RightPort:     right,
		RightPortNode: nodeBID,
	})
	assertPortNotification(t, notifications[14], pasta.PortSnapshot{
		Node:      nodeAID,
		Direction: "left",
		Name:      "input",
		Types:     []string{"example.com/typeA"},
	})
	assertNodeNotification(t, notifications[17], pasta.NodeSnapshot{
		Class:      "example.com/NodeB",
		RightPorts: []uint64{right},
	})

	if !w.UnsubscribeNotifications(subscriptionID) {
		t.Fatalf("UnsubscribeNotifications(%d) returned false", subscriptionID)
	}
	if w.UnsubscribeNotifications(subscriptionID) {
		t.Fatalf("second UnsubscribeNotifications(%d) returned true", subscriptionID)
	}
	beforeUnsubscribedMutation := len(notifications)
	if _, err := w.AddNode(&workspaceNode{}, "example.com/NodeC"); err != nil {
		t.Fatalf("AddNode C after unsubscribe: %v", err)
	}
	if len(notifications) != beforeUnsubscribedMutation {
		t.Fatalf("received %d notifications after unsubscribe, want %d", len(notifications), beforeUnsubscribedMutation)
	}
}

func TestWorkspaceNotificationMultipleSubscriptions(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	var first []pasta.WorkspaceNotification
	firstID := w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		first = append(first, notification)
	})
	var second []pasta.WorkspaceNotification
	secondID := w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		second = append(second, notification)
	})

	if firstID == 0 || secondID == 0 || firstID == secondID {
		t.Fatalf("subscription ids = %d, %d; want distinct non-zero ids", firstID, secondID)
	}

	nodeA, err := w.AddNode(&workspaceNode{}, "example.com/NodeA")
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	portA, err := w.AddPort(pasta.Port{
		Node:      nodeA,
		Direction: "left",
		Types:     []string{"example.com/typeA"},
	})
	if err != nil {
		t.Fatalf("AddPort A: %v", err)
	}

	wantActive := []notificationMatch{
		{kind: pasta.NotificationWorkspaceSnapshot},
		{kind: pasta.NotificationNodeAdded, id: nodeA},
		{kind: pasta.NotificationPortAdded, id: portA},
		{kind: pasta.NotificationNodeUpdated, id: nodeA},
	}
	assertNotificationMatches(t, first, wantActive)
	assertNotificationMatches(t, second, wantActive)
	assertSubscriptionIDs(t, first, firstID)
	assertSubscriptionIDs(t, second, secondID)

	if !w.UnsubscribeNotifications(firstID) {
		t.Fatalf("UnsubscribeNotifications(%d) returned false", firstID)
	}
	firstBefore := len(first)
	secondBefore := len(second)

	nodeB, err := w.AddNode(&workspaceNode{}, "example.com/NodeB")
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}
	if len(first) != firstBefore {
		t.Fatalf("first watcher received %d notifications after unsubscribe, want %d", len(first), firstBefore)
	}
	if len(second) != secondBefore+1 {
		t.Fatalf("second watcher notifications = %d, want %d", len(second), secondBefore+1)
	}
	if got := second[len(second)-1]; got.Kind != pasta.NotificationNodeAdded || got.ID != nodeB || got.SubscriptionID != secondID {
		t.Fatalf("second watcher last notification = %#v, want node_added for %d", got, nodeB)
	}
}

func TestWorkspaceNotificationSubscriberCanRequestFullSnapshot(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})

	var first []pasta.WorkspaceNotification
	firstID := w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		first = append(first, notification)
	})
	var second []pasta.WorkspaceNotification
	secondID := w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		second = append(second, notification)
	})
	first = nil
	second = nil

	w.Lock()
	if !w.RequestFullSnapshot(firstID) {
		t.Fatalf("RequestFullSnapshot(%d) returned false", firstID)
	}
	nodeID, err := w.AddNode(&workspaceNode{}, "example.com/NodeA")
	if err != nil {
		t.Fatalf("AddNode while snapshot request pending: %v", err)
	}
	w.Unlock()

	assertNotificationMatches(t, first, []notificationMatch{
		{kind: pasta.NotificationWorkspaceSnapshot},
		{kind: pasta.NotificationNodeAdded, id: nodeID},
	})
	assertNotificationMatches(t, second, []notificationMatch{
		{kind: pasta.NotificationNodeAdded, id: nodeID},
	})
	if first[0].Snapshot == nil {
		t.Fatal("requested full snapshot notification has nil snapshot")
	}
	if _, present := first[0].Snapshot.Nodes[nodeID]; !present {
		t.Fatalf("requested snapshot = %#v, want node %d added after request", first[0].Snapshot, nodeID)
	}
	if !w.RequestFullSnapshot(secondID) {
		t.Fatalf("RequestFullSnapshot(%d) returned false", secondID)
	}
	if got := second[len(second)-1]; got.Kind != pasta.NotificationWorkspaceSnapshot || got.Snapshot == nil {
		t.Fatalf("second last notification = %#v, want full snapshot", got)
	}

	if w.RequestFullSnapshot(999) {
		t.Fatal("RequestFullSnapshot missing subscription returned true")
	}
	if !w.UnsubscribeNotifications(firstID) {
		t.Fatalf("UnsubscribeNotifications(%d) returned false", firstID)
	}
	if w.RequestFullSnapshot(firstID) {
		t.Fatal("RequestFullSnapshot unsubscribed subscription returned true")
	}
}

type notificationMatch struct {
	kind pasta.NotificationKind
	id   uint64
}

func assertNotificationMatches(t *testing.T, got []pasta.WorkspaceNotification, want []notificationMatch) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("notifications length = %d, want %d\ngot: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Kind != want[i].kind || got[i].ID != want[i].id {
			t.Fatalf("notification[%d] = {%q, %d}, want {%q, %d}", i, got[i].Kind, got[i].ID, want[i].kind, want[i].id)
		}
	}
}

func assertSubscriptionIDs(t *testing.T, notifications []pasta.WorkspaceNotification, want uint64) {
	t.Helper()

	for i, notification := range notifications {
		if notification.SubscriptionID != want {
			t.Fatalf("notification[%d] subscription id = %d, want %d", i, notification.SubscriptionID, want)
		}
	}
}

func assertNodeNotification(t *testing.T, notification pasta.WorkspaceNotification, want pasta.NodeSnapshot) {
	t.Helper()

	if notification.Node == nil || !equalNodeSnapshot(*notification.Node, want) {
		t.Fatalf("node notification = %#v, want %#v", notification.Node, want)
	}
}

func assertPortNotification(t *testing.T, notification pasta.WorkspaceNotification, want pasta.PortSnapshot) {
	t.Helper()

	if notification.Port == nil || !equalPortSnapshot(*notification.Port, want) {
		t.Fatalf("port notification = %#v, want %#v", notification.Port, want)
	}
}

func assertLinkNotification(t *testing.T, notification pasta.WorkspaceNotification, want pasta.LinkSnapshot) {
	t.Helper()

	if notification.Link == nil || *notification.Link != want {
		t.Fatalf("link notification = %#v, want %#v", notification.Link, want)
	}
}
