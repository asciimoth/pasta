package std

import (
	"reflect"
	"slices"
	"strconv"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/persist"
)

// NodeTypeObjectUnpacker is the class name for ObjectUnpackerClass.
const NodeTypeObjectUnpacker = "pasta/ObjectUnpacker"

// ObjectUnpackerClass creates nodes that unpack one object into typed outputs.
type ObjectUnpackerClass struct{}

func (ObjectUnpackerClass) ClassName() string        { return NodeTypeObjectUnpacker }
func (ObjectUnpackerClass) ShortDescription() string { return "Unpack object" }
func (ObjectUnpackerClass) LongDescription() string {
	return "Reads configured map/vector paths from one pasta/object input and exposes them as typed output ports."
}
func (ObjectUnpackerClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeObject, InitialPorts: []pasta.Port{
		{Direction: "left", Name: "input", Types: []string{TypeObject}},
	}}
}
func (ObjectUnpackerClass) NewNode(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
	config := readObjectUnpackerConfig(cfg)
	if state := firstState(previous); state != nil {
		reconcileObjectUnpackerState(state, config)
	}
	return newObjectUnpackerNode(config), nil
}

type objectUnpackerConfig struct {
	Outputs []objectUnpackerOutput
}

type objectUnpackerOutput struct {
	ID      string
	Name    string
	Type    string
	Path    []objectPathStep
	Default any
}

type objectUnpackerNode struct {
	pasta.BasicNode

	config  objectUnpackerConfig
	input   Object
	outputs map[uint64]any

	w      *pasta.Workspace
	id     uint64
	in     uint64
	rights map[string]uint64
	fields map[uint64]string
}

func newObjectUnpackerNode(config objectUnpackerConfig) *objectUnpackerNode {
	config = normalizeObjectUnpackerConfig(config)
	n := &objectUnpackerNode{
		config:  config,
		input:   NilObject(),
		outputs: map[uint64]any{},
		rights:  map[string]uint64{},
		fields:  map[uint64]string{},
	}
	n.recalculate(false)
	return n
}

func (n *objectUnpackerNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if n.outputs == nil {
		n.outputs = map[uint64]any{}
	}
	if restored != nil {
		if len(restored.LeftPorts) > 0 {
			n.in = restored.LeftPorts[0]
		}
		n.refreshRights()
	}
	if err := n.w.SetNodePrimaryLocked(n.id, TypeObject); err != nil {
		return err
	}
	if err := n.updatePorts(); err != nil {
		return err
	}
	n.recalculate(false)
	if err := n.clearLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot(false)
	return nil
}

func (n *objectUnpackerNode) OnReady() error {
	n.requestInput()
	n.sendAll()
	return nil
}

func (n *objectUnpackerNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if portDirection == "left" {
		if port != n.in || linkType != TypeObject {
			return pasta.LinkTypeErr(linkType)
		}
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
		return nil
	}
	fieldID, ok := n.fields[port]
	if !ok {
		return pasta.LinkTypeErr(linkType)
	}
	output, ok := n.outputByID(fieldID)
	if !ok || linkType != output.Type {
		return pasta.LinkTypeErr(linkType)
	}
	return nil
}

func (n *objectUnpackerNode) OnLinkAdd(link, port uint64, _, portDirection string) error {
	if portDirection == "left" {
		n.requestLink(link, port)
		return nil
	}
	n.sendToLink(link, port)
	return nil
}

func (n *objectUnpackerNode) OnLinkRemoved(_ uint64, port uint64, _ string, portDirection string) error {
	if portDirection == "left" && port == n.in {
		n.input = NilObject()
		n.recalculate(true)
	}
	return nil
}

func (n *objectUnpackerNode) OnPortAdd(port uint64, direction string, _ []string) error {
	if direction == "right" {
		n.refreshRights()
	}
	return nil
}

func (n *objectUnpackerNode) OnPortRemoved(port uint64, direction string) error {
	if direction == "right" {
		delete(n.outputs, port)
		n.refreshRights()
	}
	return nil
}

func (n *objectUnpackerNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if receiverPortDirection == "left" {
		if event.ReceiverPort != n.in || linkType != TypeObject {
			return nil
		}
		object, ok := ObjectFromPayload(event.Payload)
		if !ok {
			return nil
		}
		n.input = object
		n.recalculate(true)
		return nil
	}
	if !isValueRequest(event.Payload) {
		return nil
	}
	n.sendToLink(event.Link, event.ReceiverPort)
	return nil
}

func (n *objectUnpackerNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.FormApplyMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(n.id) || msg.BlockID != "object" {
		return nil
	}
	next, ok := parseObjectUnpackerForm(msg.Values)
	if !ok {
		n.sendMenuSnapshot(true)
		return nil
	}
	if objectUnpackerConfigsEqual(next, n.config) {
		n.sendMenuBlock()
		return nil
	}
	n.config = next
	if err := n.updatePorts(); err != nil {
		return err
	}
	n.recalculate(true)
	n.sendMenuSnapshot(true)
	return nil
}

func (n *objectUnpackerNode) OnSave(cfg configer.Config) error {
	if err := pasta.DeleteNodeOwnedConfigKeys(cfg); err != nil {
		return err
	}
	return cfg.Set(configer.Path{"outputs"}, objectUnpackerOutputsConfig(n.config.Outputs))
}

func (n *objectUnpackerNode) outputByID(id string) (objectUnpackerOutput, bool) {
	for _, output := range n.config.Outputs {
		if output.ID == id {
			return output, true
		}
	}
	return objectUnpackerOutput{}, false
}

func (n *objectUnpackerNode) recalculate(broadcast bool) {
	changed := false
	next := map[uint64]any{}
	for _, output := range n.config.Outputs {
		port := n.rights[output.ID]
		if port == 0 {
			continue
		}
		value := n.extract(output)
		next[port] = value
		if !objectPayloadEqual(n.outputs[port], value) {
			changed = true
		}
	}
	for port := range n.outputs {
		if _, ok := next[port]; !ok {
			changed = true
		}
	}
	n.outputs = next
	_ = n.clearLabel()
	if broadcast && changed {
		n.sendAll()
	}
}

func (n *objectUnpackerNode) extract(output objectUnpackerOutput) any {
	value, ok := persist.GetIn(n.input.Value(), objectPathToPersistSteps(output.Path)...)
	if !ok || value.Kind() == persist.KindNil {
		return outputDefaultPayload(output)
	}
	switch output.Type {
	case TypeInt:
		if v, ok := value.Int64(); ok {
			return Int(v)
		}
	case TypeFloat:
		if v, ok := value.Float64(); ok {
			return Float(v)
		}
	case TypeString:
		if v, ok := value.StringValue(); ok {
			return String(v)
		}
	case TypeBool:
		if v, ok := value.BoolValue(); ok {
			return v
		}
	case TypeObject:
		if object, ok := ObjectFromValue(value); ok {
			return object
		}
	}
	return outputDefaultPayload(output)
}

func objectPathToPersistSteps(path []objectPathStep) []persist.Step {
	steps := make([]persist.Step, 0, len(path))
	for _, step := range path {
		if step.IsIndex {
			steps = append(steps, persist.Index(step.Index))
		} else {
			steps = append(steps, persist.MapKey(persist.KString(step.Key)))
		}
	}
	return steps
}

func outputDefaultPayload(output objectUnpackerOutput) any {
	switch output.Type {
	case TypeInt:
		value, _ := parseIntAny(output.Default)
		return Int(value)
	case TypeFloat:
		value, _ := parseFloatAny(output.Default)
		return Float(value)
	case TypeString:
		value, _ := parseStringAny(output.Default)
		return String(value)
	case TypeBool:
		value, _ := parseBoolAny(output.Default)
		return value
	case TypeObject:
		return NilObject()
	default:
		return nil
	}
}

func objectPayloadEqual(a, b any) bool {
	oa, oka := a.(Object)
	ob, okb := b.(Object)
	if oka || okb {
		return oka && okb && oa.Equal(ob)
	}
	return reflect.DeepEqual(a, b)
}

func (n *objectUnpackerNode) updatePorts() error {
	desired := objectUnpackerDesiredPorts(n.config.Outputs)
	byName := map[string]uint64{}
	byOutput := map[string]uint64{}
	for outputID, port := range n.rights {
		if port > 0 {
			byOutput[outputID] = port
		}
	}
	current := []uint64{}
	snapshot, ok := n.w.NodeSnapshotLocked(n.id)
	if ok {
		current = append([]uint64{}, snapshot.RightPorts...)
		for _, port := range current {
			if ps, ok := n.w.PortSnapshotLocked(port); ok {
				byName[ps.Name] = port
			}
		}
		if len(snapshot.LeftPorts) > 0 && n.in == 0 {
			n.in = snapshot.LeftPorts[0]
		}
	}

	keep := map[uint64]struct{}{}
	ordered := make([]uint64, 0, len(desired))
	for _, desired := range desired {
		port := desired.Port
		if existing := byOutput[desired.OutputID]; existing > 0 {
			keep[existing] = struct{}{}
			ordered = append(ordered, existing)
			_ = n.w.SetPortNameLocked(existing, port.Name)
			_ = n.w.SetPortTypesLocked(existing, port.Types)
			continue
		}
		if existing := byName[port.Name]; existing > 0 {
			keep[existing] = struct{}{}
			ordered = append(ordered, existing)
			_ = n.w.SetPortTypesLocked(existing, port.Types)
			continue
		}
		port.Node = n.id
		id, err := n.w.AddPortLocked(port)
		if err != nil {
			return err
		}
		keep[id] = struct{}{}
		ordered = append(ordered, id)
	}
	for _, port := range current {
		if _, ok := keep[port]; !ok {
			n.w.RemovePortLocked(port)
		}
	}
	n.refreshRights()
	if len(ordered) > 0 {
		if err := n.w.SetNodePortOrderLocked(n.id, "right", ordered); err != nil {
			return err
		}
	}
	return nil
}

func (n *objectUnpackerNode) refreshRights() {
	n.rights = map[string]uint64{}
	n.fields = map[uint64]string{}
	if n.w == nil || n.id == 0 {
		return
	}
	snapshot, ok := n.w.NodeSnapshotLocked(n.id)
	if !ok {
		return
	}
	byName := map[string]uint64{}
	for _, port := range snapshot.RightPorts {
		if ps, ok := n.w.PortSnapshotLocked(port); ok {
			byName[ps.Name] = port
		}
	}
	for _, output := range n.config.Outputs {
		port := byName[objectOutputPortName(output.Name)]
		n.rights[output.ID] = port
		if port > 0 {
			n.fields[port] = output.ID
		}
	}
}

func (n *objectUnpackerNode) requestInput() {
	snapshot, ok := n.w.PortSnapshotLocked(n.in)
	if !ok {
		return
	}
	for _, link := range snapshot.Links {
		n.requestLink(link, n.in)
	}
}

func (n *objectUnpackerNode) requestLink(link, port uint64) {
	snapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: RequestValue{}})
}

func (n *objectUnpackerNode) sendAll() {
	for _, port := range n.rights {
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if !ok {
			continue
		}
		for _, link := range snapshot.Links {
			n.sendToLink(link, port)
		}
	}
}

func (n *objectUnpackerNode) sendToLink(link, port uint64) {
	value, ok := n.outputs[port]
	if !ok {
		return
	}
	snapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: value})
}

func (n *objectUnpackerNode) clearLabel() error {
	if n.w == nil || n.id == 0 {
		return nil
	}
	return n.w.SetNodeLabelLocked(n.id, "")
}

func (n *objectUnpackerNode) sendMenuSnapshot(force bool) {
	n.w.SendNodeMenuMsgLocked(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Force:       force,
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *objectUnpackerNode) sendMenuBlock() {
	n.w.SendNodeMenuMsgLocked(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *objectUnpackerNode) menuBlock() formular.Block {
	return formular.Block{
		ID: "object", Order: 10, Generation: 1, Form: true,
		Items: []formular.Item{{
			Type:  formular.ItemField,
			ID:    "outputs",
			Label: "Outputs",
			Field: &formular.Field{
				Kind:      formular.FieldArray,
				Value:     objectUnpackerOutputsArrayValue(n.config.Outputs),
				Templates: objectUnpackerOutputTemplates(),
				Elements:  objectUnpackerOutputElements(n.config.Outputs),
			},
		}},
	}
}

func readObjectUnpackerConfig(cfg configer.Config) objectUnpackerConfig {
	config := objectUnpackerConfig{}
	if cfg == nil {
		return normalizeObjectUnpackerConfig(config)
	}
	if raw, err := cfg.Get(configer.Path{"outputs"}); err == nil {
		if outputs, ok := parseObjectUnpackerOutputs(raw); ok {
			config.Outputs = outputs
		}
	}
	config = normalizeObjectUnpackerConfig(config)
	if err := validateObjectUnpackerConfig(config); err != nil {
		return objectUnpackerConfig{}
	}
	return config
}

func parseObjectUnpackerForm(values map[string]any) (objectUnpackerConfig, bool) {
	outputs, ok := parseObjectUnpackerOutputs(values["outputs"])
	if !ok {
		return objectUnpackerConfig{}, false
	}
	config := normalizeObjectUnpackerConfig(objectUnpackerConfig{Outputs: outputs})
	if err := validateObjectUnpackerConfig(config); err != nil {
		return objectUnpackerConfig{}, false
	}
	return config, true
}

func normalizeObjectUnpackerConfig(config objectUnpackerConfig) objectUnpackerConfig {
	config.Outputs = normalizeObjectUnpackerOutputs(config.Outputs)
	return config
}

func normalizeObjectUnpackerOutputs(outputs []objectUnpackerOutput) []objectUnpackerOutput {
	out := make([]objectUnpackerOutput, 0, len(outputs))
	seenIDs := map[string]struct{}{}
	seenPorts := map[string]struct{}{}
	for i, output := range outputs {
		if output.ID == "" {
			output.ID = "output-" + strconv.Itoa(i+1)
		}
		if _, seen := seenIDs[output.ID]; seen {
			output.ID = "output-" + strconv.Itoa(i+1)
		}
		seenIDs[output.ID] = struct{}{}
		if !objectTypeSupported(output.Type) {
			output.Type = TypeObject
		}
		output.Default = normalizeObjectOutputDefault(output.Type, output.Default)
		output.Name = uniquePortName(output.Name, seenPorts)
		seenPorts[output.Name] = struct{}{}
		out = append(out, output)
	}
	return out
}

func normalizeObjectOutputDefault(typ string, value any) any {
	switch typ {
	case TypeInt:
		v, _ := parseIntAny(value)
		return v
	case TypeFloat:
		v, _ := parseFloatAny(value)
		return v
	case TypeString:
		v, _ := parseStringAny(value)
		return v
	case TypeBool:
		v, _ := parseBoolAny(value)
		return v
	default:
		return nil
	}
}

func validateObjectUnpackerConfig(config objectUnpackerConfig) error {
	for _, output := range config.Outputs {
		if !objectTypeSupported(output.Type) {
			return pasta.LinkTypeErr(output.Type)
		}
		if len(output.Path) == 0 {
			return pasta.ErrPortName
		}
	}
	return nil
}

func parseObjectUnpackerOutputs(value any) ([]objectUnpackerOutput, bool) {
	items, ok := parseObjectArray(value)
	if !ok {
		return nil, false
	}
	outputs := make([]objectUnpackerOutput, 0, len(items))
	for i, item := range items {
		output, ok := objectUnpackerOutputFromValues(item.ID, item.Values)
		if !ok {
			return nil, false
		}
		if output.ID == "" {
			output.ID = "output-" + strconv.Itoa(i+1)
		}
		outputs = append(outputs, output)
	}
	return normalizeObjectUnpackerOutputs(outputs), true
}

func objectUnpackerOutputFromValues(id string, values map[string]any) (objectUnpackerOutput, bool) {
	name, _ := parseStringAny(values["name"])
	typ, ok := objectTypeFromMenu(values["type"])
	if !ok {
		return objectUnpackerOutput{}, false
	}
	path, ok := parseObjectPath(values["path"], false)
	if !ok {
		return objectUnpackerOutput{}, false
	}
	return objectUnpackerOutput{ID: id, Name: name, Type: typ, Path: path, Default: normalizeObjectOutputDefault(typ, values["default"])}, true
}

func objectUnpackerOutputsConfig(outputs []objectUnpackerOutput) []any {
	out := make([]any, 0, len(outputs))
	for _, output := range normalizeObjectUnpackerOutputs(outputs) {
		out = append(out, map[string]any{
			"id":      output.ID,
			"name":    output.Name,
			"type":    output.Type,
			"path":    objectPathConfigValue(output.Path),
			"default": output.Default,
		})
	}
	return out
}

func objectUnpackerOutputsArrayValue(outputs []objectUnpackerOutput) []formular.ArrayElementValue {
	values := make([]formular.ArrayElementValue, 0, len(outputs))
	for _, output := range outputs {
		values = append(values, formular.ArrayElementValue{
			ID:       output.ID,
			Template: "output",
			Values: map[string]any{
				"name":    output.Name,
				"type":    objectTypeMenuName(output.Type),
				"path":    objectPathText(output.Path),
				"default": output.Default,
			},
		})
	}
	return values
}

func objectUnpackerOutputTemplates() []formular.ArrayTemplate {
	return []formular.ArrayTemplate{{
		Name:  "output",
		Label: "Output",
		Items: []formular.Item{
			{Type: formular.ItemField, ID: "name", Label: "Name", Field: &formular.Field{Kind: formular.FieldText, Placeholder: "Total"}},
			{Type: formular.ItemField, ID: "type", Label: "Type", Field: &formular.Field{Kind: formular.FieldRadio, Value: objectTypeMenuName(TypeObject), AllowedValues: objectBasicTypes}},
			{Type: formular.ItemField, ID: "path", Label: "Path", Field: &formular.Field{Kind: formular.FieldText, Placeholder: `["summary","total"]`}},
			{Type: formular.ItemField, ID: "default", Label: "Default", Field: &formular.Field{Kind: formular.FieldText, Placeholder: "0"}},
		},
	}}
}

func objectUnpackerOutputElements(outputs []objectUnpackerOutput) []formular.ArrayElement {
	elements := make([]formular.ArrayElement, 0, len(outputs))
	for _, output := range outputs {
		elements = append(elements, formular.ArrayElement{
			ID:       output.ID,
			Template: "output",
			Items: []formular.Item{
				{Type: formular.ItemField, ID: "name", Label: "Name", Field: &formular.Field{Kind: formular.FieldText, Value: output.Name, Placeholder: "Total"}},
				{Type: formular.ItemField, ID: "type", Label: "Type", Field: &formular.Field{Kind: formular.FieldRadio, Value: objectTypeMenuName(output.Type), AllowedValues: objectBasicTypes}},
				{Type: formular.ItemField, ID: "path", Label: "Path", Field: &formular.Field{Kind: formular.FieldText, Value: objectPathText(output.Path), Placeholder: `["summary","total"]`}},
				{Type: formular.ItemField, ID: "default", Label: "Default", Field: objectDefaultField(output)},
			},
		})
	}
	return elements
}

func objectDefaultField(output objectUnpackerOutput) *formular.Field {
	field := &formular.Field{Placeholder: "0"}
	switch output.Type {
	case TypeInt:
		field.Kind = formular.FieldInt
		field.Value, _ = parseIntAny(output.Default)
	case TypeFloat:
		field.Kind = formular.FieldFloat
		field.Value, _ = parseFloatAny(output.Default)
	case TypeString:
		field.Kind = formular.FieldText
		field.Value, _ = parseStringAny(output.Default)
		field.Placeholder = "missing"
	case TypeBool:
		field.Kind = formular.FieldCheckbox
		field.Value, _ = parseBoolAny(output.Default)
	default:
		field.Kind = formular.FieldText
		field.Value = ""
		field.Readonly = true
		field.Placeholder = "null"
	}
	return field
}

type objectUnpackerDesiredPort struct {
	OutputID string
	Port     pasta.Port
}

func objectUnpackerDesiredPorts(outputs []objectUnpackerOutput) []objectUnpackerDesiredPort {
	ports := make([]objectUnpackerDesiredPort, 0, len(outputs))
	for _, output := range normalizeObjectUnpackerOutputs(outputs) {
		ports = append(ports, objectUnpackerDesiredPort{
			OutputID: output.ID,
			Port:     pasta.Port{Direction: "right", Name: objectOutputPortName(output.Name), Types: []string{output.Type}},
		})
	}
	return ports
}

func objectOutputPortName(name string) string {
	return "Out " + name
}

func reconcileObjectUnpackerState(state *pasta.NodeClassState, config objectUnpackerConfig) {
	state.PrimaryType = TypeObject
	state.LeftPorts = []pasta.Port{{Direction: "left", Name: "input", Types: []string{TypeObject}}}
	previous := map[string]pasta.Port{}
	for _, port := range state.RightPorts {
		previous[port.Name] = port
	}
	state.RightPorts = nil
	for _, desired := range objectUnpackerDesiredPorts(config.Outputs) {
		port := desired.Port
		if kept, ok := previous[port.Name]; ok {
			kept.Direction = "right"
			kept.Types = slices.Clone(port.Types)
			state.RightPorts = append(state.RightPorts, kept)
			continue
		}
		state.RightPorts = append(state.RightPorts, port)
	}
}

func objectUnpackerConfigsEqual(a, b objectUnpackerConfig) bool {
	return reflect.DeepEqual(a.Outputs, b.Outputs)
}
