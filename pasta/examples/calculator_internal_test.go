package examples

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/asciimoth/pasta/pasta"
)

type failingScope struct {
	err error
}

func (s failingScope) DefineClass(pasta.ClassSpec) error { return s.err }
func (s failingScope) RecallClass(string) error          { return nil }
func (s failingScope) Classes() []pasta.ClassSnapshot    { return nil }
func (s failingScope) CanCreateNode(string) error        { return nil }
func (s failingScope) CreateNode(string, pasta.NodeOptions) (pasta.NodeID, error) {
	return 0, nil
}
func (s failingScope) CanDeleteNode(pasta.NodeID) error { return nil }
func (s failingScope) DeleteNode(pasta.NodeID) error    { return nil }
func (s failingScope) SetNodeState(pasta.NodeID, pasta.NodeState) error {
	return nil
}
func (s failingScope) SetNodePrivate(pasta.NodeID, any) error { return nil }
func (s failingScope) SetNodeCoordinate(pasta.NodeID, string) error {
	return nil
}
func (s failingScope) SetNodeMetadata(pasta.NodeID, map[string]string) error {
	return nil
}
func (s failingScope) SetNodeMetadataValue(pasta.NodeID, string, string) error {
	return nil
}
func (s failingScope) DeleteNodeMetadataValue(pasta.NodeID, string) error { return nil }
func (s failingScope) AddNodeMessage(pasta.NodeID, pasta.MessageType, string) (pasta.MessageID, error) {
	return 0, nil
}
func (s failingScope) RemoveNodeMessage(pasta.NodeID, pasta.MessageID) error { return nil }
func (s failingScope) SetNodeMenu(pasta.NodeID, pasta.NodeMenu) error        { return nil }
func (s failingScope) ClearNodeMenu(pasta.NodeID) error                      { return nil }
func (s failingScope) UpdateNodeMenuState(pasta.NodeID, pasta.MenuStateUpdate) (pasta.NodeMenu, error) {
	return pasta.NodeMenu{}, nil
}
func (s failingScope) TriggerNodeMenuButton(pasta.NodeID, pasta.MenuButtonRef) error {
	return nil
}
func (s failingScope) CanSetNodePorts(pasta.NodeID, []pasta.PortSpec, []pasta.PortSpec) error {
	return nil
}
func (s failingScope) SetNodePorts(pasta.NodeID, []pasta.PortSpec, []pasta.PortSpec) error {
	return nil
}
func (s failingScope) CanCreateLink(pasta.FullPortID, pasta.FullPortID, string) error {
	return nil
}
func (s failingScope) CreateLink(pasta.FullPortID, pasta.FullPortID, pasta.LinkOptions) (pasta.LinkID, error) {
	return 0, nil
}
func (s failingScope) CanSetLinkWaypoints(pasta.LinkID) error        { return nil }
func (s failingScope) SetLinkWaypoints(pasta.LinkID, []string) error { return nil }
func (s failingScope) CanDeleteLink(pasta.LinkID) error              { return nil }
func (s failingScope) DeleteLink(pasta.LinkID) error                 { return nil }
func (s failingScope) ReadOnly() pasta.WorkspaceRO                   { return nil }

func TestCalculatorLibraryDefineClassesReturnsScopeError(t *testing.T) {
	want := errors.New("define failed")
	if err := (CalculatorLibrary{}).DefineClasses(failingScope{err: want}); !errors.Is(err, want) {
		t.Fatalf("DefineClasses error = %v, want %v", err, want)
	}
}

func TestCalculatorRuntimeErrorBranches(t *testing.T) {
	node := &calculatorNode{kind: "add", inputs: map[pasta.PortID]*numberWire{}}
	if got, err := node.Value(); err != nil || got != 0 {
		t.Fatalf("add with missing inputs = %v, %v; want 0 nil", got, err)
	}
	node.inputs[InputA] = &numberWire{source: errSource{err: errors.New("a failed")}}
	if _, err := node.Value(); err == nil {
		t.Fatal("add with failing input A succeeded")
	}
	node.inputs[InputA] = &numberWire{source: valueSource(2)}
	node.inputs[InputB] = &numberWire{source: errSource{err: errors.New("b failed")}}
	if _, err := node.Value(); err == nil {
		t.Fatal("add with failing input B succeeded")
	}

	node.kind = "subtract"
	node.inputs[InputA] = &numberWire{source: errSource{err: errors.New("a failed")}}
	if _, err := node.Value(); err == nil {
		t.Fatal("subtract with failing input A succeeded")
	}
	node.inputs[InputA] = &numberWire{source: valueSource(4)}
	node.inputs[InputB] = &numberWire{source: errSource{err: errors.New("b failed")}}
	if _, err := node.Value(); err == nil {
		t.Fatal("subtract with failing input B succeeded")
	}

	node.kind = "divide"
	node.inputs[InputA] = &numberWire{source: errSource{err: errors.New("a failed")}}
	if _, err := node.Value(); err == nil {
		t.Fatal("divide with failing input A succeeded")
	}
	node.inputs[InputA] = &numberWire{source: valueSource(4)}
	node.inputs[InputB] = &numberWire{source: errSource{err: errors.New("b failed")}}
	if _, err := node.Value(); err == nil {
		t.Fatal("divide with failing input B succeeded")
	}
	node.inputs[InputB] = &numberWire{source: valueSource(0)}
	if _, err := node.Value(); err == nil {
		t.Fatal("divide by zero succeeded")
	}

	node.kind = "unknown"
	if _, err := node.Value(); err == nil {
		t.Fatal("unknown calculator kind succeeded")
	}
}

func TestCalculatorHooksAndConversions(t *testing.T) {
	node := &calculatorNode{kind: "result", inputs: map[pasta.PortID]*numberWire{}, sinks: map[pasta.LinkID]*numberWire{}}
	if object, err := node.LinkObject(pasta.LinkEndpoint{Direction: pasta.OutputPort}); err != nil || object != nil {
		t.Fatalf("output LinkObject = %#v, %v; want nil nil", object, err)
	}
	object, err := node.LinkObject(pasta.LinkEndpoint{Direction: pasta.InputPort})
	if err != nil {
		t.Fatal(err)
	}
	wire := object.(*numberWire)
	if wire.sink != node {
		t.Fatalf("input LinkObject sink = %#v, want node", wire.sink)
	}
	if err := node.BeforeLinkAttach(pasta.LinkEndpoint{}, "bad"); err == nil {
		t.Fatal("BeforeLinkAttach accepted wrong object type")
	}

	for _, input := range []any{math.NaN(), math.Inf(1), float32(1.5), int(2), int64(3), int32(4), uint(5), uint64(6), uint32(7), "bad"} {
		_ = floatFromAny(input)
	}
}

func TestCalculatorRemainingRuntimeBranches(t *testing.T) {
	want := errors.New("set menu failed")
	_, err := (calculatorNodeClass{kind: "constant"}).InitNode(pasta.NodeContext{Node: errNodeScope{err: want}}, pasta.NodeState{}, pasta.InitNew)
	if !errors.Is(err, want) {
		t.Fatalf("InitNode SetMenu error = %v, want %v", err, want)
	}

	update := pasta.MenuStateUpdate{Fields: []pasta.MenuFieldUpdate{{Block: "main", Field: "value", Value: 9.0}}}
	node := &calculatorNode{kind: "add", inputs: map[pasta.PortID]*numberWire{}, sinks: map[pasta.LinkID]*numberWire{}}
	got, err := node.ApplyMenuUpdate(update)
	if err != nil || !reflect.DeepEqual(got, update) {
		t.Fatalf("non-constant ApplyMenuUpdate = %#v, %v; want original nil", got, err)
	}
	if err := node.TriggerMenuButton(pasta.MenuButtonRef{Block: "main", Button: "pull"}); err != nil {
		t.Fatalf("non-result TriggerMenuButton error = %v", err)
	}
	result := &calculatorNode{kind: "result", inputs: map[pasta.PortID]*numberWire{}, sinks: map[pasta.LinkID]*numberWire{}}
	if err := result.TriggerMenuButton(pasta.MenuButtonRef{Block: "main", Button: "other"}); err != nil {
		t.Fatalf("wrong button TriggerMenuButton error = %v", err)
	}
	if ports := result.outputPorts(); ports != nil {
		t.Fatalf("result outputPorts = %#v, want nil", ports)
	}
}

type valueSource float64

func (s valueSource) Value() (float64, error) { return float64(s), nil }

type errSource struct {
	err error
}

func (s errSource) Value() (float64, error) { return 0, s.err }

type errNodeScope struct {
	err error
}

func (s errNodeScope) ID() pasta.NodeID { return 0 }
func (s errNodeScope) ReadOnly() pasta.WorkspaceRO {
	return nil
}
func (s errNodeScope) Snapshot() (pasta.NodeSnapshot, bool) { return pasta.NodeSnapshot{}, false }
func (s errNodeScope) NotifyChanged() error                 { return nil }
func (s errNodeScope) AddMessage(pasta.MessageType, string) (pasta.MessageID, error) {
	return 0, nil
}
func (s errNodeScope) RemoveMessage(pasta.MessageID) error { return nil }
func (s errNodeScope) SetMenu(pasta.NodeMenu) error        { return s.err }
func (s errNodeScope) ClearMenu() error                    { return nil }
func (s errNodeScope) UpdateMenuState(pasta.MenuStateUpdate) (pasta.NodeMenu, error) {
	return pasta.NodeMenu{}, nil
}
func (s errNodeScope) SetState(pasta.NodeState) error                    { return nil }
func (s errNodeScope) SetPrivate(any) error                              { return nil }
func (s errNodeScope) SetCoordinate(string) error                        { return nil }
func (s errNodeScope) SetMetadata(map[string]string) error               { return nil }
func (s errNodeScope) SetMetadataValue(string, string) error             { return nil }
func (s errNodeScope) DeleteMetadataValue(string) error                  { return nil }
func (s errNodeScope) SetPorts([]pasta.PortSpec, []pasta.PortSpec) error { return nil }
func (s errNodeScope) TrackResource(any, []pasta.LinkID, pasta.ResourceDestructor) error {
	return nil
}
func (s errNodeScope) UntrackResource(any) error { return nil }
