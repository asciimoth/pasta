package main

import (
	"strings"
	"testing"
	"time"

	"github.com/asciimoth/pasta/pasta"
)

func TestNetworkLoopbackHTTPClientServerAndCleanup(t *testing.T) {
	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(NetworkLibrary{}); err != nil {
		t.Fatal(err)
	}
	loopback := createNetworkNode(t, w, NetworkLoopbackClass)
	server := createNetworkNode(t, w, NetworkServerClass)
	client := createNetworkNode(t, w, NetworkClientClass)
	serverLink := linkNetwork(t, w, loopback, server)
	linkNetwork(t, w, loopback, client)

	waitForNetworkState(t, w, server, func(state networkState) bool {
		return strings.HasPrefix(state.Status, "listening ")
	})
	if err := w.TriggerNodeMenuButton(client, pasta.MenuButtonRef{Block: "main", Button: "request"}); err != nil {
		t.Fatal(err)
	}
	waitForNetworkState(t, w, client, func(state networkState) bool {
		return strings.Contains(state.Response, "pasta demo response")
	})
	waitForNetworkState(t, w, server, func(state networkState) bool {
		return state.Requests > 0
	})

	if err := w.DeleteLink(serverLink); err != nil {
		t.Fatal(err)
	}
	if err := w.TriggerNodeMenuButton(client, pasta.MenuButtonRef{Block: "main", Button: "request"}); err != nil {
		t.Fatal(err)
	}
	waitForNetworkState(t, w, client, func(state networkState) bool {
		return state.Error != ""
	})
}

func TestNetworkLoggerMiddlewareLogsOperations(t *testing.T) {
	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(NetworkLibrary{}); err != nil {
		t.Fatal(err)
	}
	loopback := createNetworkNode(t, w, NetworkLoopbackClass)
	logger := createNetworkNode(t, w, NetworkLoggerClass)
	server := createNetworkNode(t, w, NetworkServerClass)
	client := createNetworkNode(t, w, NetworkClientClass)
	linkNetwork(t, w, loopback, logger)
	linkNetwork(t, w, logger, server)
	linkNetwork(t, w, logger, client)

	waitForNetworkState(t, w, server, func(state networkState) bool {
		return strings.HasPrefix(state.Status, "listening ")
	})
	if err := w.TriggerNodeMenuButton(client, pasta.MenuButtonRef{Block: "main", Button: "request"}); err != nil {
		t.Fatal(err)
	}
	waitForNetworkState(t, w, client, func(state networkState) bool {
		return strings.Contains(state.Response, "pasta demo response")
	})
	waitForNetworkState(t, w, logger, func(state networkState) bool {
		return strings.Contains(state.Logs, "listen tcp") &&
			strings.Contains(state.Logs, "dial tcp") &&
			strings.Contains(state.Logs, "ok")
	})
}

func TestNetworkRouterRoutesHTTPByAddressRule(t *testing.T) {
	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(NetworkLibrary{}); err != nil {
		t.Fatal(err)
	}
	loopbackA := createNetworkNode(t, w, NetworkLoopbackClass)
	loopbackB := createNetworkNode(t, w, NetworkLoopbackClass)
	router := createNetworkNode(t, w, NetworkRouterClass)
	serverA := createNetworkNode(t, w, NetworkServerClass)
	serverB := createNetworkNode(t, w, NetworkServerClass)
	client := createNetworkNode(t, w, NetworkClientClass)

	updateNetworkMenu(t, w, serverA,
		pasta.MenuFieldUpdate{Block: "main", Field: "address", Value: "127.0.0.1:101"},
		pasta.MenuFieldUpdate{Block: "main", Field: "response", Value: "loopback A"},
	)
	updateNetworkMenu(t, w, serverB,
		pasta.MenuFieldUpdate{Block: "main", Field: "address", Value: "127.0.0.1:102"},
		pasta.MenuFieldUpdate{Block: "main", Field: "response", Value: "loopback B"},
	)
	if _, err := w.UpdateNodeMenuState(router, pasta.MenuStateUpdate{
		Repeats: []pasta.MenuRepeatUpdate{{
			Block:  "main",
			Repeat: "rules",
			Items: []pasta.MenuRepeatItemState{
				{ID: "rule-1", Fields: map[string]any{"address": ":101$", "slot": int64(1)}},
				{ID: "rule-2", Fields: map[string]any{"address": ":102$", "slot": int64(2)}},
			},
		}},
	}); err != nil {
		t.Fatalf("UpdateNodeMenuState(router rules) error = %v", err)
	}

	linkNetwork(t, w, loopbackA, serverA)
	linkNetwork(t, w, loopbackB, serverB)
	linkNetwork(t, w, router, client)
	linkNetworkSlot(t, w, loopbackA, router, 1)
	linkNetworkSlot(t, w, loopbackB, router, 2)

	waitForNetworkState(t, w, serverA, func(state networkState) bool {
		return strings.HasPrefix(state.Status, "listening ")
	})
	waitForNetworkState(t, w, serverB, func(state networkState) bool {
		return strings.HasPrefix(state.Status, "listening ")
	})

	updateNetworkMenu(t, w, client, pasta.MenuFieldUpdate{Block: "main", Field: "address", Value: "http://127.0.0.1:101/"})
	if err := w.TriggerNodeMenuButton(client, pasta.MenuButtonRef{Block: "main", Button: "request"}); err != nil {
		t.Fatal(err)
	}
	waitForNetworkState(t, w, client, func(state networkState) bool {
		return strings.Contains(state.Response, "loopback A")
	})

	updateNetworkMenu(t, w, client, pasta.MenuFieldUpdate{Block: "main", Field: "address", Value: "http://127.0.0.1:102/"})
	if err := w.TriggerNodeMenuButton(client, pasta.MenuButtonRef{Block: "main", Button: "request"}); err != nil {
		t.Fatal(err)
	}
	waitForNetworkState(t, w, client, func(state networkState) bool {
		return strings.Contains(state.Response, "loopback B")
	})

	updateNetworkMenu(t, w, client, pasta.MenuFieldUpdate{Block: "main", Field: "address", Value: "http://127.0.0.1:103/"})
	if err := w.TriggerNodeMenuButton(client, pasta.MenuButtonRef{Block: "main", Button: "request"}); err != nil {
		t.Fatal(err)
	}
	waitForNetworkState(t, w, client, func(state networkState) bool {
		return state.Error != ""
	})
}

func TestNetworkServerResponseMenuFieldControlsBody(t *testing.T) {
	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(NetworkLibrary{}); err != nil {
		t.Fatal(err)
	}
	loopback := createNetworkNode(t, w, NetworkLoopbackClass)
	server := createNetworkNode(t, w, NetworkServerClass)
	client := createNetworkNode(t, w, NetworkClientClass)
	linkNetwork(t, w, loopback, server)
	linkNetwork(t, w, loopback, client)

	if _, err := w.UpdateNodeMenuState(server, pasta.MenuStateUpdate{
		Fields: []pasta.MenuFieldUpdate{{
			Block: "main",
			Field: "response",
			Value: "custom server response",
		}},
	}); err != nil {
		t.Fatalf("UpdateNodeMenuState(response) error = %v", err)
	}

	waitForNetworkState(t, w, server, func(state networkState) bool {
		return strings.HasPrefix(state.Status, "listening ")
	})
	if err := w.TriggerNodeMenuButton(client, pasta.MenuButtonRef{Block: "main", Button: "request"}); err != nil {
		t.Fatal(err)
	}
	waitForNetworkState(t, w, client, func(state networkState) bool {
		return strings.Contains(state.Response, "custom server response")
	})
}

func TestNetworkClientAddressMenuUpdateAcceptsCurrentVersion(t *testing.T) {
	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(NetworkLibrary{}); err != nil {
		t.Fatal(err)
	}
	client := createNetworkNode(t, w, NetworkClientClass)
	menu, ok := w.NodeMenu(client)
	if !ok {
		t.Fatal("client menu missing")
	}

	updated, err := w.UpdateNodeMenuState(client, pasta.MenuStateUpdate{
		Version: menu.Version,
		Fields: []pasta.MenuFieldUpdate{{
			Block: "main",
			Field: "address",
			Value: "http://127.0.0.1:18080/",
		}},
	})
	if err != nil {
		t.Fatalf("UpdateNodeMenuState(address) error = %v", err)
	}
	if updated.Version != menu.Version+1 {
		t.Fatalf("updated menu version = %d, want %d", updated.Version, menu.Version+1)
	}
	waitForNetworkState(t, w, client, func(state networkState) bool {
		return state.Address == "http://127.0.0.1:18080/"
	})
}

func TestNetworkLoggerOutputSurvivesUpstreamReattach(t *testing.T) {
	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(NetworkLibrary{}); err != nil {
		t.Fatal(err)
	}
	loopback := createNetworkNode(t, w, NetworkLoopbackClass)
	logger := createNetworkNode(t, w, NetworkLoggerClass)
	server := createNetworkNode(t, w, NetworkServerClass)
	client := createNetworkNode(t, w, NetworkClientClass)
	upstream := linkNetwork(t, w, loopback, logger)
	linkNetwork(t, w, logger, server)
	linkNetwork(t, w, logger, client)

	waitForNetworkState(t, w, server, func(state networkState) bool {
		return strings.HasPrefix(state.Status, "listening ")
	})
	if err := w.TriggerNodeMenuButton(client, pasta.MenuButtonRef{Block: "main", Button: "request"}); err != nil {
		t.Fatal(err)
	}
	waitForNetworkState(t, w, client, func(state networkState) bool {
		return strings.Contains(state.Response, "pasta demo response")
	})

	if err := w.DeleteLink(upstream); err != nil {
		t.Fatal(err)
	}
	upstream = linkNetwork(t, w, loopback, logger)
	_ = upstream
	waitForNetworkState(t, w, server, func(state networkState) bool {
		return strings.HasPrefix(state.Status, "listening ") && state.Error == ""
	})
	if err := w.TriggerNodeMenuButton(client, pasta.MenuButtonRef{Block: "main", Button: "request"}); err != nil {
		t.Fatal(err)
	}
	waitForNetworkState(t, w, client, func(state networkState) bool {
		return strings.Contains(state.Response, "pasta demo response") && state.Error == ""
	})
}

func TestNetworkLoggerInputCanAttachBeforeUpstream(t *testing.T) {
	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(NetworkLibrary{}); err != nil {
		t.Fatal(err)
	}
	loopback := createNetworkNode(t, w, NetworkLoopbackClass)
	logger := createNetworkNode(t, w, NetworkLoggerClass)
	server := createNetworkNode(t, w, NetworkServerClass)
	client := createNetworkNode(t, w, NetworkClientClass)

	linkNetwork(t, w, logger, server)
	linkNetwork(t, w, logger, client)
	linkNetwork(t, w, loopback, logger)

	waitForNetworkState(t, w, server, func(state networkState) bool {
		return strings.HasPrefix(state.Status, "listening ")
	})
	if err := w.TriggerNodeMenuButton(client, pasta.MenuButtonRef{Block: "main", Button: "request"}); err != nil {
		t.Fatal(err)
	}
	waitForNetworkState(t, w, client, func(state networkState) bool {
		return strings.Contains(state.Response, "pasta demo response")
	})
	waitForNetworkState(t, w, logger, func(state networkState) bool {
		return strings.Contains(state.Logs, "dial tcp") && strings.Contains(state.Logs, "ok")
	})
}

func TestNetworkOutputAllowsOnlyOneLink(t *testing.T) {
	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(NetworkLibrary{}); err != nil {
		t.Fatal(err)
	}
	loopback := createNetworkNode(t, w, NetworkLoopbackClass)
	server := createNetworkNode(t, w, NetworkServerClass)
	linkNetwork(t, w, loopback, server)
	if _, err := w.CreateLink(
		pasta.FullPortID{Node: loopback, Port: NetworkInput},
		pasta.FullPortID{Node: server, Port: NetworkOutput},
		pasta.LinkOptions{Type: NetworkType},
	); err == nil {
		t.Fatal("second network output link succeeded, want multiplicity error")
	}
}

func createNetworkNode(t *testing.T, w *pasta.Workspace, class string) pasta.NodeID {
	t.Helper()
	id, err := w.CreateNode(class, pasta.NodeOptions{})
	if err != nil {
		t.Fatalf("CreateNode(%s) error = %v", class, err)
	}
	return id
}

func linkNetwork(t *testing.T, w *pasta.Workspace, inputNode, outputNode pasta.NodeID) pasta.LinkID {
	t.Helper()
	link, err := w.CreateLink(
		pasta.FullPortID{Node: inputNode, Port: NetworkInput},
		pasta.FullPortID{Node: outputNode, Port: NetworkOutput},
		pasta.LinkOptions{Type: NetworkType},
	)
	if err != nil {
		t.Fatalf("CreateLink(%s <- %s) error = %v", inputNode, outputNode, err)
	}
	return link
}

func linkNetworkSlot(t *testing.T, w *pasta.Workspace, inputNode, outputNode pasta.NodeID, slot int) pasta.LinkID {
	t.Helper()
	link, err := w.CreateLink(
		pasta.FullPortID{Node: inputNode, Port: NetworkInput},
		pasta.FullPortID{Node: outputNode, Port: networkRouterOutput(slot)},
		pasta.LinkOptions{Type: NetworkType},
	)
	if err != nil {
		t.Fatalf("CreateLink(%s <- %s slot %d) error = %v", inputNode, outputNode, slot, err)
	}
	return link
}

func updateNetworkMenu(t *testing.T, w *pasta.Workspace, node pasta.NodeID, fields ...pasta.MenuFieldUpdate) {
	t.Helper()
	if _, err := w.UpdateNodeMenuState(node, pasta.MenuStateUpdate{Fields: fields}); err != nil {
		t.Fatalf("UpdateNodeMenuState(%s) error = %v", node, err)
	}
}

func waitForNetworkState(t *testing.T, w *pasta.Workspace, node pasta.NodeID, ok func(networkState) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snap, exists := w.Node(node)
		if !exists {
			t.Fatalf("Node(%s) missing", node)
		}
		state := networkStateFromAny(snap.Dynamic.Private)
		if ok(state) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	snap, _ := w.Node(node)
	t.Fatalf("network node %s did not reach expected state; last=%#v", node, networkStateFromAny(snap.Dynamic.Private))
}
