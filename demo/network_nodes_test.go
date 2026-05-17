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
