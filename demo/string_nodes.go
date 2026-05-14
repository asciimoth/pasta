package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/asciimoth/pasta/pasta"
)

const (
	StringLibraryName = "strings.pasta.demo"

	TextClass         = StringLibraryName + "/Text"
	SplitClass        = StringLibraryName + "/Split"
	TrimClass         = StringLibraryName + "/Trim"
	UppercaseClass    = StringLibraryName + "/Uppercase"
	LowercaseClass    = StringLibraryName + "/Lowercase"
	ReplaceClass      = StringLibraryName + "/Replace"
	StringResultClass = StringLibraryName + "/Result"
	SingleDemoClass   = StringLibraryName + "/SingleDemo"

	StringType = StringLibraryName + "/string"
)

var (
	StringInput      = pasta.PortID{Number: 1, Kind: pasta.InputPort}
	StringOutput     = pasta.PortID{Number: 1, Kind: pasta.OutputPort}
	StringPartOutput = pasta.PortID{Number: 2, Kind: pasta.OutputPort}
	StringRestOutput = pasta.PortID{Number: 3, Kind: pasta.OutputPort}
)

type stringSink interface {
	PushString(string)
}

type stringWire struct {
	sink stringSink
}

type stringOutputWire struct {
	port pasta.PortID
	wire *stringWire
}

type stringDelivery struct {
	sink  stringSink
	value string
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
		{
			Name:        SplitClass,
			DisplayName: "Split",
			Description: "Splits incoming text across first, second, and rest outputs.",
			Default: stringDefault("Split", map[string]any{
				"value":     "",
				"separator": " ",
			}),
			Inputs: []pasta.PortSpec{stringInput(StringInput, "text")},
			Outputs: []pasta.PortSpec{
				stringOutput(StringOutput, "first"),
				stringOutput(StringPartOutput, "second"),
				stringOutput(StringRestOutput, "rest"),
			},
			Runtime: stringNodeClass{kind: "split"},
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
		{
			Name:        SingleDemoClass,
			DisplayName: "Single Demo",
			Description: "Demonstrates a class that allows only one node in the workspace.",
			Default:     stringDefault("Single Demo", map[string]any{"value": "only one allowed"}),
			SingleNode:  true,
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
		sinks: make(map[pasta.LinkID]stringOutputWire),
	}
	if err := ctx.Node.SetMenu(node.menu()); err != nil {
		return nil, err
	}
	return node, nil
}

type stringNode struct {
	mu    sync.RWMutex
	ctx   pasta.NodeContext
	kind  string
	input string
	state stringState
	msgs  []pasta.MessageID
	sinks map[pasta.LinkID]stringOutputWire
}

type stringState struct {
	Value       string `json:"value"`
	Find        string `json:"find,omitempty"`
	Replacement string `json:"replacement,omitempty"`
	Separator   string `json:"separator,omitempty"`
}

func (n *stringNode) PushString(value string) {
	n.mu.Lock()
	n.input = value
	kind := n.kind
	state := n.state
	n.mu.Unlock()

	if kind == "split" {
		n.setValue(value)
		n.pushSplit(value, state.Separator)
		return
	}

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
		n.sinks[endpoint.Link] = stringOutputWire{port: endpoint.Self.Port, wire: wire}
		n.mu.Unlock()
	}
	return nil
}

func (n *stringNode) AfterLinkAttach(endpoint pasta.LinkEndpoint, _ any) {
	if endpoint.Direction == pasta.OutputPort {
		n.pushCurrent(endpoint.Self.Port)
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
	n.mu.RLock()
	state := n.state
	kind := n.kind
	n.mu.RUnlock()

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
		case "separator":
			if kind == "split" {
				state.Separator = stringFromAny(field.Value)
			}
		}
	}

	n.mu.Lock()
	n.state = state
	input := n.input
	n.mu.Unlock()
	if kind == "split" {
		n.setValueWithoutMenu(input)
		n.pushSplit(input, state.Separator)
		return update, nil
	}
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

func (n *stringNode) TriggerMenuButton(ref pasta.MenuButtonRef) error {
	if ref.Block != "main" {
		return nil
	}
	switch ref.Button {
	case "message-note":
		return n.addMessage(pasta.MessageNote, "This is a demo note attached to the node.")
	case "message-warn":
		return n.addMessage(pasta.MessageWarn, "This is a demo warning attached to the node.")
	case "message-err":
		return n.addMessage(pasta.MessageErr, "This is a demo error attached to the node.")
	case "messages-clear":
		return n.clearMessages()
	default:
		return nil
	}
}

func (n *stringNode) addMessage(typ pasta.MessageType, text string) error {
	id, err := n.ctx.Node.AddMessage(typ, text)
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.msgs = append(n.msgs, id)
	n.mu.Unlock()
	return nil
}

func (n *stringNode) clearMessages() error {
	n.mu.Lock()
	ids := append([]pasta.MessageID(nil), n.msgs...)
	n.msgs = nil
	n.mu.Unlock()
	for _, id := range ids {
		if err := n.ctx.Node.RemoveMessage(id); err != nil && !errors.Is(err, pasta.ErrNotFound) {
			return err
		}
	}
	return nil
}

func (n *stringNode) ImportPrivateState(private any) error {
	n.mu.Lock()
	n.state = stringStateFromAny(private)
	n.mu.Unlock()
	_ = n.ctx.Node.SetMenu(n.menu())
	return nil
}

func (n *stringNode) ExportPrivateState() (any, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.state, nil
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
	n.pushOutputs(nil, value)
}

func (n *stringNode) pushCurrent(port pasta.PortID) {
	n.mu.RLock()
	value := n.state.Value
	separator := n.state.Separator
	kind := n.kind
	n.mu.RUnlock()
	if kind == "split" {
		n.pushOutput(port, splitString(value, separator)[port])
		return
	}
	n.pushOutput(port, value)
}

func (n *stringNode) pushSplit(value, separator string) {
	n.pushOutputs(splitString(value, separator), "")
}

func (n *stringNode) pushOutput(port pasta.PortID, value string) {
	n.pushOutputs(map[pasta.PortID]string{port: value}, "")
}

func (n *stringNode) pushOutputs(values map[pasta.PortID]string, fallback string) {
	n.mu.RLock()
	deliveries := make([]stringDelivery, 0, len(n.sinks))
	for _, out := range n.sinks {
		if out.wire == nil || out.wire.sink == nil {
			continue
		}
		value := fallback
		if values != nil {
			var ok bool
			value, ok = values[out.port]
			if !ok {
				continue
			}
		}
		deliveries = append(deliveries, stringDelivery{sink: out.wire.sink, value: value})
	}
	n.mu.RUnlock()
	for _, delivery := range deliveries {
		delivery.sink.PushString(delivery.value)
	}
}

func (n *stringNode) menu() pasta.NodeMenu {
	n.mu.RLock()
	kind := n.kind
	state := n.state
	n.mu.RUnlock()

	var fields []pasta.MenuField
	switch kind {
	case "replace":
		fields = append(fields,
			pasta.MenuField{ID: "find", Label: "Find", Kind: pasta.MenuFieldString, Value: state.Find},
			pasta.MenuField{ID: "replacement", Label: "Replacement", Kind: pasta.MenuFieldString, Value: state.Replacement},
		)
	case "split":
		fields = append(fields,
			pasta.MenuField{ID: "separator", Label: "Separator", Kind: pasta.MenuFieldString, Value: state.Separator},
			pasta.MenuField{ID: "value", Label: "Text", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Value},
		)
	default:
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
		Buttons: []pasta.MenuButton{
			{ID: "message-note", Label: "Show Note"},
			{ID: "message-warn", Label: "Show Warning"},
			{ID: "message-err", Label: "Show Error"},
			{ID: "messages-clear", Label: "Clear Messages"},
		},
	}}}
}

func splitString(value, separator string) map[pasta.PortID]string {
	if separator == "" {
		separator = " "
	}
	parts := strings.SplitN(value, separator, 3)
	out := map[pasta.PortID]string{
		StringOutput:     "",
		StringPartOutput: "",
		StringRestOutput: "",
	}
	if len(parts) > 0 {
		out[StringOutput] = parts[0]
	}
	if len(parts) > 1 {
		out[StringPartOutput] = parts[1]
	}
	if len(parts) > 2 {
		out[StringRestOutput] = parts[2]
	}
	return out
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
			Separator:   stringFromAny(x["separator"]),
		}
	case map[string]string:
		return stringState{Value: x["value"], Find: x["find"], Replacement: x["replacement"], Separator: x["separator"]}
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
