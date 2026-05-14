# Library Tutorial

This tutorial is for authors of Pasta libraries and node runtimes.

The tested reference code for this tutorial is:

- `pasta/examples/calculator.go`
- `pasta/examples/usage_test.go`
- `demo/string_nodes.go`
- `demo/stream_nodes.go`
- `demo/pastatest_test.go`
- `pasta/pastatest/suite.go`

## 1. Choose Names And Types

A library has one namespace-like name. Classes live under that prefix and start with an uppercase segment. Types are lowercase names under a library-like prefix.

```go
const (
	CalculatorLibraryName = "calc.pasta.example.com"

	ConstantClass = CalculatorLibraryName + "/Constant"
	AddClass      = CalculatorLibraryName + "/Add"
	ResultClass   = CalculatorLibraryName + "/Result"

	NumberType = CalculatorLibraryName + "/number"
)
```

Define stable port IDs for every public port:

```go
var (
	InputA = pasta.PortID{Number: 1, Kind: pasta.InputPort}
	InputB = pasta.PortID{Number: 2, Kind: pasta.InputPort}
	Output = pasta.PortID{Number: 1, Kind: pasta.OutputPort}
)
```

## 2. Implement The Library

The library publishes classes through the `LibraryScope` it receives during registration.

```go
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
```

`DefineClasses` is transactional. If it returns an error or panics, registration rolls back.

## 3. Define Classes

A class describes default node state, ports, metadata, and an optional runtime factory.

```go
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
}
```

Set `SingleNode: true` only for classes that represent a workspace-wide object,
such as a singleton inspector or demo-only global control. Pasta rejects direct
creation when one already exists, while paste drops duplicate single-node
entries and keeps the rest of the clipboard.

Set `KeyNode: true` for classes that are observable or meaningful application
roots. Nodes of other classes have key-node access only while connected to at
least one active key node. Inactive nodes are not treated as key nodes, even
when their class is marked key.

Keep port definitions small and explicit:

```go
func numberInput(id pasta.PortID, name string) pasta.PortSpec {
	return pasta.PortSpec{
		ID:        id,
		Name:      name,
		Direction: pasta.InputPort,
		FixedType: NumberType,
		Metadata:  map[string]string{"label": name},
	}
}
```

Use `AcceptedTypes` instead of `FixedType` only when a flexible port is really needed.

## 4. Initialize Node Runtimes

A class runtime implements `InitNode`. It receives a `NodeContext`, cloned initial state, and an `InitMode`.

```go
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
```

Runtime callbacks run outside the workspace lock. Use the `NodeScope` in `ctx.Node` for workspace mutations owned by that node, and expect those calls to return errors if the node is deleted, inactivated, or the workspace closes.

## 5. Define The Link Contract

Pasta validates graph shape and stores an opaque link object. Your library defines what the object means.

The calculator link object from examples supports both pull and push:

```go
type numberWire struct {
	source numberSource
	sink   numberSink
}
```

The input runtime can provide the object:

```go
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
```

Both endpoints validate and attach it:

```go
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
```

Before hooks may reject the operation. After hooks observe committed state and should tolerate failures without assuming rollback.

## 6. Pick A Data Flow

Pasta does not force one execution model. The tested examples demonstrate several patterns.

Pull: downstream code calls upstream through the link object.

```go
func (n *calculatorNode) Value() (float64, error) {
	a, err := pull(inputs[InputA])
	if err != nil {
		return 0, err
	}
	b, err := pull(inputs[InputB])
	if err != nil {
		return 0, err
	}
	return a + b, nil
}
```

Push: upstream code notifies sinks when its value changes.

```go
func (n *calculatorNode) Push(value float64) {
	if n.kind == "result" {
		n.setValue(value)
		return
	}
	_ = n.recomputeAndPublish()
}
```

Stream/pull with workers: demo stream nodes mark sinks as key nodes. Sink
runtimes keep link objects after attach, but only launch pull goroutines while
`HasKeyNodeAccess(true)` is in effect. They stop those workers on lost access,
detach, inactive, delete, and close.

```go
func (n *streamNode) HasKeyNodeAccess(access bool) {
	n.mu.Lock()
	if n.keyAccess == access {
		n.mu.Unlock()
		return
	}
	n.keyAccess = access
	if !access {
		cancels := n.pullCancels
		n.pullCancels = make(map[pasta.LinkID]context.CancelFunc)
		n.mu.Unlock()
		for _, cancel := range cancels {
			cancel()
		}
		return
	}
	for link, wire := range n.pullWires {
		n.startPullLocked(link, wire)
	}
	n.mu.Unlock()
}
```

For long-lived work, keep your own cancellation primitive. Pasta will notify
lifecycle and key-node access hooks but will not stop goroutines for you. Some
nodes may intentionally delay shutdown after losing key-node access, for example
to avoid thrashing workers while a user is rewiring a graph.

## 7. Store Private State

`NodeState.Private` is application-owned state. Use `NodeScope.SetPrivate` when runtime changes should be visible in snapshots and `Save`.

```go
func (n *calculatorNode) setValueWithoutMenu(value float64) {
	n.mu.Lock()
	n.value = value
	n.mu.Unlock()
	_ = n.ctx.Node.SetPrivate(value)
}
```

If the runtime owns fresher volatile state, implement export/import hooks:

```go
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
```

`SaveWithRuntimeState`, `SaveConfigWithRuntimeState`, and `Copy` use `ExportPrivateState` for active nodes.

## 8. Implement Node Menus

Menus are the generic control surface for node-specific UI. Build them from runtime state and install them with `NodeScope.SetMenu`.

The schema is intentionally small:

- `NodeMenu`: top-level document with a workspace-assigned `Version`, `Blocks`, and optional `Metadata`.
- `MenuBlock`: a named group with `Fields`, `Buttons`, `Repeats`, and optional title/metadata.
- `MenuField`: one scalar value. Supported kinds are `read-only`, `string`, `int64`, `float64`, and `bool`.
- `MenuOption`: an allowed value for a field, with optional label and disabled flag.
- `MenuButton`: an action by ID. Triggering it calls `NodeMenuButtonHook`.
- `MenuRepeat`: a repeatable list. Its `Template` defines the fields every item can contain, and `Items` holds current item state.
- `MenuRepeatItem`: one row/item in a repeat list.

Use `MenuRenderCheckbox` only for `bool` fields. `read-only` fields may hold JSON-compatible values and are rejected in external state updates.

```go
func (n *streamNode) menu() pasta.NodeMenu {
	fields := []pasta.MenuField{
		{ID: "value", Label: "Latest", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Value},
		{ID: "count", Label: "Count", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Count},
	}
	return pasta.NodeMenu{Blocks: []pasta.MenuBlock{{
		ID:      "main",
		Title:   "Stream",
		Fields:  fields,
		Buttons: []pasta.MenuButton{{ID: "messages-clear", Label: "Clear Messages"}},
	}}}
}
```

Options turn a scalar field into a fixed choice. The workspace validates both the current value and incoming updates against the option values:

```go
pasta.MenuField{
	ID:      "choice",
	Kind:    pasta.MenuFieldString,
	Value:   "a",
	Options: []pasta.MenuOption{{Value: "a"}, {Value: "b"}},
}
```

Repeat lists are for arrays of field groups. The template is the allowed field schema; each item supplies an item ID plus values for fields from that template. During normalization, item fields inherit the template kind, options, render hint, label, read-only flag, and metadata unless the item explicitly overrides allowed display data.

```go
pasta.MenuRepeat{
	ID: "rows",
	Template: []pasta.MenuField{
		{ID: "name", Label: "Name", Kind: pasta.MenuFieldString},
		{ID: "count", Label: "Count", Kind: pasta.MenuFieldInt64, Value: int64(1)},
	},
	Items: []pasta.MenuRepeatItem{{
		ID:    "one",
		Title: "One",
		Fields: []pasta.MenuField{
			{ID: "name", Value: "alpha"},
			{ID: "count", Value: int64(1)},
		},
	}},
}
```

External updates replace repeat item state for a repeat group:

```go
pasta.MenuStateUpdate{
	Version: menu.Version,
	Repeats: []pasta.MenuRepeatUpdate{{
		Block:  "main",
		Repeat: "rows",
		Items: []pasta.MenuRepeatItemState{{
			ID:     "one",
			Title:  "One",
			Fields: map[string]any{"name": "alpha", "count": int64(1)},
		}},
	}},
}
```

Design repeat item IDs as stable application IDs, not display indexes, so a UI can preserve focus and local drafts when rows are reordered or refreshed.

Validate or normalize external edits with `ApplyMenuUpdate`:

```go
func (n *stringNode) ApplyMenuUpdate(update pasta.MenuStateUpdate) (pasta.MenuStateUpdate, error) {
	for _, field := range update.Fields {
		if field.Block != "main" {
			continue
		}
		switch field.Field {
		case "value":
			if kind == "text" {
				state.Value = stringFromAny(field.Value)
			}
		}
	}
	return update, nil
}
```

Buttons are handled with `TriggerMenuButton`:

```go
func (n *stringNode) TriggerMenuButton(ref pasta.MenuButtonRef) error {
	if ref.Block != "main" {
		return nil
	}
	switch ref.Button {
	case "message-note":
		return n.addMessage(pasta.MessageNote, "This is a demo note attached to the node.")
	case "messages-clear":
		return n.clearMessages()
	default:
		return nil
	}
}
```

## 9. Send Messages

Messages are ephemeral node notifications. They appear in snapshots and watcher events, but they are not saved or copied.

```go
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
```

When clearing remembered message IDs, ignore `ErrNotFound`; the UI or workspace may already have removed them.

## 10. Implement Lifecycle Cleanup

Detach, inactive, delete, and close hooks should release runtime resources and unblock in-flight work.

```go
func (n *streamNode) AfterLinkInactive(endpoint pasta.LinkEndpoint, _ pasta.InactiveReason) {
	n.closeLink(endpoint.Link)
}

func (n *streamNode) AfterInactive(pasta.InactiveReason) {
	n.closeAll()
}

func (n *streamNode) Close() error {
	n.closeAll()
	return nil
}
```

Class recall, library unregister, restore, and workspace close can all remove or inactivate active runtime objects. Hooks must be idempotent where practical.

## 11. Test Compliance With pastatest

`pastatest.RunSuite` checks class registration, node creation, link validation, persistence, copy/paste, ownership, and inactive recovery.

```go
func TestCalculatorLibraryConforms(t *testing.T) {
	pastatest.RunSuite(t, pastatest.Suite{
		LibraryName: examples.CalculatorLibraryName,
		NewLibrary: func(*testing.T) pasta.Library {
			return examples.CalculatorLibrary{}
		},
		Classes: examples.CalculatorClasses(),
		Links: []pastatest.LinkCase{{
			Name:   "constant to add",
			Output: pastatest.Endpoint{Class: examples.ConstantClass, Port: examples.Output},
			Input:  pastatest.Endpoint{Class: examples.AddClass, Port: examples.InputA},
			Type:   examples.NumberType,
		}},
	})
}
```

Add focused behavior tests for the runtime contract itself. The calculator tests cover push, pull, mixed recomputation, save/restore, copy/paste, and link removal.
