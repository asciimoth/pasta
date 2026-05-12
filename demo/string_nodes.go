package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/asciimoth/pasta/pasta"
)

const (
	StringLibraryName = "strings.pasta.demo"

	TextClass         = StringLibraryName + "/Text"
	TrimClass         = StringLibraryName + "/Trim"
	UppercaseClass    = StringLibraryName + "/Uppercase"
	LowercaseClass    = StringLibraryName + "/Lowercase"
	ReplaceClass      = StringLibraryName + "/Replace"
	StringResultClass = StringLibraryName + "/Result"

	StringType = StringLibraryName + "/string"
)

var (
	StringInput  = pasta.PortID{Number: 1, Kind: pasta.InputPort}
	StringOutput = pasta.PortID{Number: 1, Kind: pasta.OutputPort}
)

type stringSink interface {
	PushString(string)
}

type stringWire struct {
	sink stringSink
}

type StringLibrary struct{}

func (StringLibrary) Name() string { return StringLibraryName }

func (StringLibrary) DefineClasses(scope pasta.LibraryScope) error {
	for _, class := range StringClasses() {
		if err := scope.DefineClass(class); err != nil {
			return err
		}
	}
	return nil
}

func StringClasses() []pasta.ClassSpec {
	return []pasta.ClassSpec{
		{
			Name:        TextClass,
			DisplayName: "Text",
			Description: "Stores text and pushes it downstream when edited.",
			Default:     stringDefault("Text", map[string]any{"value": "hello pasta"}),
			Outputs:     []pasta.PortSpec{stringOutput(StringOutput, "text")},
			Runtime:     stringNodeClass{kind: "text"},
		},
		stringTransformClass(TrimClass, "Trim", "Trims leading and trailing whitespace.", "trim"),
		stringTransformClass(UppercaseClass, "Uppercase", "Converts text to upper case.", "uppercase"),
		stringTransformClass(LowercaseClass, "Lowercase", "Converts text to lower case.", "lowercase"),
		{
			Name:        ReplaceClass,
			DisplayName: "Replace",
			Description: "Replaces matching text and pushes the result.",
			Default: stringDefault("Replace", map[string]any{
				"value":       "",
				"find":        "PASTA",
				"replacement": "Pasta",
			}),
			Inputs:  []pasta.PortSpec{stringInput(StringInput, "text")},
			Outputs: []pasta.PortSpec{stringOutput(StringOutput, "text")},
			Runtime: stringNodeClass{kind: "replace"},
		},
		{
			Name:        StringResultClass,
			DisplayName: "String Result",
			Description: "Receives pushed text and displays the latest value.",
			Default:     stringDefault("String Result", map[string]any{"value": ""}),
			Inputs:      []pasta.PortSpec{stringInput(StringInput, "text")},
			Runtime:     stringNodeClass{kind: "result"},
		},
	}
}

func stringTransformClass(name, display, description, kind string) pasta.ClassSpec {
	return pasta.ClassSpec{
		Name:        name,
		DisplayName: display,
		Description: description,
		Default:     stringDefault(display, map[string]any{"value": ""}),
		Inputs:      []pasta.PortSpec{stringInput(StringInput, "text")},
		Outputs:     []pasta.PortSpec{stringOutput(StringOutput, "text")},
		Runtime:     stringNodeClass{kind: kind},
	}
}

func stringDefault(display string, private map[string]any) pasta.NodeState {
	return pasta.NodeState{
		DisplayName: display,
		PrimaryType: StringType,
		Private:     private,
		Metadata:    map[string]string{"palette": "strings"},
	}
}

func stringInput(id pasta.PortID, name string) pasta.PortSpec {
	return pasta.PortSpec{
		ID:        id,
		Name:      name,
		Direction: pasta.InputPort,
		FixedType: StringType,
		Metadata:  map[string]string{"label": name},
	}
}

func stringOutput(id pasta.PortID, name string) pasta.PortSpec {
	return pasta.PortSpec{
		ID:        id,
		Name:      name,
		Direction: pasta.OutputPort,
		FixedType: StringType,
		Metadata:  map[string]string{"label": name},
	}
}

type stringNodeClass struct {
	kind string
}

func (c stringNodeClass) InitNode(ctx pasta.NodeContext, state pasta.NodeState, _ pasta.InitMode) (pasta.NodeRuntime, error) {
	node := &stringNode{
		ctx:   ctx,
		kind:  c.kind,
		state: stringStateFromAny(state.Private),
		sinks: make(map[pasta.LinkID]*stringWire),
	}
	if err := ctx.Node.SetMenu(node.menu()); err != nil {
		return nil, err
	}
	return node, nil
}

type stringNode struct {
	mu    sync.Mutex
	ctx   pasta.NodeContext
	kind  string
	input string
	state stringState
	sinks map[pasta.LinkID]*stringWire
}

type stringState struct {
	Value       string `json:"value"`
	Find        string `json:"find,omitempty"`
	Replacement string `json:"replacement,omitempty"`
}

func (n *stringNode) PushString(value string) {
	n.mu.Lock()
	n.input = value
	kind := n.kind
	state := n.state
	n.mu.Unlock()

	value = processString(kind, state, value)
	n.setValue(value)
	n.push(value)
}

func (n *stringNode) LinkObject(endpoint pasta.LinkEndpoint) (any, error) {
	if endpoint.Direction != pasta.InputPort {
		return nil, nil
	}
	return &stringWire{sink: n}, nil
}

func (n *stringNode) BeforeLinkAttach(endpoint pasta.LinkEndpoint, object any) error {
	wire, ok := object.(*stringWire)
	if !ok {
		return fmt.Errorf("string link object has type %T, want *stringWire", object)
	}
	if endpoint.Direction == pasta.OutputPort {
		n.mu.Lock()
		n.sinks[endpoint.Link] = wire
		n.mu.Unlock()
	}
	return nil
}

func (n *stringNode) AfterLinkAttach(endpoint pasta.LinkEndpoint, _ any) {
	if endpoint.Direction == pasta.OutputPort {
		n.push(n.value())
	}
}

func (n *stringNode) BeforeLinkDetach(pasta.LinkEndpoint) error { return nil }

func (n *stringNode) AfterLinkDetach(endpoint pasta.LinkEndpoint) {
	if endpoint.Direction == pasta.OutputPort {
		n.mu.Lock()
		delete(n.sinks, endpoint.Link)
		n.mu.Unlock()
		return
	}
	if n.kind != "text" {
		n.setValue("")
		n.push("")
	}
}

func (n *stringNode) ApplyMenuUpdate(update pasta.MenuStateUpdate) (pasta.MenuStateUpdate, error) {
	n.mu.Lock()
	state := n.state
	kind := n.kind
	n.mu.Unlock()

	for _, field := range update.Fields {
		if field.Block != "main" {
			continue
		}
		switch field.Field {
		case "value":
			if kind == "text" {
				state.Value = stringFromAny(field.Value)
			}
		case "find":
			if kind == "replace" {
				state.Find = stringFromAny(field.Value)
			}
		case "replacement":
			if kind == "replace" {
				state.Replacement = stringFromAny(field.Value)
			}
		}
	}

	n.mu.Lock()
	n.state = state
	input := n.input
	n.mu.Unlock()
	if kind == "replace" {
		value := processString(kind, state, input)
		n.setValueWithoutMenu(value)
		n.push(value)
		return update, nil
	}
	n.setValueWithoutMenu(state.Value)
	n.push(state.Value)
	return update, nil
}

func (n *stringNode) ImportPrivateState(private any) error {
	n.mu.Lock()
	n.state = stringStateFromAny(private)
	n.mu.Unlock()
	_ = n.ctx.Node.SetMenu(n.menu())
	return nil
}

func (n *stringNode) ExportPrivateState() (any, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.state, nil
}

func (n *stringNode) value() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.state.Value
}

func (n *stringNode) setValue(value string) {
	n.setValueWithoutMenu(value)
	_ = n.ctx.Node.SetMenu(n.menu())
}

func (n *stringNode) setValueWithoutMenu(value string) {
	n.mu.Lock()
	n.state.Value = value
	state := n.state
	n.mu.Unlock()
	_ = n.ctx.Node.SetPrivate(state)
}

func (n *stringNode) push(value string) {
	n.mu.Lock()
	sinks := make([]stringSink, 0, len(n.sinks))
	for _, wire := range n.sinks {
		if wire.sink != nil {
			sinks = append(sinks, wire.sink)
		}
	}
	n.mu.Unlock()
	for _, sink := range sinks {
		sink.PushString(value)
	}
}

func (n *stringNode) menu() pasta.NodeMenu {
	n.mu.Lock()
	kind := n.kind
	state := n.state
	n.mu.Unlock()

	var fields []pasta.MenuField
	if kind == "replace" {
		fields = append(fields,
			pasta.MenuField{ID: "find", Label: "Find", Kind: pasta.MenuFieldString, Value: state.Find},
			pasta.MenuField{ID: "replacement", Label: "Replacement", Kind: pasta.MenuFieldString, Value: state.Replacement},
		)
	} else {
		valueField := pasta.MenuField{
			ID:    "value",
			Label: "Text",
			Kind:  pasta.MenuFieldString,
			Value: state.Value,
		}
		if kind != "text" {
			valueField.Kind = pasta.MenuFieldReadOnly
			valueField.ReadOnly = true
		}
		fields = append(fields, valueField)
	}
	return pasta.NodeMenu{Blocks: []pasta.MenuBlock{{
		ID:     "main",
		Title:  "String",
		Fields: fields,
	}}}
}

func processString(kind string, state stringState, value string) string {
	switch kind {
	case "trim":
		return strings.TrimSpace(value)
	case "uppercase":
		return strings.ToUpper(value)
	case "lowercase":
		return strings.ToLower(value)
	case "replace":
		if state.Find != "" {
			return strings.ReplaceAll(value, state.Find, state.Replacement)
		}
	}
	return value
}

func stringStateFromAny(v any) stringState {
	switch x := v.(type) {
	case stringState:
		return x
	case map[string]any:
		return stringState{
			Value:       stringFromAny(x["value"]),
			Find:        stringFromAny(x["find"]),
			Replacement: stringFromAny(x["replacement"]),
		}
	case map[string]string:
		return stringState{Value: x["value"], Find: x["find"], Replacement: x["replacement"]}
	case string:
		return stringState{Value: x}
	default:
		return stringState{}
	}
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
