package pasta

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type testResource struct {
	name string
}

func TestResourceTrackingDeleteLinkAndUntrack(t *testing.T) {
	w, _ := testWorkspace(t)
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	link, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	res := &testResource{name: "link"}
	calls := 0
	if err := w.TrackResource(res, ResourceRelations{Nodes: []NodeID{a, b}, Links: []LinkID{link}}, func(resource any) error {
		if resource != res {
			t.Fatalf("resource = %#v, want %#v", resource, res)
		}
		calls++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.DeleteLink(link); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("destructor calls = %d, want 1", calls)
	}

	res = &testResource{name: "untrack"}
	if err := w.TrackResource(res, ResourceRelations{Nodes: []NodeID{a}}, func(any) error {
		calls++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.UntrackResource(res); err != nil {
		t.Fatal(err)
	}
	if err := w.DeleteNode(a); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("destructor after untrack calls = %d, want 1", calls)
	}
}

func TestResourceTrackingReplacementInactiveAndInvalidIdentity(t *testing.T) {
	w, _ := testWorkspace(t)
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	res := &testResource{name: "replace"}
	calls := []string{}
	if err := w.TrackResource(res, ResourceRelations{Nodes: []NodeID{a}}, func(any) error {
		calls = append(calls, "old")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.TrackResource(res, ResourceRelations{Nodes: []NodeID{b}}, func(any) error {
		calls = append(calls, "new")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("replacement called destructor: %#v", calls)
	}
	if err := w.DeleteNode(a); err != nil {
		t.Fatal(err)
	}
	if got := calls; len(got) != 1 || got[0] != "new" {
		t.Fatalf("calls = %#v, want new only", got)
	}

	immediate := 0
	if err := w.TrackResource(&testResource{name: "inactive"}, ResourceRelations{Nodes: []NodeID{a}}, func(any) error {
		immediate++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if immediate != 1 {
		t.Fatalf("inactive immediate destructor calls = %d, want 1", immediate)
	}
	if err := w.TrackResource([]int{1}, ResourceRelations{Nodes: []NodeID{a}}, func(any) error { return nil }); !errors.Is(err, ErrInvalidResource) {
		t.Fatalf("non-comparable resource error = %v, want ErrInvalidResource", err)
	}
}

func TestResourceDestructorCanReenterWorkspace(t *testing.T) {
	w, _ := testWorkspace(t)
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	res := &testResource{name: "reenter"}
	done := make(chan struct{})
	if err := w.TrackResource(res, ResourceRelations{Nodes: []NodeID{a}}, func(resource any) error {
		defer close(done)
		if err := w.UntrackResource(resource); err != nil {
			return err
		}
		_, _ = w.Node(a)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.DeleteNode(a); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("resource destructor deadlocked")
	}
}

func TestNodeScopeTrackResourceFromBackground(t *testing.T) {
	var scope NodeScope
	w := NewWorkspace()
	if err := w.RegisterLibrary(StaticLibrary{LibraryName: "example.com", Classes: []ClassSpec{{
		Name: "example.com/Scoped",
		Runtime: nodeClassFunc(func(ctx NodeContext, _ NodeState, _ InitMode) (NodeRuntime, error) {
			scope = ctx.Node
			return struct{}{}, nil
		}),
	}}}); err != nil {
		t.Fatal(err)
	}
	id, err := w.CreateNode("example.com/Scoped", NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	res := &testResource{name: "background"}
	var wg sync.WaitGroup
	wg.Add(1)
	calls := 0
	go func() {
		defer wg.Done()
		if err := scope.TrackResource(res, nil, func(any) error {
			calls++
			return nil
		}); err != nil {
			t.Errorf("TrackResource() error = %v", err)
		}
	}()
	wg.Wait()
	if err := w.DeleteNode(id); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("destructor calls = %d, want 1", calls)
	}
}

func TestResourceTrackingClassRedefinitionPrunedLink(t *testing.T) {
	w, class := testWorkspace(t)
	a, _ := w.CreateNode("example.com/Source", NodeOptions{})
	b, _ := w.CreateNode("example.com/Source", NodeOptions{})
	link, err := w.CreateLink(
		FullPortID{Node: b, Port: PortID{Number: 1, Kind: InputPort}},
		FullPortID{Node: a, Port: PortID{Number: 1, Kind: OutputPort}},
		LinkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := w.TrackResource(&testResource{name: "pruned"}, ResourceRelations{Links: []LinkID{link}}, func(any) error {
		calls++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	class.Outputs = nil
	if err := w.DefineClass("example.com", class); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("destructor calls = %d, want 1", calls)
	}
	if _, ok := w.Link(link); ok {
		t.Fatal("link still exists after class redefinition removed its output port")
	}
}
