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

func TestTunJoinerSplitterTopologyHTTPAndDynamicPorts(t *testing.T) {
	w := newTunTestWorkspace(t)
	ids := createJoinerSplitterTopology(t, w)
	assertTunHTTP(t, w, ids.clientA, ids.serverA, "join split A response")
	assertTunHTTP(t, w, ids.clientB, ids.serverB, "join split B response")
	assertJoinerInputPorts(t, w, ids.joiner, 3)

	if err := w.DeleteLink(ids.joinerSecondaryLink); err != nil {
		t.Fatal(err)
	}
	assertJoinerInputPorts(t, w, ids.joiner, 2)
	ids.joinerSecondaryLink = linkTunOutput(t, w, ids.joiner, tunJoinerInput(1), ids.splitter, tunSplitterOutput(2))
	assertJoinerInputPorts(t, w, ids.joiner, 3)
	assertTunHTTP(t, w, ids.clientB, ids.serverB, "join split B response")

	data, err := w.SaveWithRuntimeState()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Restore(data); err != nil {
		t.Fatal(err)
	}
	assertJoinerInputPorts(t, w, ids.joiner, 3)
	assertTunHTTP(t, w, ids.clientA, ids.serverA, "join split A response")
	assertTunHTTP(t, w, ids.clientB, ids.serverB, "join split B response")
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

type joinerSplitterTopologyIDs struct {
	serverA             pasta.NodeID
	serverB             pasta.NodeID
	clientA             pasta.NodeID
	clientB             pasta.NodeID
	loopback            pasta.NodeID
	vtun                pasta.NodeID
	splitter            pasta.NodeID
	joiner              pasta.NodeID
	spoofer             pasta.NodeID
	joinerSecondaryLink pasta.LinkID
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

func createJoinerSplitterTopology(t *testing.T, w *pasta.Workspace) joinerSplitterTopologyIDs {
	t.Helper()
	ids := joinerSplitterTopologyIDs{
		serverA:  createNetworkNode(t, w, NetworkServerClass),
		serverB:  createNetworkNode(t, w, NetworkServerClass),
		clientA:  createNetworkNode(t, w, NetworkClientClass),
		clientB:  createNetworkNode(t, w, NetworkClientClass),
		loopback: createNetworkNode(t, w, NetworkLoopbackClass),
		vtun:     createNetworkNode(t, w, TunVTunClass),
		splitter: createNetworkNode(t, w, TunSplitterClass),
		joiner:   createNetworkNode(t, w, TunJoinerClass),
		spoofer:  createNetworkNode(t, w, TunSpooferClass),
	}
	updateNetworkMenu(t, w, ids.serverA,
		pasta.MenuFieldUpdate{Block: "main", Field: "address", Value: "10.92.0.10:8081"},
		pasta.MenuFieldUpdate{Block: "main", Field: "response", Value: "join split A response"},
	)
	updateNetworkMenu(t, w, ids.serverB,
		pasta.MenuFieldUpdate{Block: "main", Field: "address", Value: "10.92.0.20:8082"},
		pasta.MenuFieldUpdate{Block: "main", Field: "response", Value: "join split B response"},
	)
	updateNetworkMenu(t, w, ids.clientA,
		pasta.MenuFieldUpdate{Block: "main", Field: "address", Value: "http://10.92.0.10:8081/"},
	)
	updateNetworkMenu(t, w, ids.clientB,
		pasta.MenuFieldUpdate{Block: "main", Field: "address", Value: "http://10.92.0.20:8082/"},
	)
	updateTunMenu(t, w, ids.vtun,
		pasta.MenuFieldUpdate{Block: "main", Field: "localAddrs", Value: "10.92.0.2"},
		pasta.MenuFieldUpdate{Block: "main", Field: "dnsServers", Value: "10.92.0.1"},
	)
	updateTunRepeats(t, w, ids.splitter, pasta.MenuRepeatUpdate{
		Block:  "main",
		Repeat: "splitRules",
		Items: []pasta.MenuRepeatItemState{
			{ID: "rule-1", Fields: map[string]any{"address": `^10\.92\.0\.10$`, "slot": int64(1)}},
			{ID: "rule-2", Fields: map[string]any{"address": `^10\.92\.0\.20$`, "slot": int64(2)}},
		},
	})
	linkNetwork(t, w, ids.loopback, ids.serverA)
	linkNetwork(t, w, ids.loopback, ids.serverB)
	linkNetwork(t, w, ids.vtun, ids.clientA)
	linkNetwork(t, w, ids.vtun, ids.clientB)
	linkNetwork(t, w, ids.loopback, ids.spoofer)
	linkTun(t, w, ids.splitter, TunInputA, ids.vtun)
	linkTunOutput(t, w, ids.joiner, TunJoinerDefaultInput, ids.splitter, tunSplitterOutput(1))
	ids.joinerSecondaryLink = linkTunOutput(t, w, ids.joiner, tunJoinerInput(1), ids.splitter, tunSplitterOutput(2))
	linkTun(t, w, ids.spoofer, TunInputA, ids.joiner)
	return ids
}

func linkTun(t *testing.T, w *pasta.Workspace, inputNode pasta.NodeID, inputPort pasta.PortID, outputNode pasta.NodeID) pasta.LinkID {
	t.Helper()
	return linkTunOutput(t, w, inputNode, inputPort, outputNode, TunOutput)
}

func linkTunOutput(t *testing.T, w *pasta.Workspace, inputNode pasta.NodeID, inputPort pasta.PortID, outputNode pasta.NodeID, outputPort pasta.PortID) pasta.LinkID {
	t.Helper()
	link, err := w.CreateLink(
		pasta.FullPortID{Node: inputNode, Port: inputPort},
		pasta.FullPortID{Node: outputNode, Port: outputPort},
		pasta.LinkOptions{Type: TunType},
	)
	if err != nil {
		t.Fatalf("CreateLink(%s:%s <- %s:%s) error = %v", inputNode, inputPort, outputNode, outputPort, err)
	}
	return link
}

func updateTunMenu(t *testing.T, w *pasta.Workspace, node pasta.NodeID, fields ...pasta.MenuFieldUpdate) {
	t.Helper()
	if _, err := w.UpdateNodeMenuState(node, pasta.MenuStateUpdate{Fields: fields}); err != nil {
		t.Fatalf("UpdateNodeMenuState(%s) error = %v", node, err)
	}
}

func updateTunRepeats(t *testing.T, w *pasta.Workspace, node pasta.NodeID, repeats ...pasta.MenuRepeatUpdate) {
	t.Helper()
	if _, err := w.UpdateNodeMenuState(node, pasta.MenuStateUpdate{Repeats: repeats}); err != nil {
		t.Fatalf("UpdateNodeMenuState(%s) error = %v", node, err)
	}
}

func assertJoinerInputPorts(t *testing.T, w *pasta.Workspace, node pasta.NodeID, want int) {
	t.Helper()
	snap, ok := w.Node(node)
	if !ok {
		t.Fatalf("Node(%s) missing", node)
	}
	if len(snap.Inputs) != want {
		t.Fatalf("joiner input count = %d, want %d: %#v", len(snap.Inputs), want, snap.Inputs)
	}
	for i := range want {
		if snap.Inputs[i].ID.Number != int64(i+1) {
			t.Fatalf("joiner input %d = %s, want port number %d", i, snap.Inputs[i].ID, i+1)
		}
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
