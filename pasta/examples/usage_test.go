package examples

import (
	"fmt"
	"testing"

	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/pasta/pasta/pastatest"
)

func TestCalculatorLibraryConforms(t *testing.T) {
	pastatest.RunSuite(t, pastatest.Suite{
		LibraryName: CalculatorLibraryName,
		NewLibrary: func(*testing.T) pasta.Library {
			return CalculatorLibrary{}
		},
		Classes: CalculatorClasses(),
		Links: []pastatest.LinkCase{
			{
				Name:   "constant to add",
				Output: pastatest.Endpoint{Class: ConstantClass, Port: Output},
				Input:  pastatest.Endpoint{Class: AddClass, Port: InputA},
				Type:   NumberType,
			},
			{
				Name:   "add to subtract",
				Output: pastatest.Endpoint{Class: AddClass, Port: Output},
				Input:  pastatest.Endpoint{Class: SubtractClass, Port: InputA},
				Type:   NumberType,
			},
			{
				Name:   "subtract to divide",
				Output: pastatest.Endpoint{Class: SubtractClass, Port: Output},
				Input:  pastatest.Endpoint{Class: DivideClass, Port: InputA},
				Type:   NumberType,
			},
			{
				Name:   "divide to result",
				Output: pastatest.Endpoint{Class: DivideClass, Port: Output},
				Input:  pastatest.Endpoint{Class: ResultClass, Port: InputA},
				Type:   NumberType,
			},
		},
	})
}

func TestCalculatorPushPullAndMixedFlow(t *testing.T) {
	w := newCalculatorWorkspace(t)

	// Build this graph:
	//
	//   (8 + 4 - 2) / 5 -> result
	//
	// Each CreateLink call mutates only through Workspace methods, so link
	// validation, DAG checks, lifecycle hooks, and locking all stay centralized.
	eight := mustCreate(t, w, ConstantClass, 8)
	four := mustCreate(t, w, ConstantClass, 4)
	two := mustCreate(t, w, ConstantClass, 2)
	five := mustCreate(t, w, ConstantClass, 5)
	add := mustCreate(t, w, AddClass, 0)
	subtract := mustCreate(t, w, SubtractClass, 0)
	divide := mustCreate(t, w, DivideClass, 0)
	result := mustCreate(t, w, ResultClass, 0)

	link(t, w, add, InputA, eight, Output)
	link(t, w, add, InputB, four, Output)
	link(t, w, subtract, InputA, add, Output)
	link(t, w, subtract, InputB, two, Output)
	link(t, w, divide, InputA, subtract, Output)
	denominatorLink := link(t, w, divide, InputB, five, Output)
	link(t, w, result, InputA, divide, Output)

	// Pull model: the result node asks upstream nodes for the current value when
	// its menu button is triggered.
	if err := w.TriggerNodeMenuButton(result, pasta.MenuButtonRef{Block: "main", Button: "pull"}); err != nil {
		t.Fatalf("pull result: %v", err)
	}
	if got := nodeValue(t, w, result); got != 2 {
		t.Fatalf("pulled result = %v, want 2", got)
	}

	// Push model: editing the constant menu updates its runtime, which pushes
	// through each attached wire and refreshes the result node.
	setConstant(t, w, eight, 18)
	if got := nodeValue(t, w, result); got != 4 {
		t.Fatalf("pushed result = %v, want 4", got)
	}

	// Mixed model: operator nodes receive push notifications, then use pull
	// reads from their other inputs while recomputing.
	setConstant(t, w, five, 4)
	if got := nodeValue(t, w, result); got != 5 {
		t.Fatalf("mixed result = %v, want 5", got)
	}

	// Removing a link immediately changes the graph. The result can still be
	// pulled; the missing denominator input defaults to zero and division fails,
	// so the previous result is preserved.
	if err := w.DeleteLink(denominatorLink); err != nil {
		t.Fatalf("delete link: %v", err)
	}
	if err := w.TriggerNodeMenuButton(result, pasta.MenuButtonRef{Block: "main", Button: "pull"}); err == nil {
		t.Fatal("pull after removing denominator link succeeded, want divide by zero error")
	}
}

func TestCalculatorSaveRestoreAndCopyPaste(t *testing.T) {
	w := newCalculatorWorkspace(t)
	left, right, sum, result := createSumGraph(t, w, 10, 6)

	if err := w.TriggerNodeMenuButton(result, pasta.MenuButtonRef{Block: "main", Button: "pull"}); err != nil {
		t.Fatalf("pull original result: %v", err)
	}
	if got := nodeValue(t, w, result); got != 16 {
		t.Fatalf("original result = %v, want 16", got)
	}

	// SaveWithRuntimeState asks active runtimes to export their private values.
	// Restore rebuilds active runtimes and reconnects application link objects.
	saved, err := w.SaveWithRuntimeState()
	if err != nil {
		t.Fatalf("save with runtime state: %v", err)
	}
	restored := newCalculatorWorkspace(t)
	if err := restored.Restore(saved); err != nil {
		t.Fatalf("restore: %v", err)
	}
	reconnectRuntimeLinks(t, restored)
	if err := restored.TriggerNodeMenuButton(result, pasta.MenuButtonRef{Block: "main", Button: "pull"}); err != nil {
		t.Fatalf("pull restored result: %v", err)
	}
	if got := nodeValue(t, restored, result); got != 16 {
		t.Fatalf("restored result = %v, want 16", got)
	}

	// Copy only serializes the selected nodes and links between them. Paste
	// allocates fresh node/link IDs and initializes fresh runtimes.
	nodes, links, err := w.Paste(mustCopy(t, w, []pasta.NodeID{left, right, sum}))
	if err != nil {
		t.Fatalf("paste copied sum graph: %v", err)
	}
	if len(nodes) != 3 || len(links) != 2 {
		t.Fatalf("paste returned nodes=%#v links=%#v, want 3 nodes and 2 links", nodes, links)
	}
	for _, node := range nodes {
		if node == left || node == right || node == sum {
			t.Fatalf("paste reused original node ID %s", node)
		}
	}
}

func Example_workspaceMutations() {
	w := pasta.NewWorkspace()
	defer func() { _ = w.Close() }()

	// Registering the library asks it to define its classes.
	_ = w.RegisterLibrary(CalculatorLibrary{})

	left, _ := w.CreateNode(ConstantClass, pasta.NodeOptions{State: stateWithValue(2), UseState: true})
	right, _ := w.CreateNode(ConstantClass, pasta.NodeOptions{State: stateWithValue(3), UseState: true})
	sum, _ := w.CreateNode(AddClass, pasta.NodeOptions{})
	result, _ := w.CreateNode(ResultClass, pasta.NodeOptions{})

	// Links are directed from an output port to an input port. The first
	// argument is the input endpoint because the input runtime may provide the
	// application-owned link object.
	linkID, _ := w.CreateLink(full(sum, InputA), full(left, Output), pasta.LinkOptions{Type: NumberType})
	_, _ = w.CreateLink(full(sum, InputB), full(right, Output), pasta.LinkOptions{Type: NumberType})
	_, _ = w.CreateLink(full(result, InputA), full(sum, Output), pasta.LinkOptions{Type: NumberType})

	// Menus are ephemeral control documents. This button asks the result node to
	// pull the current value through its upstream wires.
	_ = w.TriggerNodeMenuButton(result, pasta.MenuButtonRef{Block: "main", Button: "pull"})
	fmt.Println(nodeValueNoTest(w, result))

	// Controllers can remove links, nodes, and libraries with workspace methods.
	_ = w.DeleteLink(linkID)
	_ = w.DeleteNode(right)
	_ = w.UnregisterLibrary(CalculatorLibraryName)

	// Output:
	// 5
}

func Example_nodeMenus() {
	w := pasta.NewWorkspace()
	defer func() { _ = w.Close() }()
	_ = w.RegisterLibrary(CalculatorLibrary{})

	constant, _ := w.CreateNode(ConstantClass, pasta.NodeOptions{})
	menu, _ := w.NodeMenu(constant)

	// External UI code sends state updates back through the workspace. The
	// runtime hook accepts the new value, stores it in private state, and pushes
	// it to any connected downstream nodes.
	_, _ = w.UpdateNodeMenuState(constant, pasta.MenuStateUpdate{
		Version: menu.Version,
		Fields: []pasta.MenuFieldUpdate{{
			Block: "main",
			Field: "value",
			Value: 7.5,
		}},
	})

	fmt.Println(nodeValueNoTest(w, constant))

	// Output:
	// 7.5
}

func Example_saveRestore() {
	original := pasta.NewWorkspace()
	defer func() { _ = original.Close() }()
	_ = original.RegisterLibrary(CalculatorLibrary{})

	_, _, _, result := createSumGraphNoTest(original, 12, 5)
	_ = original.TriggerNodeMenuButton(result, pasta.MenuButtonRef{Block: "main", Button: "pull"})

	// SaveWithRuntimeState includes values exported by NodePrivateExportHook.
	// Use Save when all private state has already been committed to the model.
	saved, _ := original.SaveWithRuntimeState()

	restored := pasta.NewWorkspace()
	defer func() { _ = restored.Close() }()
	_ = restored.RegisterLibrary(CalculatorLibrary{})
	_ = restored.Restore(saved)

	// SaveData restores graph state. This example's numberWire link objects are
	// runtime-only values, so the app reconnects the restored links to rebuild
	// those objects before asking nodes to communicate through them again.
	_ = reconnectRuntimeLinksNoTest(restored)

	// Restore keeps node IDs from the save data, so the result ID is still valid
	// after the runtime links have been rehydrated.
	_ = restored.TriggerNodeMenuButton(result, pasta.MenuButtonRef{Block: "main", Button: "pull"})
	fmt.Println(nodeValueNoTest(restored, result))

	// Output:
	// 17
}

func Example_copyPaste() {
	w := pasta.NewWorkspace()
	defer func() { _ = w.Close() }()
	_ = w.RegisterLibrary(CalculatorLibrary{})

	left, right, sum, _ := createSumGraphNoTest(w, 20, 1)

	// Copy omits external links. Selecting these three nodes includes the two
	// constant-to-add links because both endpoints are inside the selection.
	clip, _ := w.Copy([]pasta.NodeID{left, right, sum})
	pastedNodes, pastedLinks, _ := w.Paste(clip)

	// Paste creates fresh IDs and leaves the original graph in place.
	pastedSum := findNodeByClassNoTest(w, pastedNodes, AddClass)
	value, _ := pullValueNoTest(w, pastedSum)
	fmt.Println(len(pastedNodes), len(pastedLinks), value)

	// Output:
	// 3 2 21
}

func newCalculatorWorkspace(t *testing.T) *pasta.Workspace {
	t.Helper()
	w := pasta.NewWorkspace()
	if err := w.RegisterLibrary(CalculatorLibrary{}); err != nil {
		t.Fatalf("register calculator library: %v", err)
	}
	return w
}

func createSumGraph(t *testing.T, w *pasta.Workspace, leftValue, rightValue float64) (pasta.NodeID, pasta.NodeID, pasta.NodeID, pasta.NodeID) {
	t.Helper()
	left, right, sum, result := createSumGraphNoTest(w, leftValue, rightValue)
	if left == 0 || right == 0 || sum == 0 || result == 0 {
		t.Fatal("create sum graph failed")
	}
	return left, right, sum, result
}

func createSumGraphNoTest(w *pasta.Workspace, leftValue, rightValue float64) (pasta.NodeID, pasta.NodeID, pasta.NodeID, pasta.NodeID) {
	left, _ := w.CreateNode(ConstantClass, pasta.NodeOptions{State: stateWithValue(leftValue), UseState: true})
	right, _ := w.CreateNode(ConstantClass, pasta.NodeOptions{State: stateWithValue(rightValue), UseState: true})
	sum, _ := w.CreateNode(AddClass, pasta.NodeOptions{})
	result, _ := w.CreateNode(ResultClass, pasta.NodeOptions{})
	_, _ = w.CreateLink(full(sum, InputA), full(left, Output), pasta.LinkOptions{Type: NumberType})
	_, _ = w.CreateLink(full(sum, InputB), full(right, Output), pasta.LinkOptions{Type: NumberType})
	_, _ = w.CreateLink(full(result, InputA), full(sum, Output), pasta.LinkOptions{Type: NumberType})
	return left, right, sum, result
}

func mustCreate(t *testing.T, w *pasta.Workspace, class string, value float64) pasta.NodeID {
	t.Helper()
	node, err := w.CreateNode(class, pasta.NodeOptions{State: stateWithValue(value), UseState: true})
	if err != nil {
		t.Fatalf("create %s: %v", class, err)
	}
	return node
}

func stateWithValue(value float64) pasta.NodeState {
	return pasta.NodeState{
		DisplayName: "Example",
		PrimaryType: NumberType,
		Private:     value,
		Metadata:    map[string]string{"createdBy": "examples"},
	}
}

func link(t *testing.T, w *pasta.Workspace, inputNode pasta.NodeID, inputPort pasta.PortID, outputNode pasta.NodeID, outputPort pasta.PortID) pasta.LinkID {
	t.Helper()
	id, err := w.CreateLink(full(inputNode, inputPort), full(outputNode, outputPort), pasta.LinkOptions{Type: NumberType})
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	return id
}

func mustCopy(t *testing.T, w *pasta.Workspace, nodes []pasta.NodeID) pasta.Clipboard {
	t.Helper()
	clip, err := w.Copy(nodes)
	if err != nil {
		t.Fatalf("copy nodes: %v", err)
	}
	return clip
}

func reconnectRuntimeLinks(t *testing.T, w *pasta.Workspace) {
	t.Helper()
	if err := reconnectRuntimeLinksNoTest(w); err != nil {
		t.Fatalf("reconnect runtime links: %v", err)
	}
}

func reconnectRuntimeLinksNoTest(w *pasta.Workspace) error {
	for _, link := range w.Snapshot().Links {
		if err := w.DeleteLink(link.ID); err != nil {
			return err
		}
		if _, err := w.CreateLink(link.Input, link.Output, pasta.LinkOptions{
			Type:      link.Type,
			Waypoints: link.Waypoints,
		}); err != nil {
			return err
		}
	}
	return nil
}

func full(node pasta.NodeID, port pasta.PortID) pasta.FullPortID {
	return pasta.FullPortID{Node: node, Port: port}
}

func setConstant(t *testing.T, w *pasta.Workspace, node pasta.NodeID, value float64) {
	t.Helper()
	menu, ok := w.NodeMenu(node)
	if !ok {
		t.Fatalf("node %s has no menu", node)
	}
	if _, err := w.UpdateNodeMenuState(node, pasta.MenuStateUpdate{
		Version: menu.Version,
		Fields: []pasta.MenuFieldUpdate{{
			Block: "main",
			Field: "value",
			Value: value,
		}},
	}); err != nil {
		t.Fatalf("set constant: %v", err)
	}
}

func nodeValue(t *testing.T, w *pasta.Workspace, node pasta.NodeID) float64 {
	t.Helper()
	return nodeValueNoTest(w, node)
}

func nodeValueNoTest(w *pasta.Workspace, node pasta.NodeID) float64 {
	snap, ok := w.Node(node)
	if !ok {
		return 0
	}
	return floatFromAny(snap.Dynamic.Private)
}

func findNodeByClassNoTest(w *pasta.Workspace, nodes []pasta.NodeID, class string) pasta.NodeID {
	for _, node := range nodes {
		snap, ok := w.Node(node)
		if ok && snap.Class == class {
			return node
		}
	}
	return 0
}

func pullValueNoTest(w *pasta.Workspace, node pasta.NodeID) (float64, error) {
	// Attach a temporary result node to the selected output and use the same
	// public menu button path an editor would use.
	result, err := w.CreateNode(ResultClass, pasta.NodeOptions{})
	if err != nil {
		return 0, err
	}
	if _, err := w.CreateLink(full(result, InputA), full(node, Output), pasta.LinkOptions{Type: NumberType}); err != nil {
		return 0, err
	}
	if err := w.TriggerNodeMenuButton(result, pasta.MenuButtonRef{Block: "main", Button: "pull"}); err != nil {
		return 0, err
	}
	return nodeValueNoTest(w, result), nil
}
