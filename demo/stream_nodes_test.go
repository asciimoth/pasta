package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/asciimoth/pasta/pasta"
)

func TestStreamLibraryPullFlow(t *testing.T) {
	w := pasta.NewWorkspace()
	defer func() { _ = w.Close() }()
	if err := w.RegisterLibrary(StreamLibrary{}); err != nil {
		t.Fatalf("RegisterLibrary() error = %v", err)
	}
	sink := createStreamNode(t, w, StreamSinkClass, streamState{Value: "waiting"})
	upper := createStreamNode(t, w, StreamUppercaseClass, streamState{Value: "waiting"})
	prefix := createStreamNode(t, w, StreamPrefixClass, streamState{Prefix: "processed: ", Value: "waiting"})
	provider := createStreamNode(t, w, StreamProviderClass, streamState{IntervalSeconds: 0.05, Value: "waiting"})

	linkStream(t, w, upper, sink)
	linkStream(t, w, prefix, upper)
	linkStream(t, w, provider, prefix)

	waitForStream(t, w, sink, func(state streamState) bool {
		return state.Count >= 2 && strings.HasPrefix(state.Value, "PROCESSED: CHUNK-")
	})
	if text, ok := streamTextValue(streamNodeState(t, w, sink)); !ok || !strings.HasPrefix(text, "PROCESSED: CHUNK-") {
		t.Fatalf("streamTextValue() = %q, %v; want processed stream text", text, ok)
	}
}

func TestStreamDetachStopsInputReader(t *testing.T) {
	w := pasta.NewWorkspace()
	defer func() { _ = w.Close() }()
	if err := w.RegisterLibrary(StreamLibrary{}); err != nil {
		t.Fatalf("RegisterLibrary() error = %v", err)
	}
	provider := createStreamNode(t, w, StreamProviderClass, streamState{IntervalSeconds: 0.05, Value: "waiting"})
	sink := createStreamNode(t, w, StreamSinkClass, streamState{Value: "waiting"})
	linkID, err := w.CreateLink(
		pasta.FullPortID{Node: provider, Port: StreamInput},
		pasta.FullPortID{Node: sink, Port: StreamOutput},
		pasta.LinkOptions{Type: StreamType},
	)
	if err != nil {
		t.Fatalf("CreateLink() error = %v", err)
	}

	waitForStream(t, w, sink, func(state streamState) bool {
		return state.Count >= 1
	})
	before := streamNodeState(t, w, sink)
	if err := w.DeleteLink(linkID); err != nil {
		t.Fatalf("DeleteLink() error = %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	after := streamNodeState(t, w, sink)
	if after.Count != before.Count || after.Value != before.Value {
		t.Fatalf("sink changed after detach: before=%#v after=%#v", before, after)
	}
}

func TestStreamPullWaitsForLateSource(t *testing.T) {
	w := pasta.NewWorkspace()
	defer func() { _ = w.Close() }()
	if err := w.RegisterLibrary(StreamLibrary{}); err != nil {
		t.Fatalf("RegisterLibrary() error = %v", err)
	}
	sink := createStreamNode(t, w, StreamSinkClass, streamState{Value: "waiting"})
	upper := createStreamNode(t, w, StreamUppercaseClass, streamState{Value: "waiting"})
	provider := createStreamNode(t, w, StreamProviderClass, streamState{IntervalSeconds: 0.05, Value: "waiting"})

	linkStream(t, w, upper, sink)
	time.Sleep(25 * time.Millisecond)
	linkStream(t, w, provider, upper)

	waitForStream(t, w, sink, func(state streamState) bool {
		return state.Count >= 1 && strings.HasPrefix(state.Value, "CHUNK-")
	})
}

func TestStreamWireReadUnblocksOnContextCancel(t *testing.T) {
	wire := newStreamWire(func(ctx context.Context) (string, bool) {
		<-ctx.Done()
		return "", false
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		_, ok := wire.Read(ctx)
		done <- ok
	}()
	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("read returned ok after context cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("read did not unblock after context cancel")
	}
}

func createStreamNode(t *testing.T, w *pasta.Workspace, class string, private streamState) pasta.NodeID {
	t.Helper()
	id, err := w.CreateNode(class, pasta.NodeOptions{UseState: true, State: pasta.NodeState{
		DisplayName: class,
		PrimaryType: StreamType,
		Private:     private,
	}})
	if err != nil {
		t.Fatalf("CreateNode(%s) error = %v", class, err)
	}
	return id
}

func linkStream(t *testing.T, w *pasta.Workspace, inputNode, outputNode pasta.NodeID) {
	t.Helper()
	if _, err := w.CreateLink(
		pasta.FullPortID{Node: inputNode, Port: StreamInput},
		pasta.FullPortID{Node: outputNode, Port: StreamOutput},
		pasta.LinkOptions{Type: StreamType},
	); err != nil {
		t.Fatalf("CreateLink(%s <- %s) error = %v", inputNode, outputNode, err)
	}
}

func waitForStream(t *testing.T, w *pasta.Workspace, node pasta.NodeID, ok func(streamState) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state := streamNodeState(t, w, node)
		if ok(state) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("stream node %s did not reach expected state; last=%#v", node, streamNodeState(t, w, node))
}

func streamNodeState(t *testing.T, w *pasta.Workspace, node pasta.NodeID) streamState {
	t.Helper()
	snap, ok := w.Node(node)
	if !ok {
		t.Fatalf("Node(%s) missing", node)
	}
	return streamStateFromAny(snap.Dynamic.Private)
}
