//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/pasta/pasta/examples"
)

type appState struct {
	mu          sync.Mutex
	workspace   *pasta.Workspace
	sub         *pasta.WorkspaceSubscription
	subCallback js.Value
	hasCallback bool
	logs        []string
	clip        pasta.Clipboard
}

type response struct {
	OK    bool     `json:"ok"`
	Error string   `json:"error,omitempty"`
	Data  any      `json:"data,omitempty"`
	Logs  []string `json:"logs,omitempty"`
}

type snapshotDTO struct {
	Classes []classDTO `json:"classes"`
	Nodes   []nodeDTO  `json:"nodes"`
	Links   []linkDTO  `json:"links"`
}

type classDTO struct {
	Name        string    `json:"name"`
	ShortName   string    `json:"shortName"`
	DisplayName string    `json:"displayName"`
	Description string    `json:"description"`
	SingleNode  bool      `json:"singleNode,omitempty"`
	KeyNode     bool      `json:"keyNode,omitempty"`
	Inputs      []portDTO `json:"inputs"`
	Outputs     []portDTO `json:"outputs"`
}

type nodeDTO struct {
	ID          string          `json:"id"`
	Number      int64           `json:"number"`
	Class       string          `json:"class"`
	ShortClass  string          `json:"shortClass"`
	DisplayName string          `json:"displayName"`
	State       string          `json:"state"`
	KeyAccess   bool            `json:"keyAccess"`
	PrimaryType string          `json:"primaryType"`
	Value       float64         `json:"value"`
	Text        string          `json:"text"`
	Coordinate  [2]float64      `json:"coordinate"`
	Inputs      []portDTO       `json:"inputs"`
	Outputs     []portDTO       `json:"outputs"`
	Messages    []messageDTO    `json:"messages,omitempty"`
	Menu        *pasta.NodeMenu `json:"menu,omitempty"`
}

type messageDTO struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
	Text string `json:"text"`
}

type linkDTO struct {
	ID     string      `json:"id"`
	Number int64       `json:"number"`
	Input  endpointDTO `json:"input"`
	Output endpointDTO `json:"output"`
	Type   string      `json:"type"`
	State  string      `json:"state"`
}

type endpointDTO struct {
	Node string `json:"node"`
	Port string `json:"port"`
}

type portDTO struct {
	ID        string `json:"id"`
	Number    int64  `json:"number"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	FixedType string `json:"fixedType,omitempty"`
	Multiple  bool   `json:"multiple,omitempty"`
}

type pasteRequest struct {
	Clipboard string  `json:"clipboard"`
	DX        float64 `json:"dx"`
	DY        float64 `json:"dy"`
}

var app = &appState{}

func main() {
	app.reset()
	js.Global().Set("pastaDemoCall", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return app.fail(fmt.Errorf("missing method"))
		}
		method := args[0].String()
		raw := ""
		if len(args) > 1 {
			raw = args[1].String()
		}
		return app.call(method, raw)
	}))
	js.Global().Set("pastaDemoSubscribe", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || args[0].Type() != js.TypeFunction {
			return app.fail(fmt.Errorf("missing subscription callback"))
		}
		return app.subscribe(args[0])
	}))
	select {}
}

func (a *appState) reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sub != nil {
		a.sub.Close()
		a.sub = nil
	}
	if a.workspace != nil {
		_ = a.workspace.Close()
	}
	a.logs = nil
	a.workspace = pasta.NewWorkspace(pasta.WithLogger((*demoLogger)(a)))
	a.must(a.workspace.RegisterLibrary(examples.CalculatorLibrary{}), "register calculator library")
	a.must(a.workspace.RegisterLibrary(StringLibrary{}), "register string library")
	a.must(a.workspace.RegisterLibrary(StreamLibrary{}), "register stream library")
	a.subscribeLocked()
	a.log("workspace initialized and demo classes registered")
}

func (a *appState) subscribe(callback js.Value) string {
	a.mu.Lock()
	a.subCallback = callback
	a.hasCallback = true
	a.subscribeLocked()
	a.log("workspace notifications subscribed")
	logs := append([]string(nil), a.logs...)
	a.mu.Unlock()
	return a.encode(response{OK: true, Logs: logs})
}

func (a *appState) subscribeLocked() {
	if !a.hasCallback || a.workspace == nil {
		return
	}
	if a.sub != nil {
		a.sub.Close()
	}
	sub := a.workspace.WatchWorkspace(64)
	callback := a.subCallback
	a.sub = sub
	go func() {
		for range sub.Events() {
			callback.Invoke()
		}
	}()
}

func (a *appState) call(method, raw string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var data any
	var err error
	switch method {
	case "snapshot":
		data = a.snapshot()
	case "seed":
		err = a.seed()
		data = a.snapshot()
	case "clear":
		err = a.restoreLocked(pasta.SaveData{})
		data = a.snapshot()
	case "createNode":
		data, err = a.createNode(raw)
	case "deleteNode":
		err = a.deleteNode(raw)
		data = a.snapshot()
	case "moveNode":
		err = a.moveNode(raw)
		data = a.snapshot()
	case "renameNode":
		err = a.renameNode(raw)
		data = a.snapshot()
	case "createLink":
		data, err = a.createLink(raw)
	case "deleteLink":
		err = a.deleteLink(raw)
		data = a.snapshot()
	case "updateMenuField":
		err = a.updateMenuField(raw)
		data = a.snapshot()
	case "triggerMenuButton":
		err = a.triggerMenuButton(raw)
		data = a.snapshot()
	case "copy":
		data, err = a.copy(raw)
	case "paste":
		err = a.paste(raw)
		data = a.snapshot()
	case "save":
		data, err = a.saveDump()
	case "restore":
		err = a.restoreDump(raw)
		data = a.snapshot()
	default:
		err = fmt.Errorf("unknown method %q", method)
	}
	if err != nil {
		a.log("ERROR %s: %v", method, err)
		return a.encode(response{OK: false, Error: err.Error(), Logs: a.logs})
	}
	a.log("OK %s", method)
	return a.encode(response{OK: true, Data: data, Logs: a.logs})
}

func (a *appState) fail(err error) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log("ERROR %v", err)
	return a.encode(response{OK: false, Error: err.Error(), Logs: a.logs})
}

func (a *appState) must(err error, what string) {
	if err != nil {
		panic(fmt.Sprintf("%s: %v", what, err))
	}
}

func (a *appState) log(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	a.logs = append(a.logs, time.Now().Format("15:04:05.000")+" "+line)
	if len(a.logs) > 500 {
		a.logs = append([]string(nil), a.logs[len(a.logs)-500:]...)
	}
}

func (a *appState) encode(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"ok":false,"error":%q}`, err.Error())
	}
	return string(b)
}

func (a *appState) seed() error {
	if err := a.restoreLocked(pasta.SaveData{}); err != nil {
		return err
	}
	left, err := a.newNode(examples.ConstantClass, 80, 80, 10)
	if err != nil {
		return err
	}
	right, err := a.newNode(examples.ConstantClass, 80, 300, 6)
	if err != nil {
		return err
	}
	sum, err := a.newNode(examples.AddClass, 540, 190, 0)
	if err != nil {
		return err
	}
	result, err := a.newNode(examples.ResultClass, 1000, 190, 0)
	if err != nil {
		return err
	}
	if _, err := a.workspace.CreateLink(full(sum, examples.InputA), full(left, examples.Output), pasta.LinkOptions{Type: examples.NumberType}); err != nil {
		return err
	}
	if _, err := a.workspace.CreateLink(full(sum, examples.InputB), full(right, examples.Output), pasta.LinkOptions{Type: examples.NumberType}); err != nil {
		return err
	}
	if _, err := a.workspace.CreateLink(full(result, examples.InputA), full(sum, examples.Output), pasta.LinkOptions{Type: examples.NumberType}); err != nil {
		return err
	}
	if err := a.workspace.TriggerNodeMenuButton(result, pasta.MenuButtonRef{Block: "main", Button: "pull"}); err != nil {
		return err
	}
	text, err := a.newNode(TextClass, 80, 560, 0)
	if err != nil {
		return err
	}
	split, err := a.newNode(SplitClass, 540, 560, 0)
	if err != nil {
		return err
	}
	upper, err := a.newNode(UppercaseClass, 1000, 460, 0)
	if err != nil {
		return err
	}
	lower, err := a.newNode(LowercaseClass, 1000, 680, 0)
	if err != nil {
		return err
	}
	replace, err := a.newNode(ReplaceClass, 1000, 900, 0)
	if err != nil {
		return err
	}
	firstResult, err := a.newNode(StringResultClass, 1460, 460, 0)
	if err != nil {
		return err
	}
	secondResult, err := a.newNode(StringResultClass, 1460, 680, 0)
	if err != nil {
		return err
	}
	restResult, err := a.newNode(StringResultClass, 1460, 900, 0)
	if err != nil {
		return err
	}
	if _, err := a.workspace.CreateLink(full(split, StringInput), full(text, StringOutput), pasta.LinkOptions{Type: StringType}); err != nil {
		return err
	}
	if _, err := a.workspace.CreateLink(full(upper, StringInput), full(split, StringOutput), pasta.LinkOptions{Type: StringType}); err != nil {
		return err
	}
	if _, err := a.workspace.CreateLink(full(lower, StringInput), full(split, StringPartOutput), pasta.LinkOptions{Type: StringType}); err != nil {
		return err
	}
	if _, err := a.workspace.CreateLink(full(replace, StringInput), full(split, StringRestOutput), pasta.LinkOptions{Type: StringType}); err != nil {
		return err
	}
	if _, err := a.workspace.CreateLink(full(firstResult, StringInput), full(upper, StringOutput), pasta.LinkOptions{Type: StringType}); err != nil {
		return err
	}
	if _, err := a.workspace.CreateLink(full(secondResult, StringInput), full(lower, StringOutput), pasta.LinkOptions{Type: StringType}); err != nil {
		return err
	}
	if _, err := a.workspace.CreateLink(full(restResult, StringInput), full(replace, StringOutput), pasta.LinkOptions{Type: StringType}); err != nil {
		return err
	}
	streamSink, err := a.newNode(StreamSinkClass, 80, 1180, 0)
	if err != nil {
		return err
	}
	upperStream, err := a.newNode(StreamUppercaseClass, 540, 1180, 0)
	if err != nil {
		return err
	}
	prefix, err := a.newNode(StreamPrefixClass, 1000, 1180, 0)
	if err != nil {
		return err
	}
	provider, err := a.newNode(StreamProviderClass, 1460, 1180, 0)
	if err != nil {
		return err
	}
	if _, err := a.workspace.CreateLink(full(upperStream, StreamInput), full(streamSink, StreamOutput), pasta.LinkOptions{Type: StreamType}); err != nil {
		return err
	}
	if _, err := a.workspace.CreateLink(full(prefix, StreamInput), full(upperStream, StreamOutput), pasta.LinkOptions{Type: StreamType}); err != nil {
		return err
	}
	if _, err := a.workspace.CreateLink(full(provider, StreamInput), full(prefix, StreamOutput), pasta.LinkOptions{Type: StreamType}); err != nil {
		return err
	}
	a.log("seeded calculator graph, string push graph, and stream pull graph")
	return nil
}

func (a *appState) createNode(raw string) (snapshotDTO, error) {
	var in struct {
		Class string  `json:"class"`
		X     float64 `json:"x"`
		Y     float64 `json:"y"`
		Value float64 `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return snapshotDTO{}, err
	}
	if _, err := a.newNode(in.Class, in.X, in.Y, in.Value); err != nil {
		return snapshotDTO{}, err
	}
	return a.snapshot(), nil
}

func (a *appState) newNode(class string, x, y, value float64) (pasta.NodeID, error) {
	opts := pasta.NodeOptions{}
	switch class {
	case examples.ConstantClass:
		opts = pasta.NodeOptions{UseState: true, State: pasta.NodeState{
			DisplayName: "Constant",
			PrimaryType: examples.NumberType,
			Private:     value,
			Metadata:    map[string]string{"createdBy": "demo"},
		}}
	case TextClass:
		opts = pasta.NodeOptions{UseState: true, State: pasta.NodeState{
			DisplayName: "Text",
			PrimaryType: StringType,
			Private:     map[string]any{"value": "hello PASTA demo"},
			Metadata:    map[string]string{"createdBy": "demo"},
		}}
	}
	id, err := a.workspace.CreateNode(class, opts)
	if err != nil {
		return 0, err
	}
	if err := a.workspace.SetNodeCoordinate(id, encodeCoord(x, y)); err != nil {
		return 0, err
	}
	a.log("created %s node %s at %.0f,%.0f", shortClass(class), id, x, y)
	return id, nil
}

func (a *appState) deleteNode(raw string) error {
	id, err := parseNode(raw)
	if err != nil {
		return err
	}
	a.log("deleting node %s", id)
	return a.workspace.DeleteNode(id)
}

func (a *appState) moveNode(raw string) error {
	var in struct {
		ID string  `json:"id"`
		X  float64 `json:"x"`
		Y  float64 `json:"y"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return err
	}
	id, err := pasta.ParseNodeID(in.ID)
	if err != nil {
		return err
	}
	return a.workspace.SetNodeCoordinate(id, encodeCoord(in.X, in.Y))
}

func (a *appState) renameNode(raw string) error {
	var in struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return err
	}
	id, err := pasta.ParseNodeID(in.ID)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return fmt.Errorf("node name cannot be empty")
	}
	snap, ok := a.workspace.Node(id)
	if !ok {
		return fmt.Errorf("node %s not found", id)
	}
	state := snap.Dynamic
	old := state.DisplayName
	state.DisplayName = name
	if err := a.workspace.SetNodeState(id, state); err != nil {
		return err
	}
	a.log("renamed node %s from %q to %q", id, old, name)
	return nil
}

func (a *appState) createLink(raw string) (snapshotDTO, error) {
	var in struct {
		InputNode  string `json:"inputNode"`
		InputPort  string `json:"inputPort"`
		OutputNode string `json:"outputNode"`
		OutputPort string `json:"outputPort"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return snapshotDTO{}, err
	}
	input, err := fullPort(in.InputNode, in.InputPort)
	if err != nil {
		return snapshotDTO{}, err
	}
	output, err := fullPort(in.OutputNode, in.OutputPort)
	if err != nil {
		return snapshotDTO{}, err
	}
	linkType, err := a.linkType(input, output)
	if err != nil {
		return snapshotDTO{}, err
	}
	id, err := a.workspace.CreateLink(input, output, pasta.LinkOptions{Type: linkType})
	if err != nil {
		return snapshotDTO{}, err
	}
	a.log("created link %s from %s to %s", id, output, input)
	return a.snapshot(), nil
}

func (a *appState) deleteLink(raw string) error {
	id, err := parseLink(raw)
	if err != nil {
		return err
	}
	a.log("deleting link %s", id)
	return a.workspace.DeleteLink(id)
}

func (a *appState) updateMenuField(raw string) error {
	var in struct {
		Node    string `json:"node"`
		Version int64  `json:"version"`
		Block   string `json:"block"`
		Field   string `json:"field"`
		Value   any    `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return err
	}
	id, err := pasta.ParseNodeID(in.Node)
	if err != nil {
		return err
	}
	_, err = a.workspace.UpdateNodeMenuState(id, pasta.MenuStateUpdate{
		Version: in.Version,
		Fields:  []pasta.MenuFieldUpdate{{Block: in.Block, Field: in.Field, Value: in.Value}},
	})
	if err == nil {
		a.log("menu field updated on %s: %s/%s=%v", id, in.Block, in.Field, in.Value)
	}
	return err
}

func (a *appState) triggerMenuButton(raw string) error {
	var in struct {
		Node   string `json:"node"`
		Block  string `json:"block"`
		Button string `json:"button"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return err
	}
	id, err := pasta.ParseNodeID(in.Node)
	if err != nil {
		return err
	}
	err = a.workspace.TriggerNodeMenuButton(id, pasta.MenuButtonRef{Block: in.Block, Button: in.Button})
	if err == nil {
		a.log("menu button triggered on %s: %s/%s", id, in.Block, in.Button)
	}
	return err
}

func (a *appState) copy(raw string) (string, error) {
	var idsText []string
	if err := json.Unmarshal([]byte(raw), &idsText); err != nil {
		return "", err
	}
	ids := make([]pasta.NodeID, 0, len(idsText))
	for _, text := range idsText {
		id, err := pasta.ParseNodeID(text)
		if err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	clip, err := a.workspace.Copy(ids)
	if err != nil {
		return "", err
	}
	a.clip = clip
	data, err := json.MarshalIndent(clip, "", "  ")
	if err != nil {
		return "", err
	}
	a.log("copied %d node(s) and %d internal link(s)", len(clip.Nodes), len(clip.Links))
	return string(data), nil
}

func (a *appState) paste(raw string) error {
	var in pasteRequest
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return err
	}
	clip := a.clip
	if strings.TrimSpace(in.Clipboard) != "" {
		if err := json.Unmarshal([]byte(in.Clipboard), &clip); err != nil {
			return err
		}
	}
	nodes, links, err := a.workspace.Paste(clip)
	if err != nil {
		return err
	}
	for _, id := range nodes {
		snap, ok := a.workspace.Node(id)
		if !ok {
			continue
		}
		x, y := decodeCoord(snap.Dynamic.Coordinate)
		_ = a.workspace.SetNodeCoordinate(id, encodeCoord(x+in.DX, y+in.DY))
	}
	a.log("pasted %d node(s) and %d link(s)", len(nodes), len(links))
	return nil
}

func (a *appState) saveDump() (string, error) {
	data, err := a.workspace.SaveWithRuntimeState()
	if err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	a.log("saved workspace dump with %d node(s) and %d link(s)", len(data.Nodes), len(data.Links))
	return string(out), nil
}

func (a *appState) restoreDump(raw string) error {
	var data pasta.SaveData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return err
	}
	return a.restoreLocked(data)
}

func (a *appState) restoreLocked(data pasta.SaveData) error {
	if a.workspace != nil {
		_ = a.workspace.Close()
	}
	a.workspace = pasta.NewWorkspace(pasta.WithLogger((*demoLogger)(a)))
	if err := a.workspace.RegisterLibrary(examples.CalculatorLibrary{}); err != nil {
		return err
	}
	if err := a.workspace.RegisterLibrary(StringLibrary{}); err != nil {
		return err
	}
	if err := a.workspace.RegisterLibrary(StreamLibrary{}); err != nil {
		return err
	}
	a.subscribeLocked()
	if err := a.workspace.Restore(data); err != nil {
		return err
	}
	if err := a.rehydrateLinks(); err != nil {
		return err
	}
	a.log("restored workspace dump with runtime links rehydrated")
	return nil
}

func (a *appState) rehydrateLinks() error {
	links := a.workspace.Snapshot().Links
	sort.Slice(links, func(i, j int) bool { return links[i].ID < links[j].ID })
	for _, link := range links {
		if err := a.workspace.DeleteLink(link.ID); err != nil {
			return err
		}
		if _, err := a.workspace.CreateLink(link.Input, link.Output, pasta.LinkOptions{
			Type: link.Type, Waypoints: link.Waypoints,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (a *appState) snapshot() snapshotDTO {
	s := a.workspace.Snapshot()
	out := snapshotDTO{}
	for _, class := range s.Classes {
		if !class.Active || (class.Library != examples.CalculatorLibraryName && class.Library != StringLibraryName && class.Library != StreamLibraryName) {
			continue
		}
		out.Classes = append(out.Classes, classDTO{
			Name: class.Spec.Name, ShortName: shortClass(class.Spec.Name),
			DisplayName: class.Spec.DisplayName, Description: class.Spec.Description,
			SingleNode: class.Spec.SingleNode, KeyNode: class.Spec.KeyNode,
			Inputs: portsDTO(class.Spec.Inputs), Outputs: portsDTO(class.Spec.Outputs),
		})
	}
	for _, node := range s.Nodes {
		x, y := decodeCoord(node.Dynamic.Coordinate)
		out.Nodes = append(out.Nodes, nodeDTO{
			ID: node.ID.String(), Number: int64(node.ID), Class: node.Class,
			ShortClass: shortClass(node.Class), DisplayName: node.Dynamic.DisplayName,
			State: string(node.State), KeyAccess: node.HasKeyNodeAccess, PrimaryType: node.Dynamic.PrimaryType,
			Value: numberValue(node.Dynamic.Private), Text: textValue(node.Dynamic.Private),
			Coordinate: [2]float64{x, y}, Inputs: portsDTO(node.Inputs),
			Outputs: portsDTO(node.Outputs), Messages: messagesDTO(node.Messages), Menu: node.Menu,
		})
	}
	for _, link := range s.Links {
		out.Links = append(out.Links, linkDTO{
			ID: link.ID.String(), Number: int64(link.ID),
			Input:  endpointDTO{Node: link.Input.Node.String(), Port: link.Input.Port.String()},
			Output: endpointDTO{Node: link.Output.Node.String(), Port: link.Output.Port.String()},
			Type:   link.Type, State: string(link.State),
		})
	}
	return out
}

func (a *appState) linkType(input, output pasta.FullPortID) (string, error) {
	inNode, ok := a.workspace.Node(input.Node)
	if !ok {
		return "", fmt.Errorf("input node %s not found", input.Node)
	}
	outNode, ok := a.workspace.Node(output.Node)
	if !ok {
		return "", fmt.Errorf("output node %s not found", output.Node)
	}
	inPort, ok := findPort(inNode.Inputs, input.Port)
	if !ok {
		return "", fmt.Errorf("input port %s not found", input.Port)
	}
	outPort, ok := findPort(outNode.Outputs, output.Port)
	if !ok {
		return "", fmt.Errorf("output port %s not found", output.Port)
	}
	if outPort.FixedType != "" {
		return outPort.FixedType, nil
	}
	if inPort.FixedType != "" {
		return inPort.FixedType, nil
	}
	return "", nil
}

func findPort(ports []pasta.PortSpec, id pasta.PortID) (pasta.PortSpec, bool) {
	for _, port := range ports {
		if port.ID == id {
			return port, true
		}
	}
	return pasta.PortSpec{}, false
}

func portsDTO(ports []pasta.PortSpec) []portDTO {
	out := make([]portDTO, 0, len(ports))
	for _, port := range ports {
		out = append(out, portDTO{
			ID: port.ID.String(), Number: port.ID.Number, Kind: string(port.Direction),
			Name: port.Name, FixedType: port.FixedType, Multiple: port.Multiple,
		})
	}
	return out
}

func messagesDTO(messages []pasta.NodeMessage) []messageDTO {
	out := make([]messageDTO, 0, len(messages))
	for _, message := range messages {
		out = append(out, messageDTO{
			ID:   int64(message.ID),
			Type: string(message.Type),
			Text: message.Text,
		})
	}
	return out
}

func parseNode(raw string) (pasta.NodeID, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return 0, err
	}
	return pasta.ParseNodeID(in.ID)
}

func parseLink(raw string) (pasta.LinkID, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return 0, err
	}
	return pasta.ParseLinkID(in.ID)
}

func fullPort(nodeText, portText string) (pasta.FullPortID, error) {
	node, err := pasta.ParseNodeID(nodeText)
	if err != nil {
		return pasta.FullPortID{}, err
	}
	port, err := pasta.ParsePortID(portText)
	if err != nil {
		return pasta.FullPortID{}, err
	}
	return full(node, port), nil
}

func full(node pasta.NodeID, port pasta.PortID) pasta.FullPortID {
	return pasta.FullPortID{Node: node, Port: port}
}

func encodeCoord(x, y float64) string {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		x = 0
	}
	if math.IsNaN(y) || math.IsInf(y, 0) {
		y = 0
	}
	data, _ := json.Marshal([2]float64{x, y})
	return string(data)
}

func decodeCoord(text string) (float64, float64) {
	var xy [2]float64
	if err := json.Unmarshal([]byte(text), &xy); err == nil {
		return xy[0], xy[1]
	}
	return 0, 0
}

func shortClass(class string) string {
	if i := strings.LastIndex(class, "/"); i >= 0 {
		return class[i+1:]
	}
	return class
}

func numberValue(v any) float64 {
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
	case json.Number:
		f, _ := x.Float64()
		return f
	default:
		return 0
	}
}

func textValue(v any) string {
	if text, ok := streamTextValue(v); ok {
		return text
	}
	return stringStateFromAny(v).Value
}

type demoLogger appState

func (l *demoLogger) Debug(args ...any)                 { (*appState)(l).log("debug %s", fmt.Sprint(args...)) }
func (l *demoLogger) Debugf(format string, args ...any) { (*appState)(l).log("debug "+format, args...) }
func (l *demoLogger) Info(args ...any)                  { (*appState)(l).log("info %s", fmt.Sprint(args...)) }
func (l *demoLogger) Infof(format string, args ...any)  { (*appState)(l).log("info "+format, args...) }
func (l *demoLogger) Warn(args ...any)                  { (*appState)(l).log("warn %s", fmt.Sprint(args...)) }
func (l *demoLogger) Warnf(format string, args ...any)  { (*appState)(l).log("warn "+format, args...) }
func (l *demoLogger) Err(args ...any)                   { (*appState)(l).log("error %s", fmt.Sprint(args...)) }
func (l *demoLogger) Errf(format string, args ...any)   { (*appState)(l).log("error "+format, args...) }
func (l *demoLogger) Fatal(args ...any)                 { (*appState)(l).log("fatal %s", fmt.Sprint(args...)) }
func (l *demoLogger) Fatalf(format string, args ...any) { (*appState)(l).log("fatal "+format, args...) }
