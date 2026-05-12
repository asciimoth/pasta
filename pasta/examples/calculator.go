// Package examples contains small, complete Pasta libraries that applications
// can copy from when wiring their own node catalogs and runtimes.
package examples

import (
	"fmt"
	"math"
	"sync"

	"github.com/asciimoth/pasta/pasta"
)

const (
	// CalculatorLibraryName is the namespace used by every class and type in
	// this example. Real applications usually use their own domain name here.
	CalculatorLibraryName = "calc.pasta.example.com"

	ConstantClass = CalculatorLibraryName + "/Constant"
	AddClass      = CalculatorLibraryName + "/Add"
	SubtractClass = CalculatorLibraryName + "/Subtract"
	DivideClass   = CalculatorLibraryName + "/Divide"
	ResultClass   = CalculatorLibraryName + "/Result"

	NumberType = CalculatorLibraryName + "/number"
)

var (
	InputA = pasta.PortID{Number: 1, Kind: pasta.InputPort}
	InputB = pasta.PortID{Number: 2, Kind: pasta.InputPort}
	Output = pasta.PortID{Number: 1, Kind: pasta.OutputPort}
)

type numberSource interface {
	Value() (float64, error)
}

type numberSink interface {
	Push(float64)
}

// numberWire is the application-owned link object. Pasta stores it and passes
// it to both endpoints, but the framework never interprets it.
//
// The output endpoint fills source, which enables pull evaluation. The input
// endpoint can fill sink, which enables push propagation. A single wire can
// therefore demonstrate pull-only, push-only, and mixed data-flow models.
type numberWire struct {
	source numberSource
	sink   numberSink
}

// CalculatorLibrary is a custom library implementation. Its DefineClasses
// method is where applications publish class definitions to a workspace.
type CalculatorLibrary struct{}

func (CalculatorLibrary) Name() string { return CalculatorLibraryName }

func (CalculatorLibrary) DefineClasses(scope pasta.LibraryScope) error {
	for _, class := range CalculatorClasses() {
		if err := scope.DefineClass(class); err != nil {
			return err
		}
	}
	return nil
}

// CalculatorClasses returns the class specs separately from CalculatorLibrary
// so tests and documentation can use the same source of truth.
func CalculatorClasses() []pasta.ClassSpec {
	return []pasta.ClassSpec{
		{
			Name:        ConstantClass,
			DisplayName: "Constant",
			Description: "Stores one number and publishes it on an output port.",
			Default: pasta.NodeState{
				DisplayName: "Constant",
				PrimaryType: NumberType,
				Private:     float64(0),
				Metadata:    map[string]string{"palette": "math"},
			},
			Outputs: []pasta.PortSpec{numberOutput(Output, "value")},
			Runtime: calculatorNodeClass{kind: "constant"},
		},
		operatorClass(AddClass, "Add", "Adds two input numbers.", "add"),
		operatorClass(SubtractClass, "Subtract", "Subtracts input b from input a.", "subtract"),
		operatorClass(DivideClass, "Divide", "Divides input a by input b.", "divide"),
		{
			Name:        ResultClass,
			DisplayName: "Result",
			Description: "Receives a number and exposes the last result.",
			Default: pasta.NodeState{
				DisplayName: "Result",
				PrimaryType: NumberType,
				Private:     float64(0),
				Metadata:    map[string]string{"palette": "math"},
			},
			Inputs:  []pasta.PortSpec{numberInput(InputA, "value")},
			Runtime: calculatorNodeClass{kind: "result"},
		},
	}
}

func operatorClass(name, display, description, kind string) pasta.ClassSpec {
	return pasta.ClassSpec{
		Name:        name,
		DisplayName: display,
		Description: description,
		Default: pasta.NodeState{
			DisplayName: display,
			PrimaryType: NumberType,
			Private:     float64(0),
			Metadata:    map[string]string{"palette": "math"},
		},
		Inputs: []pasta.PortSpec{
			numberInput(InputA, "a"),
			numberInput(InputB, "b"),
		},
		Outputs: []pasta.PortSpec{numberOutput(Output, "value")},
		Runtime: calculatorNodeClass{kind: kind},
	}
}

func numberInput(id pasta.PortID, name string) pasta.PortSpec {
	return pasta.PortSpec{
		ID:        id,
		Name:      name,
		Direction: pasta.InputPort,
		FixedType: NumberType,
		Metadata:  map[string]string{"label": name},
	}
}

func numberOutput(id pasta.PortID, name string) pasta.PortSpec {
	return pasta.PortSpec{
		ID:        id,
		Name:      name,
		Direction: pasta.OutputPort,
		FixedType: NumberType,
		Metadata:  map[string]string{"label": name},
	}
}

type calculatorNodeClass struct {
	kind string
}

func (c calculatorNodeClass) InitNode(ctx pasta.NodeContext, state pasta.NodeState, _ pasta.InitMode) (pasta.NodeRuntime, error) {
	node := &calculatorNode{
		ctx:    ctx,
		kind:   c.kind,
		inputs: make(map[pasta.PortID]*numberWire),
		sinks:  make(map[pasta.LinkID]*numberWire),
		value:  floatFromAny(state.Private),
	}
	if err := ctx.Node.SetMenu(node.menu()); err != nil {
		return nil, err
	}
	return node, nil
}

type calculatorNode struct {
	mu     sync.Mutex
	ctx    pasta.NodeContext
	kind   string
	value  float64
	inputs map[pasta.PortID]*numberWire
	sinks  map[pasta.LinkID]*numberWire
}

// Value is the pull side of the number contract. Downstream nodes call it
// through the numberWire.source field when they want the current value.
func (n *calculatorNode) Value() (float64, error) {
	n.mu.Lock()
	kind := n.kind
	value := n.value
	inputs := map[pasta.PortID]*numberWire{}
	for id, wire := range n.inputs {
		inputs[id] = wire
	}
	n.mu.Unlock()

	switch kind {
	case "constant", "result":
		return value, nil
	case "add":
		a, err := pull(inputs[InputA])
		if err != nil {
			return 0, err
		}
		b, err := pull(inputs[InputB])
		if err != nil {
			return 0, err
		}
		return a + b, nil
	case "subtract":
		a, err := pull(inputs[InputA])
		if err != nil {
			return 0, err
		}
		b, err := pull(inputs[InputB])
		if err != nil {
			return 0, err
		}
		return a - b, nil
	case "divide":
		a, err := pull(inputs[InputA])
		if err != nil {
			return 0, err
		}
		b, err := pull(inputs[InputB])
		if err != nil {
			return 0, err
		}
		if b == 0 {
			return 0, fmt.Errorf("divide by zero")
		}
		return a / b, nil
	default:
		return 0, fmt.Errorf("unknown calculator node kind %q", kind)
	}
}

// Push is the push side of the number contract. Upstream nodes call it when a
// value changes; operator nodes recompute and propagate the result downstream.
func (n *calculatorNode) Push(value float64) {
	if n.kind == "result" {
		n.setValue(value)
		return
	}
	_ = n.recomputeAndPublish()
}

func (n *calculatorNode) LinkObject(endpoint pasta.LinkEndpoint) (any, error) {
	if endpoint.Direction != pasta.InputPort {
		return nil, nil
	}
	wire := &numberWire{}
	if n.kind == "result" || len(n.outputPorts()) > 0 {
		wire.sink = n
	}
	return wire, nil
}

func (n *calculatorNode) BeforeLinkAttach(endpoint pasta.LinkEndpoint, object any) error {
	wire, ok := object.(*numberWire)
	if !ok {
		return fmt.Errorf("calculator link object has type %T, want *numberWire", object)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if endpoint.Direction == pasta.InputPort {
		n.inputs[endpoint.Self.Port] = wire
		return nil
	}
	wire.source = n
	n.sinks[endpoint.Link] = wire
	return nil
}

func (n *calculatorNode) AfterLinkAttach(pasta.LinkEndpoint, any) {
	_ = n.recomputeAndPublish()
}

func (n *calculatorNode) BeforeLinkDetach(pasta.LinkEndpoint) error { return nil }

func (n *calculatorNode) AfterLinkDetach(endpoint pasta.LinkEndpoint) {
	n.mu.Lock()
	if endpoint.Direction == pasta.InputPort {
		delete(n.inputs, endpoint.Self.Port)
	} else {
		delete(n.sinks, endpoint.Link)
	}
	n.mu.Unlock()
	_ = n.recomputeAndPublish()
}

func (n *calculatorNode) ApplyMenuUpdate(update pasta.MenuStateUpdate) (pasta.MenuStateUpdate, error) {
	if n.kind != "constant" {
		return update, nil
	}
	for _, field := range update.Fields {
		if field.Block == "main" && field.Field == "value" {
			value := floatFromAny(field.Value)
			n.setValueWithoutMenu(value)
			n.push(value)
		}
	}
	return update, nil
}

func (n *calculatorNode) TriggerMenuButton(ref pasta.MenuButtonRef) error {
	if n.kind != "result" || ref.Block != "main" || ref.Button != "pull" {
		return nil
	}
	n.mu.Lock()
	wire := n.inputs[InputA]
	n.mu.Unlock()
	value, err := pull(wire)
	if err != nil {
		return err
	}
	n.setValueWithoutMenu(value)
	return nil
}

func (n *calculatorNode) ImportPrivateState(private any) error {
	n.setValue(floatFromAny(private))
	return nil
}

func (n *calculatorNode) ExportPrivateState() (any, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.value, nil
}

func (n *calculatorNode) recomputeAndPublish() error {
	value, err := n.Value()
	if err != nil {
		return err
	}
	n.setValue(value)
	n.push(value)
	return nil
}

func (n *calculatorNode) setValue(value float64) {
	n.setValueWithoutMenu(value)
	_ = n.ctx.Node.SetMenu(n.menu())
}

func (n *calculatorNode) setValueWithoutMenu(value float64) {
	n.mu.Lock()
	n.value = value
	n.mu.Unlock()
	_ = n.ctx.Node.SetPrivate(value)
}

func (n *calculatorNode) push(value float64) {
	n.mu.Lock()
	sinks := make([]numberSink, 0, len(n.sinks))
	for _, wire := range n.sinks {
		if wire.sink != nil {
			sinks = append(sinks, wire.sink)
		}
	}
	n.mu.Unlock()
	for _, sink := range sinks {
		sink.Push(value)
	}
}

func (n *calculatorNode) menu() pasta.NodeMenu {
	n.mu.Lock()
	value := n.value
	kind := n.kind
	n.mu.Unlock()

	field := pasta.MenuField{
		ID:    "value",
		Label: "Value",
		Kind:  pasta.MenuFieldFloat64,
		Value: value,
	}
	if kind != "constant" {
		field.ReadOnly = true
		field.Kind = pasta.MenuFieldReadOnly
	}
	block := pasta.MenuBlock{
		ID:     "main",
		Title:  "Calculator",
		Fields: []pasta.MenuField{field},
	}
	if kind == "result" {
		block.Buttons = []pasta.MenuButton{{ID: "pull", Label: "Pull"}}
	}
	return pasta.NodeMenu{Blocks: []pasta.MenuBlock{block}}
}

func (n *calculatorNode) outputPorts() []pasta.PortID {
	if n.kind == "result" {
		return nil
	}
	return []pasta.PortID{Output}
}

func pull(wire *numberWire) (float64, error) {
	if wire == nil || wire.source == nil {
		return 0, nil
	}
	return wire.source.Value()
}

func floatFromAny(v any) float64 {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0
		}
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case int32:
		return float64(x)
	case uint:
		return float64(x)
	case uint64:
		return float64(x)
	case uint32:
		return float64(x)
	default:
		return 0
	}
}
