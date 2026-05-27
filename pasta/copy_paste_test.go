package pasta_test

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

type clipboardStateNode struct {
	workspaceNode
	value string
}

func (n *clipboardStateNode) OnSave(cfg configer.Config) error {
	if err := n.workspaceNode.OnSave(cfg); err != nil {
		return err
	}
	return cfg.Set(configer.Path{"value"}, n.value)
}

func TestWorkspaceCopyPasteRecreatesNodesPortsStateAndInternalLinks(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	var restoredValues []string
	if err := w.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{name: "example.com/Clipboard"},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			value, _ := cfg.Get(configer.Path{"value"})
			if s, ok := value.(string); ok {
				restoredValues = append(restoredValues, s)
			}
			return &workspaceNode{}, nil
		},
	}); err != nil {
		t.Fatalf("AddNodeClass: %v", err)
	}

	source, err := w.AddNode(&clipboardStateNode{value: "source-state"}, "example.com/Clipboard", "source")
	if err != nil {
		t.Fatalf("AddNode source: %v", err)
	}
	target, err := w.AddNode(&clipboardStateNode{value: "target-state"}, "example.com/Clipboard", "target")
	if err != nil {
		t.Fatalf("AddNode target: %v", err)
	}
	out, err := w.AddPort(pasta.Port{Node: source, Direction: "right", Name: "out", Types: []string{"example.com/typeA"}})
	if err != nil {
		t.Fatalf("AddPort out: %v", err)
	}
	in, err := w.AddPort(pasta.Port{Node: target, Direction: "left", Name: "in", Types: []string{"example.com/typeA"}})
	if err != nil {
		t.Fatalf("AddPort in: %v", err)
	}
	if _, _, err := w.AddLink(out, in); err != nil {
		t.Fatalf("AddLink internal: %v", err)
	}
	external, err := w.AddNode(&workspaceNode{}, "example.com/Clipboard", "external")
	if err != nil {
		t.Fatalf("AddNode external: %v", err)
	}
	externalIn, err := w.AddPort(pasta.Port{Node: external, Direction: "left", Name: "in", Types: []string{"example.com/typeA"}})
	if err != nil {
		t.Fatalf("AddPort external: %v", err)
	}
	if _, _, err := w.AddLink(out, externalIn); err != nil {
		t.Fatalf("AddLink external: %v", err)
	}
	if err := w.SetNodeLabel(source, "Source"); err != nil {
		t.Fatalf("SetNodeLabel: %v", err)
	}
	if err := w.SetNodePosition(source, "10 20"); err != nil {
		t.Fatalf("SetNodePosition: %v", err)
	}

	clip := w.Copy([]uint64{source, 999999, target})
	pasted := w.Paste(clip)
	if len(pasted) != 2 {
		t.Fatalf("Paste returned %v, want two new nodes", pasted)
	}
	if slices.Contains(pasted, source) || slices.Contains(pasted, target) {
		t.Fatalf("Paste reused source IDs: pasted=%v source=%d target=%d", pasted, source, target)
	}
	if !reflect.DeepEqual(restoredValues, []string{"source-state", "target-state"}) {
		t.Fatalf("factory restored values = %#v, want source/target state", restoredValues)
	}

	snapshot := w.Snapshot()
	pastedSource := snapshot.Nodes[pasted[0]]
	pastedTarget := snapshot.Nodes[pasted[1]]
	if pastedSource.Name == "source" || pastedTarget.Name == "target" {
		t.Fatalf("pasted duplicate names were not regenerated: %#v %#v", pastedSource, pastedTarget)
	}
	if pastedSource.Label != "Source" || pastedSource.Position != "10 20" {
		t.Fatalf("pasted source state = %#v, want label and position", pastedSource)
	}
	if got := portNames(snapshot, pastedSource.RightPorts); !reflect.DeepEqual(got, []string{"out"}) {
		t.Fatalf("pasted source right ports = %#v, want [out]", got)
	}
	if got := portNames(snapshot, pastedTarget.LeftPorts); !reflect.DeepEqual(got, []string{"in"}) {
		t.Fatalf("pasted target left ports = %#v, want [in]", got)
	}
	if len(snapshot.Ports[pastedSource.RightPorts[0]].Links) != 1 || len(snapshot.Ports[pastedTarget.LeftPorts[0]].Links) != 1 {
		t.Fatalf("pasted ports links = %#v %#v, want only internal link", snapshot.Ports[pastedSource.RightPorts[0]], snapshot.Ports[pastedTarget.LeftPorts[0]])
	}
	if _, _, ok := w.LinkByPorts(pastedSource.RightPorts[0], pastedTarget.LeftPorts[0]); !ok {
		t.Fatal("pasted internal link missing")
	}
}

func TestWorkspacePasteUsesPlaceholdersSkipsUniqueDuplicatesAndIgnoresFailures(t *testing.T) {
	source := pasta.NewWorkspace(&StringLoggerFactory{})
	uniqueClass := testNodeClass{name: "example.com/UniqueClipboard", params: pasta.NodeClassParams{Unique: true}}
	if err := source.AddNodeClass(uniqueClass); err != nil {
		t.Fatalf("source AddNodeClass unique: %v", err)
	}
	if _, err := source.AddNode(&workspaceNode{}, "example.com/UniqueClipboard", "unique"); err != nil {
		t.Fatalf("source AddNode unique: %v", err)
	}
	missing, err := source.AddPlaceholderNode("example.com/MissingClipboard", []pasta.Port{{
		Direction: "right",
		Name:      "out",
		Types:     []string{"example.com/typeA"},
	}}, "missing")
	if err != nil {
		t.Fatalf("source AddPlaceholderNode: %v", err)
	}
	uniqueID, _ := source.NodeIDByName("unique")
	clip := source.Copy([]uint64{uniqueID, missing})

	target := pasta.NewWorkspace(&StringLoggerFactory{})
	if err := target.AddNodeClass(uniqueClass); err != nil {
		t.Fatalf("target AddNodeClass unique: %v", err)
	}
	if _, err := target.AddNode(&workspaceNode{}, "example.com/UniqueClipboard", "existing unique"); err != nil {
		t.Fatalf("target AddNode unique: %v", err)
	}
	if err := target.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{name: "example.com/PanicClipboard"},
		newNode: func(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
			panic("factory panic")
		},
	}); err != nil {
		t.Fatalf("target AddNodeClass panic: %v", err)
	}

	pasted := target.Paste(clip)
	if len(pasted) != 1 {
		t.Fatalf("Paste returned %v, want only missing-class placeholder", pasted)
	}
	snapshot := target.Snapshot()
	node := snapshot.Nodes[pasted[0]]
	if !node.Placeholder || node.Class != "example.com/MissingClipboard" {
		t.Fatalf("pasted node = %#v, want missing class placeholder", node)
	}
	if nodes, err := target.NodesByClass("example.com/UniqueClipboard"); err != nil || len(nodes) != 1 {
		t.Fatalf("unique nodes = %v, %v; want existing only", nodes, err)
	}

	bad := target.Paste(`{"version":1,"nodes":[{"id":1,"class":"example.com/PanicClipboard","name":"panic"}]}`)
	if len(bad) != 0 {
		t.Fatalf("panic factory paste returned %v, want ignored", bad)
	}
	invalid := target.Paste("not clipboard data")
	if len(invalid) != 0 {
		t.Fatalf("invalid paste returned %v, want ignored", invalid)
	}
}

func TestWorkspacePasteHonorsFactoryMutatedState(t *testing.T) {
	source := pasta.NewWorkspace(&StringLoggerFactory{})
	id, err := source.AddNode(&workspaceNode{}, "example.com/MutatingClipboard", "original")
	if err != nil {
		t.Fatalf("source AddNode: %v", err)
	}
	if _, err := source.AddPort(pasta.Port{
		Node:      id,
		Direction: "right",
		Name:      "copied",
		Types:     []string{"example.com/typeA"},
	}); err != nil {
		t.Fatalf("source AddPort: %v", err)
	}
	clip := source.Copy([]uint64{id})

	target := pasta.NewWorkspace(&StringLoggerFactory{})
	if err := target.AddNodeClass(testFactoryNodeClass{
		testNodeClass: testNodeClass{name: "example.com/MutatingClipboard"},
		newNode: func(_ configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			previous[0].Name = "factory pasted"
			previous[0].Label = "factory label"
			previous[0].RightPorts = append(previous[0].RightPorts, pasta.Port{
				Direction: "right",
				Name:      "factory",
				Types:     []string{"example.com/typeB"},
			})
			return &workspaceNode{}, nil
		},
	}); err != nil {
		t.Fatalf("target AddNodeClass: %v", err)
	}

	pasted := target.Paste(clip)
	if len(pasted) != 1 {
		t.Fatalf("Paste returned %v, want one node", pasted)
	}
	node := target.Snapshot().Nodes[pasted[0]]
	if node.Name != "factory pasted" || node.Label != "factory label" {
		t.Fatalf("pasted node = %#v, want factory-mutated name and label", node)
	}
	if got := portNames(target.Snapshot(), node.RightPorts); !reflect.DeepEqual(got, []string{"copied", "factory"}) {
		t.Fatalf("right ports = %#v, want copied and factory", got)
	}
}

func TestWorkspacePasteUndoRedoIsSingleBestEffortOperation(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	first, err := w.AddNode(&workspaceNode{}, "example.com/UndoPaste", "first")
	if err != nil {
		t.Fatalf("AddNode first: %v", err)
	}
	second, err := w.AddNode(&workspaceNode{}, "example.com/UndoPaste", "second")
	if err != nil {
		t.Fatalf("AddNode second: %v", err)
	}
	out, err := w.AddPort(pasta.Port{Node: first, Direction: "right", Name: "out", Types: []string{"example.com/typeA"}})
	if err != nil {
		t.Fatalf("AddPort out: %v", err)
	}
	in, err := w.AddPort(pasta.Port{Node: second, Direction: "left", Name: "in", Types: []string{"example.com/typeA"}})
	if err != nil {
		t.Fatalf("AddPort in: %v", err)
	}
	if _, _, err := w.AddLink(out, in); err != nil {
		t.Fatalf("AddLink: %v", err)
	}

	clip := w.Copy([]uint64{first, second})
	pasted := w.Paste(clip)
	if len(pasted) != 2 {
		t.Fatalf("Paste returned %v, want two nodes", pasted)
	}
	if len(w.Snapshot().Nodes) != 4 {
		t.Fatalf("nodes after paste = %d, want 4", len(w.Snapshot().Nodes))
	}

	w.Undo()
	if _, ok := w.NodeSnapshot(pasted[0]); ok {
		t.Fatalf("first pasted node %d survived single undo", pasted[0])
	}
	if _, ok := w.NodeSnapshot(pasted[1]); ok {
		t.Fatalf("second pasted node %d survived single undo", pasted[1])
	}
	if len(w.Snapshot().Nodes) != 2 {
		t.Fatalf("nodes after one undo = %d, want original two", len(w.Snapshot().Nodes))
	}

	w.Redo()
	snapshot := w.Snapshot()
	if _, ok := snapshot.Nodes[pasted[0]]; !ok {
		t.Fatalf("first pasted node %d not restored by redo", pasted[0])
	}
	if _, ok := snapshot.Nodes[pasted[1]]; !ok {
		t.Fatalf("second pasted node %d not restored by redo", pasted[1])
	}
	restoredA := snapshot.Nodes[pasted[0]]
	restoredB := snapshot.Nodes[pasted[1]]
	if _, _, ok := w.LinkByPorts(restoredA.RightPorts[0], restoredB.LeftPorts[0]); !ok {
		t.Fatal("redo did not restore pasted link")
	}
}

func TestWorkspaceCopyReturnsEmptyForMissingNodesAndOnSaveFailures(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	id, err := w.AddNode(&workspaceNode{failOn: map[string]error{"OnSave": errors.New("save failed")}}, "example.com/Node", "node")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if got := w.Copy([]uint64{999999}); got != "" {
		t.Fatalf("Copy missing node = %q, want empty", got)
	}
	if got := w.Copy([]uint64{id}); got != "" {
		t.Fatalf("Copy failed OnSave = %q, want empty", got)
	}
}
