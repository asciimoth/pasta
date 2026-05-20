package main

import (
	"strings"
	"testing"
	"time"

	"github.com/asciimoth/pasta/pasta"
)

func TestTunVTunSpooferLoopbackTopologyHTTPAndLifecycle(t *testing.T) {
	w := newTunTestWorkspace(t)
	ids := createSpooferTopology(t, w)
	assertTunHTTP(t, w, ids.client, ids.server, "spoofer response")

	if err := w.DeleteLink(ids.tunLink); err != nil {
		t.Fatal(err)
	}
	ids.tunLink = linkTun(t, w, ids.spoofer, TunInputA, ids.vtun)
	assertTunHTTP(t, w, ids.client, ids.server, "spoofer response")

	data, err := w.SaveWithRuntimeState()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Restore(data); err != nil {
		t.Fatal(err)
	}
	assertTunHTTP(t, w, ids.client, ids.server, "spoofer response")

	clip, err := w.Copy([]pasta.NodeID{ids.server, ids.client, ids.loopback, ids.spoofer, ids.vtun})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.Paste(clip); err != nil {
		t.Fatal(err)
	}
}

func TestTunCopyTopologyHTTPAndLifecycle(t *testing.T) {
	w := newTunTestWorkspace(t)
	ids := createCopyTopology(t, w)
	assertTunHTTP(t, w, ids.client, ids.server, "copy response")

	if err := w.DeleteLink(ids.tunALink); err != nil {
		t.Fatal(err)
	}
	ids.tunALink = linkTun(t, w, ids.copy, TunInputA, ids.clientVTun)
	assertTunHTTP(t, w, ids.client, ids.server, "copy response")

	data, err := w.SaveWithRuntimeState()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Restore(data); err != nil {
		t.Fatal(err)
	}
	assertTunHTTP(t, w, ids.client, ids.server, "copy response")

	clip, err := w.Copy([]pasta.NodeID{ids.server, ids.client, ids.serverVTun, ids.clientVTun, ids.copy})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.Paste(clip); err != nil {
		t.Fatal(err)
	}
}

func newTunTestWorkspace(t *testing.T) *pasta.Workspace {
	t.Helper()
	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(NetworkLibrary{}); err != nil {
		t.Fatal(err)
	}
	if err := w.RegisterLibrary(TunLibrary{}); err != nil {
		t.Fatal(err)
	}
	return w
}

type spooferTopologyIDs struct {
	server   pasta.NodeID
	client   pasta.NodeID
	loopback pasta.NodeID
	vtun     pasta.NodeID
	spoofer  pasta.NodeID
	tunLink  pasta.LinkID
}

func createSpooferTopology(t *testing.T, w *pasta.Workspace) spooferTopologyIDs {
	t.Helper()
	ids := spooferTopologyIDs{
		server:   createNetworkNode(t, w, NetworkServerClass),
		client:   createNetworkNode(t, w, NetworkClientClass),
		loopback: createNetworkNode(t, w, NetworkLoopbackClass),
		vtun:     createNetworkNode(t, w, TunVTunClass),
		spoofer:  createNetworkNode(t, w, TunSpooferClass),
	}
	updateNetworkMenu(t, w, ids.server,
		pasta.MenuFieldUpdate{Block: "main", Field: "address", Value: "10.90.0.10:8080"},
		pasta.MenuFieldUpdate{Block: "main", Field: "response", Value: "spoofer response"},
	)
	updateNetworkMenu(t, w, ids.client,
		pasta.MenuFieldUpdate{Block: "main", Field: "address", Value: "http://10.90.0.10:8080/"},
	)
	updateTunMenu(t, w, ids.vtun,
		pasta.MenuFieldUpdate{Block: "main", Field: "localAddrs", Value: "10.90.0.2"},
		pasta.MenuFieldUpdate{Block: "main", Field: "dnsServers", Value: "10.90.0.1"},
	)
	linkNetwork(t, w, ids.loopback, ids.server)
	linkNetwork(t, w, ids.vtun, ids.client)
	ids.tunLink = linkTun(t, w, ids.spoofer, TunInputA, ids.vtun)
	linkNetwork(t, w, ids.loopback, ids.spoofer)
	return ids
}

type copyTopologyIDs struct {
	server     pasta.NodeID
	client     pasta.NodeID
	serverVTun pasta.NodeID
	clientVTun pasta.NodeID
	copy       pasta.NodeID
	tunALink   pasta.LinkID
	tunBLink   pasta.LinkID
}

func createCopyTopology(t *testing.T, w *pasta.Workspace) copyTopologyIDs {
	t.Helper()
	ids := copyTopologyIDs{
		server:     createNetworkNode(t, w, NetworkServerClass),
		client:     createNetworkNode(t, w, NetworkClientClass),
		serverVTun: createNetworkNode(t, w, TunVTunClass),
		clientVTun: createNetworkNode(t, w, TunVTunClass),
		copy:       createNetworkNode(t, w, TunCopyClass),
	}
	updateNetworkMenu(t, w, ids.server,
		pasta.MenuFieldUpdate{Block: "main", Field: "address", Value: "10.91.0.10:8080"},
		pasta.MenuFieldUpdate{Block: "main", Field: "response", Value: "copy response"},
	)
	updateNetworkMenu(t, w, ids.client,
		pasta.MenuFieldUpdate{Block: "main", Field: "address", Value: "http://10.91.0.10:8080/"},
	)
	updateTunMenu(t, w, ids.serverVTun,
		pasta.MenuFieldUpdate{Block: "main", Field: "localAddrs", Value: "10.91.0.10"},
	)
	updateTunMenu(t, w, ids.clientVTun,
		pasta.MenuFieldUpdate{Block: "main", Field: "localAddrs", Value: "10.91.0.20"},
		pasta.MenuFieldUpdate{Block: "main", Field: "dnsServers", Value: "10.91.0.10"},
	)
	linkNetwork(t, w, ids.serverVTun, ids.server)
	linkNetwork(t, w, ids.clientVTun, ids.client)
	ids.tunALink = linkTun(t, w, ids.copy, TunInputA, ids.clientVTun)
	ids.tunBLink = linkTun(t, w, ids.copy, TunInputB, ids.serverVTun)
	return ids
}

func linkTun(t *testing.T, w *pasta.Workspace, inputNode pasta.NodeID, inputPort pasta.PortID, outputNode pasta.NodeID) pasta.LinkID {
	t.Helper()
	link, err := w.CreateLink(
		pasta.FullPortID{Node: inputNode, Port: inputPort},
		pasta.FullPortID{Node: outputNode, Port: TunOutput},
		pasta.LinkOptions{Type: TunType},
	)
	if err != nil {
		t.Fatalf("CreateLink(%s:%s <- %s:%s) error = %v", inputNode, inputPort, outputNode, TunOutput, err)
	}
	return link
}

func updateTunMenu(t *testing.T, w *pasta.Workspace, node pasta.NodeID, fields ...pasta.MenuFieldUpdate) {
	t.Helper()
	if _, err := w.UpdateNodeMenuState(node, pasta.MenuStateUpdate{Fields: fields}); err != nil {
		t.Fatalf("UpdateNodeMenuState(%s) error = %v", node, err)
	}
}

func assertTunHTTP(t *testing.T, w *pasta.Workspace, client, server pasta.NodeID, body string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := w.TriggerNodeMenuButton(client, pasta.MenuButtonRef{Block: "main", Button: "request"}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
		snap, exists := w.Node(client)
		if !exists {
			t.Fatalf("Node(%s) missing", client)
		}
		state := networkStateFromAny(snap.Dynamic.Private)
		if strings.Contains(state.Response, body) && state.Error == "" {
			return
		}
	}
	snap, _ := w.Node(client)
	t.Fatalf("client %s did not receive %q; last=%#v", client, body, networkStateFromAny(snap.Dynamic.Private))
}
